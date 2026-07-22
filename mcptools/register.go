package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bronlabs/bron-api-toolkit/body"
	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/jqfilter"
	"github.com/bronlabs/bron-api-toolkit/output"
	"github.com/bronlabs/bron-api-toolkit/qparam"
)

// Doer executes one Bron API request. Both the CLI's HTTP client (process-wide
// workspace + API key) and Desktop's per-grant signer satisfy it, so the
// spec-driven registration below is shared: the caller supplies how a request
// is authenticated and dispatched, the registrar owns which tools exist and
// how their arguments map onto a request.
type Doer interface {
	Do(ctx context.Context, method, path string, pathParams map[string]string, body, query, result any) error
}

// EmbedAugmentor encapsulates a client-side join: when the agent passes an
// `embed` token on a registered (resource, verb), Apply mutates the result in
// place to attach the resolved/calculated extras under `_embedded`.
type EmbedAugmentor struct {
	Description string
	Apply       func(ctx context.Context, doer Doer, result any, tokens []string) error
}

// Options tunes spec-driven registration for one consumer.
//
//   - ReadOnly skips state-changing endpoints (keeps GET + `tx.dry-run`).
//   - Allow gates each candidate tool by name; nil means "register everything".
//   - WorkspaceParam adds a required `workspaceId` selector to every tool and
//     routes it into the request path — Desktop resolves the signing grant from
//     it. The CLI leaves it off (its client injects a process-wide workspace).
//   - EmbedAugmentors / PreCallValidators attach per-tool client-side behaviour.
type Options struct {
	ReadOnly          bool
	Allow             func(toolName string) bool
	WorkspaceParam    bool
	EmbedAugmentors   map[string]*EmbedAugmentor
	PreCallValidators map[string]func(in map[string]any) error
}

func (o Options) allows(name string) bool {
	if o.Allow == nil {
		return true
	}
	return o.Allow(name)
}

// SpecTool is one spec-driven MCP tool that passed the active Options filters.
type SpecTool struct {
	Name        string
	Resource    string
	Verb        string
	Entry       catalog.HelpEntry
	Description string
	InputSchema *jsonschema.Schema
}

// SpecTools returns the endpoint tools that survive the ReadOnly and Allow
// filters, with input schemas already built for opts. RegisterSpecDriven wires
// these onto a server; tests inspect them directly.
func SpecTools(opts Options) []SpecTool {
	var out []SpecTool
	for _, r := range SortedKeys(catalog.HelpEntries) {
		verbs := catalog.HelpEntries[r]
		for _, v := range SortedKeys(verbs) {
			e := verbs[v]
			if opts.ReadOnly && !IsReadOnlyEndpoint(r, v, e) {
				continue
			}
			name := ToolName(r, v)
			if !opts.allows(name) {
				continue
			}
			embedDesc := ""
			if aug := opts.EmbedAugmentors[r+"."+v]; aug != nil {
				embedDesc = aug.Description
			}
			out = append(out, SpecTool{
				Name:        name,
				Resource:    r,
				Verb:        v,
				Entry:       e,
				Description: endpointDescription(r, v, e),
				InputSchema: endpointSchema(r, v, e, embedDesc, opts.WorkspaceParam),
			})
		}
	}
	return out
}

// RegisterSpecDriven auto-registers one MCP tool per CLI endpoint that passes
// opts, all driven by the generated metadata. Untouched when new endpoints
// land — regen is the only step.
func RegisterSpecDriven(server *mcp.Server, doer Doer, opts Options) {
	for _, t := range SpecTools(opts) {
		registerEndpoint(server, doer, t, opts)
	}
}

func registerEndpoint(server *mcp.Server, doer Doer, t SpecTool, opts Options) {
	aug := opts.EmbedAugmentors[t.Resource+"."+t.Verb]
	validate := opts.PreCallValidators[t.Resource+"."+t.Verb]
	untrusted := catalog.ExternalTextKeys(t.Entry.ResponseRef)
	tool := &mcp.Tool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		if validate != nil {
			if err := validate(in); err != nil {
				return ErrorResult(err), nil, nil
			}
		}
		result, err := runEndpoint(ctx, doer, t.Entry, in, opts.WorkspaceParam)
		if err != nil {
			return ErrorResult(err), nil, nil
		}
		if aug != nil {
			tokens := embedTokensFromInput(in)
			if len(tokens) > 0 {
				if err := aug.Apply(ctx, augmentorDoer(doer, in, opts), result, tokens); err != nil {
					return ErrorResult(err), nil, nil
				}
			}
		}
		shaped, err := shapeResult(result, fieldsFromInput(in), jqFromInput(in), untrusted)
		if err != nil {
			return ErrorResult(err), nil, nil
		}
		return toolResult(shaped)
	})
}

