package mcptools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/qparam"
)

// WorkspaceParamName is the required workspace selector every desktop tool
// carries. It is not a spec path arg — it routes the call to the (client,
// workspace) grant that signs the request, and is substituted into any
// `{workspaceId}` path placeholder when present.
const WorkspaceParamName = "workspaceId"

// ToolName converts (resource, verb) into a stable MCP tool name.
//
//	bron_<resource>_<verb> with dashes turned into underscores.
//
// The MCP spec restricts tool names to [a-zA-Z0-9_-]; our resources and verbs
// already comply, but address-book/create-signing-request style verbs need
// the dash → underscore swap so the JSON-Schema name pattern ($_a-z0-9) is
// satisfied uniformly.
func ToolName(resource, verb string) string {
	return "bron_" + SanitizeName(resource) + "_" + SanitizeName(verb)
}

func SanitizeName(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsReadOnlyEndpoint flags endpoints that are safe to expose under
// `bron mcp --read-only`. The source of truth is the OpenAPI spec's
// per-endpoint API-key permissions list (mined into `e.Permissions` by
// cligen): a tool is read-only iff its permissions include "View only".
// This avoids the "GET that mutates" footgun where a future endpoint like
// `GET /transactions/{id}/retry-broadcast` would slip through a method-only
// heuristic, and inverts the safety polarity to fail-closed (no permissions
// metadata → not read-only).
//
// One explicit allow-list entry: `tx.dry-run` is a POST per spec but pure
// validation (no DB writes, no audit-log entries, no rate-counter advance —
// confirmed against backend). It's the only POST surfaced in read-only mode.
func IsReadOnlyEndpoint(resource, verb string, e catalog.HelpEntry) bool {
	if resource == "tx" && verb == "dry-run" {
		return true
	}
	for _, p := range e.Permissions {
		if p == "View only" {
			return true
		}
	}
	return false
}

// endpointSchema derives a JSON schema from the HelpEntry's path args, query
// params and (for write endpoints) the writeBodyFields fallback list. embedDesc
// (non-empty) exposes an `embed` property; workspaceParam adds a required
// workspaceId selector at the top level.
func endpointSchema(resource, verb string, e catalog.HelpEntry, embedDesc string, workspaceParam bool) *jsonschema.Schema {
	props := map[string]*jsonschema.Schema{}
	var required []string

	if workspaceParam {
		props[WorkspaceParamName] = &jsonschema.Schema{
			Type:        "string",
			Description: "Workspace to act in — required. Routes the call to that workspace's grant.",
		}
		required = append(required, WorkspaceParamName)
	}

	if embedDesc != "" {
		props["embed"] = &jsonschema.Schema{
			Type:        "string",
			Description: embedDesc,
		}
	}

	for _, name := range e.PathArgs {
		props[name] = &jsonschema.Schema{
			Type:        "string",
			Description: "Path parameter — required.",
		}
		required = append(required, name)
	}

	for _, q := range e.QueryParams {
		props[q.Name] = queryParamSchema(q)
		if q.Required {
			required = append(required, q.Name)
		}
	}

	if e.Method == "GET" {
		props["fields"] = &jsonschema.Schema{
			Type:        "string",
			Description: "Keep only these dot-paths, e.g. `transactionId,params.amount` (see server instructions).",
		}
		props["jq"] = &jsonschema.Schema{
			Type:        "string",
			Description: "gojq program to reshape/filter the reply server-side, after `fields` (see server instructions).",
		}
	}

	if e.Method != "GET" && e.Method != "DELETE" {
		props["body"] = &jsonschema.Schema{
			Type:        "object",
			Description: fmt.Sprintf("Full request body as JSON (matches the %s schema). Optional — overrides individual fields below.", e.BodyRef),
		}
		for k, desc := range writeBodyFields(resource, verb) {
			props[k] = &jsonschema.Schema{Type: "string", Description: desc}
		}
	}

	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           props,
		Required:             required,
		AdditionalProperties: &jsonschema.Schema{},
	}
}

// maxInlineEnum caps how many enum values we inline into a tool schema.
// Only an oversized niche list crosses it (the 37-value activity-type filter,
// ~1k chars and repeated twice) — its values are dropped and the agent is
// pointed at `--schema`, trading a one-off lookup for context re-read every
// session. Common tx status/type enums (≤28 values) stay inline so the model
// fills them without a round-trip.
const maxInlineEnum = 30

var (
	mdSeePointerRe = regexp.MustCompile(`\s*\[[Ss]ee [^\]]*\]\(/[^)]*\)`)
	mdDocLinkRe    = regexp.MustCompile(`\[([^\]]+)\]\(/[^)]*\)`)
	multiDotRe     = regexp.MustCompile(`\.\s*\.`)
	multiSpaceRe   = regexp.MustCompile(`\s{2,}`)
)

