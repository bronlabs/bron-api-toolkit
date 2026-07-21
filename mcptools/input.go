package mcptools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/qparam"
)

// fieldsFromInput pulls the `fields` value (comma-separated dot-paths) out of
// the agent-passed input for response projection. Absent / empty → no
// projection (the full object is returned).
func fieldsFromInput(in map[string]any) []string {
	raw, ok := in["fields"]
	if !ok || raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// jqFromInput pulls the `jq` program string out of the agent-passed input.
// Absent / empty / non-string → "" (no transform).
func jqFromInput(in map[string]any) string {
	raw, ok := in["jq"]
	if !ok || raw == nil {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// embedTokensFromInput pulls the `embed` value out of the agent-passed input
// and returns it as a clean token slice. The schema constrains `embed` to a
// comma-separated string (matches the CLI's `--embed prices,foo`); the
// MCP-go-sdk validates incoming arguments against the schema before this
// runs, so a non-string here would have already been rejected.
func embedTokensFromInput(in map[string]any) []string {
	raw, ok := in["embed"]
	if !ok || raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CoerceBodyDates is a thin alias over qparam.CoerceBodyDates so the MCP path
// reads symmetrically alongside coerceBodyDates calls in the CLI's generated
// write handlers — same coercion, single source of truth.
func CoerceBodyDates(payload any) error {
	return qparam.CoerceBodyDates(payload)
}

// ExtractBodyBaseline pulls the optional `body` field out of the input map and
// returns it as the JSON baseline. Maps pass through (interface{} == any in
// Go 1.18+, no copy needed); anything else gets re-marshalled through a
// json.Decoder with UseNumber so big-int amount fields don't lose precision
// (`15000000000` would otherwise round-trip as `1.5e+10` and fail the
// backend's decimal parser).
func ExtractBodyBaseline(in map[string]any) (any, error) {
	v, ok := in["body"]
	if !ok || v == nil {
		return nil, nil
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("body: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("body: unmarshal: %w", err)
	}
	return out, nil
}

// bodyFields collects flat overlay fields from the input map — everything that
// isn't a path arg, query param, the reserved `body` key, or the workspace
// selector. Each value is stringified via StringValue so body.Compose can
// JSON-parse numerics/bools.
func bodyFields(in map[string]any, e catalog.HelpEntry) map[string]string {
	skip := map[string]bool{"body": true, WorkspaceParamName: true}
	for _, p := range e.PathArgs {
		skip[p] = true
	}
	for _, q := range e.QueryParams {
		skip[q.Name] = true
	}
	out := map[string]string{}
	for k, v := range in {
		if skip[k] {
			continue
		}
		if s := StringValue(v); s != "" {
			out[k] = s
		}
	}
	return out
}

// StringValue stringifies one input value the way the CLI does — strings pass
// through, numbers/booleans become their JSON repr (so body.Compose's
// json.Unmarshal recovers the typed scalar), nested objects/arrays go through
// json.Marshal. Empty / nil → empty string so callers can `if s == ""` skip.
func StringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case json.Number:
		return string(x)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// queryParamValue is the query-param flavour of StringValue: arrays of
// scalars collapse to a comma-separated string (the wire form the backend's
// list-query parser expects), everything else falls through to StringValue.
//
// MCP clients that respect a `string` schema (Cursor, Cline, Claude Code)
// already pass an array as `["a","b"]` even when we declared the schema as
// `string` for legacy reasons. Without this helper they'd land in the URL as
// the raw JSON, which the backend rejects.
func queryParamValue(v any) string {
	if arr, ok := v.([]any); ok {
		parts := make([]string, 0, len(arr))
		for _, item := range arr {
			s := StringValue(item)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	}
	return StringValue(v)
}