// shapeResult runs the response pipeline: untrusted wrapping first (so its
// markers survive projection), then field projection, then the optional jq
// program, then date humanization. Wrapping before Project is a security
// requirement — projecting first strips leaves before they can be wrapped. The
// untrusted set is the endpoint's spec-driven `external-text` key names.
func shapeResult(result any, fields []string, jqProg string, untrusted map[string]bool) (any, error) {
	shaped := output.Project(WrapUntrustedFieldsWithKeys(result, untrusted), fields)
	if jqProg != "" {
		plain := output.Plain(shaped)
		out, err := jqfilter.Run(jqProg, plain)
		if err != nil {
			return nil, err
		}
		if isEmptyJqResult(out) {
			return map[string]any{"result": out, "hint": emptyJqHint(plain)}, nil
		}
		shaped = out
	}
	return output.HumanizeDates(shaped), nil
}

func isEmptyJqResult(v any) bool {
	if v == nil {
		return true
	}
	arr, ok := v.([]any)
	return ok && len(arr) == 0
}

// emptyJqHint tells the agent what it could have matched: a jq program that
// yields null / [] usually guessed a wrong envelope key, and the silent null
// gives no signal on its own.
func emptyJqHint(plain any) string {
	if m, ok := plain.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "jq produced no output; input top-level keys: [" + strings.Join(keys, ", ") + "]"
	}
	return "jq produced no output; input is " + jsonTypeName(plain)
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case float64, int, int64, json.Number:
		return "a number"
	default:
		return "a scalar"
	}
}

// toolResult builds the MCP result. structuredContent must be a JSON object per
// the MCP spec, so a non-object final value (jq yielding a number/array/etc.) is
// wrapped as {"result": <value>}; objects pass through unwrapped. The text
// content mirrors the SDK default — the serialized raw value, unwrapped.
func toolResult(v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ErrorResult(err), nil, nil
	}
	structured := v
	if len(raw) == 0 || raw[0] != '{' {
		structured = map[string]any{"result": v}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, structured, nil
}

// augmentorDoer scopes enrichment sub-calls to the tool call's workspace:
// workspace-routing doers mint per-workspace credentials from pathParams, and
// augmentor endpoints (/dictionary/*) carry no workspace of their own.
func augmentorDoer(doer Doer, in map[string]any, opts Options) Doer {
	if !opts.WorkspaceParam {
		return doer
	}
	wsID := StringValue(in[WorkspaceParamName])
	if wsID == "" {
		return doer
	}
	return scopedDoer{inner: doer, params: map[string]string{WorkspaceParamName: wsID}}
}

type scopedDoer struct {
	inner  Doer
	params map[string]string
}

func (d scopedDoer) Do(ctx context.Context, method, path string, pathParams map[string]string, body, query, result any) error {
	merged := make(map[string]string, len(d.params)+len(pathParams))
	for k, v := range d.params {
		merged[k] = v
	}
	for k, v := range pathParams {
		merged[k] = v
	}
	return d.inner.Do(ctx, method, path, merged, body, query, result)
}

// runEndpoint executes one HelpEntry — same code path as the generated CLI
// command for that endpoint. When workspaceParam is set the `workspaceId`
// argument is routed as a path parameter so the Doer can both select the
// signing grant and resolve any `{workspaceId}` placeholder.
func runEndpoint(ctx context.Context, doer Doer, e catalog.HelpEntry, in map[string]any, workspaceParam bool) (any, error) {
	pathParams := map[string]string{}
	if workspaceParam {
		wsID := StringValue(in[WorkspaceParamName])
		if wsID == "" {
			return nil, fmt.Errorf("missing required parameter %q", WorkspaceParamName)
		}
		pathParams[WorkspaceParamName] = wsID
	}
	for _, name := range e.PathArgs {
		s := StringValue(in[name])
		if s == "" {
			return nil, fmt.Errorf("missing required path parameter %q", name)
		}
		pathParams[name] = s
	}

	var query any
	if len(e.QueryParams) > 0 {
		q := map[string]any{}
		for _, p := range e.QueryParams {
			s := queryParamValue(in[p.Name])
			if s == "" {
				continue
			}
			coerced, err := qparam.MaybeDate(p.Name, s)
			if err != nil {
				return nil, err
			}
			q[p.Name] = coerced
		}
		if len(q) > 0 {
			query = q
		}
	}

	var payload any
	if e.Method != "GET" && e.Method != "DELETE" {
		baseline, err := ExtractBodyBaseline(in)
		if err != nil {
			return nil, err
		}
		fields := bodyFields(in, e)
		payload, err = body.Compose(baseline, fields)
		if err != nil {
			return nil, err
		}
		if err := CoerceBodyDates(payload); err != nil {
			return nil, err
		}
	}

	var result any
	if err := doer.Do(ctx, e.Method, e.Path, pathParams, payload, query, &result); err != nil {
		return nil, err
	}
	return result, nil
}