// slimDesc strips MCP-only fat from a spec-sourced description: internal
// markdown doc-links the model can't follow. "See details" pointers are
// dropped outright; any other internal link is unwrapped to its text. The
// human-facing OpenAPI/Mintlify copy keeps the links — only the schema the
// agent loads every session is slimmed.
func slimDesc(s string) string {
	s = mdSeePointerRe.ReplaceAllString(s, "")
	s = mdDocLinkRe.ReplaceAllString(s, "$1")
	s = multiDotRe.ReplaceAllString(s, ".")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// queryParamSchema maps one OpenAPI query parameter to a JSON Schema.
// Mapping rules:
//   - boolean → boolean (e.g. nonEmpty, includeEvents)
//   - integer or string+int* format → integer (e.g. limit, offset — backend
//     declares them as string for URL-encoding reasons but they are
//     numeric)
//   - date-time-millis (any underlying type) → string with the ISO/epoch
//     coercion note, since both the wire and the description match
//   - array, string[], integer[] → string carrying "comma-separated …" hint
//     (the URL form is CSV; coercion from agent-passed arrays happens in
//     stringValue → json.Marshal → not-CSV, so we keep callers on the CSV
//     contract for consistency with the CLI)
//   - everything else → string
//
// Enums propagate so agents see the allowed values up front. Description
// falls back to the spec description; for date params we override with the
// epoch-millis coercion note.
func queryParamSchema(q catalog.HelpQueryParam) *jsonschema.Schema {
	s := &jsonschema.Schema{}
	switch {
	case q.Type == "boolean":
		s.Type = "boolean"
	case q.Type == "integer", q.Type == "string" && (q.Format == "int64" || q.Format == "int32"):
		s.Type = "integer"
	case q.Type == "number":
		s.Type = "number"
	default:
		s.Type = "string"
	}

	if qparam.IsDateParam(q.Name) {
		s.Description = "ISO 8601 or epoch ms."
	} else if q.Description != "" {
		s.Description = slimDesc(q.Description)
	}

	if q.Type == "array" || (strings.HasSuffix(q.Type, "[]") && q.Type != "") {
		const hint = "Comma-separated."
		lower := strings.ToLower(s.Description)
		alreadyNoted := strings.Contains(lower, "comma-separat") || strings.Contains(lower, "comma separat")
		switch {
		case s.Description == "":
			s.Description = hint
		case !alreadyNoted:
			s.Description = strings.TrimRight(s.Description, ". ") + ". " + hint
		}
	}

	if n := len(q.Enum); n > 0 && n <= maxInlineEnum {
		s.Enum = make([]any, 0, n)
		for _, e := range q.Enum {
			s.Enum = append(s.Enum, e)
		}
	} else if n > maxInlineEnum {
		note := fmt.Sprintf("One of %d enum values — pass the one you want; run the CLI with `--schema` for the full list.", n)
		if s.Description == "" {
			s.Description = note
		} else {
			s.Description = strings.TrimRight(s.Description, ". ") + ". " + note
		}
	}

	if strings.Contains(strings.ToLower(q.Name), "symbolid") {
		const hint = "Symbol ids look like s01, s21252 — not s1."
		if s.Description == "" {
			s.Description = hint
		} else {
			s.Description = strings.TrimRight(s.Description, ". ") + ". " + hint
		}
	}
	return s
}

// endpointDescription is the agent-facing tool description. We keep it short
// and lean on the schema to document each parameter — the full per-endpoint
// docs live in `bron <resource> <verb> --help` and `--schema`.
//
// `tx.dry-run` opts out of the auto-appended "State-changing" label —
// dry-run is a `POST /…/dry-run` but it's a read-only validate-only call
// (the "state" prefix would mislead an agent). Override via methodLabelOverrides.
func endpointDescription(resource, verb string, e catalog.HelpEntry) string {
	role := actionDescription(resource, verb, e)
	label := methodLabel(e.Method)
	if override, ok := methodLabelOverrides[resource+"."+verb]; ok {
		label = override
	}
	desc := fmt.Sprintf("%s. CLI mirror: `bron %s %s`. %s.", role, resource, verb, label)
	if e.Method == "GET" {
		if key, ok := catalog.ResponseEnvelopeArrayKey(e.ResponseRef); ok {
			desc += fmt.Sprintf(" Response shape: {%q: [...]}.", key)
		}
	}
	return desc
}

// methodLabelOverrides forces a specific Read-only / State-changing label on
// endpoints whose HTTP method doesn't reflect their actual semantics — the
// only cases today are POST endpoints that don't mutate state.
var methodLabelOverrides = map[string]string{
	"tx.dry-run": "Read-only — safe to call freely (validates a transaction body without sending it)",
}

func actionDescription(resource, verb string, e catalog.HelpEntry) string {
	if d, ok := actionDescriptions[resource+"."+verb]; ok {
		return d
	}
	switch verb {
	case "list":
		return fmt.Sprintf("List %s in the workspace", resource)
	case "get":
		return fmt.Sprintf("Get one %s by id", strings.TrimSuffix(resource, "s"))
	case "create":
		return fmt.Sprintf("Create a %s", strings.TrimSuffix(resource, "s"))
	case "delete":
		return fmt.Sprintf("Delete a %s by id", strings.TrimSuffix(resource, "s"))
	}
	return fmt.Sprintf("`bron %s %s`", resource, verb)
}

// actionDescriptions overrides the generic description for endpoints where the
// auto-generated phrasing is misleading. Keep this short — anything longer
// belongs in the CLI help text, not the MCP description.
var actionDescriptions = map[string]string{
	"workspace.info":            "Get the active workspace's metadata",
	"tx.list":                   "List transactions. For financial totals pass `includeEvents: true` and aggregate `_embedded.events`, not `params.amount` (call `bron_help` for the model)",
	"tx.get":                    "Get one transaction by id. `params.amount` is the requested amount, not the settled one — for actual settlement call `bron_tx_events` and aggregate its events (call `bron_help` for the model)",
	"assets.prices":             "Get USD market prices for assets (filter via baseAssetIds)",
	"symbols.prices":            "Get USD market prices for symbols",
	"tx.approve":                "Approve a transaction (signing-required → waiting-approval → signing). State-changing — confirm with the user before invoking",
	"tx.decline":                "Decline a transaction. Terminal. State-changing — confirm with the user. `reason` surfaces in the audit log",
	"tx.cancel":                 "Cancel a transaction (only valid before signing). Terminal. State-changing — confirm with the user",
	"tx.create":                 "Create a new transaction. Pass `transactionType` + `accountId` + per-type `params.*` fields, OR use a `bron_tx_<type>` shortcut. Call `bron_help` with a shortcut name (e.g. `bron_tx_withdrawal`) for that type's params schema. State-changing — confirm with the user",
	"tx.create-signing-request": "Create a signing request on an existing transaction so signers can produce signatures. State-changing — confirm with the user before invoking",
	"tx.dry-run":                "Validate a transaction body without sending it. Use to preview fees, balance checks, etc.",
	"tx.bulk-create":            "Create many transactions at once — pass `body` as `{ transactions: [CreateTransaction, ...] }` (the spec wraps the array under `transactions`, not a bare array). State-changing — confirm with the user before invoking",
	"tx.events":                 "Get the audit-log event timeline of one transaction",
	"tx.accept-deposit-offer":   "Accept an incoming deposit offer (state-changing)",
	"tx.reject-outgoing-offer":  "Reject an outgoing offer (state-changing)",
	"address-book.create":       "Create an address-book record (saved address / tag / bank). State-changing — confirm with the user",
	"address-book.delete":       "Delete an address-book record by id. State-changing — confirm with the user",
	"intents.create":            "Create a DeFi intent. State-changing — confirm with the user",
}

func methodLabel(method string) string {
	switch method {
	case "GET":
		return "Read-only"
	case "DELETE":
		return "State-changing — destructive"
	default:
		return "State-changing"
	}
}

// writeBodyFields lists the known body-overlay fields per (resource, verb) so
// agents see them as typed inputs instead of having to fall back to the
// catch-all `body` JSON. Keep this in sync with the CLI flags emitted by
// cligen for the matching command.
func writeBodyFields(resource, verb string) map[string]string {
	switch resource + "." + verb {
	case "tx.approve":
		return nil
	case "tx.decline", "tx.cancel":
		return map[string]string{"reason": "Free-text reason surfaced in the audit log"}
	case "tx.create", "tx.dry-run":
		return map[string]string{
			"accountId":       "Source account id",
			"description":     "Free-form description",
			"expiresAt":       "Optional expiry — ISO 8601 or epoch millis",
			"externalId":      "Idempotency key",
			"transactionType": "Transaction type — e.g. withdrawal, allowance, bridge, deposit, defi, defi-message, fiat-in, fiat-out, stake-delegation, stake-undelegation, stake-claim, stake-withdrawal, address-creation, address-activation, intents",
		}
	case "address-book.create":
		return map[string]string{
			"name":       "Display name",
			"address":    "Blockchain address (or tag / bank account number depending on `recordType`)",
			"networkId":  "Network id (ETH, TRX, BTC, ...). Required for blockchain addresses",
			"memo":       "Optional memo / destination tag (XRP, EOS, ...)",
			"recordType": "address | tag | bank",
		}
	case "tx.accept-deposit-offer", "tx.reject-outgoing-offer":
		return map[string]string{"reason": "Free-text reason"}
	}
	return nil
}
