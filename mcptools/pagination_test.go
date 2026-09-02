package mcptools

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/output"
)

func txListParam(t *testing.T, name string) catalog.HelpQueryParam {
	t.Helper()
	for _, p := range catalog.HelpEntries["tx"]["list"].QueryParams {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("tx.list has no query param %q", name)
	return catalog.HelpQueryParam{}
}

func TestMultiValueEnumParamAcceptsCSVArrayAndUnlistedValues(t *testing.T) {
	q := txListParam(t, "transactionStatuses")
	s := queryParamSchema(q)

	if len(s.Enum) != 0 || (s.Items != nil && len(s.Items.Enum) != 0) || s.Pattern != "" {
		t.Fatalf("enum values must be documented, not constrained, got %+v", s)
	}
	if !strings.Contains(s.Description, q.Enum[0]) {
		t.Fatalf("description must list the known values, got %q", s.Description)
	}

	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	valid := []any{
		"completed,canceled",
		[]any{"completed", "canceled"},
		"completed",
		"power-bank-usage",
		[]any{"completed", "power-bank-usage"},
	}
	for _, v := range valid {
		if err := resolved.Validate(v); err != nil {
			t.Fatalf("%v must validate: %v", v, err)
		}
	}
	if err := resolved.Validate(42); err == nil {
		t.Fatal("a number must still be rejected")
	}
}

func TestMultiValueParamWithoutEnum(t *testing.T) {
	s := queryParamSchema(txListParam(t, "accountIds"))

	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, valid := range []any{"a1,a2", []any{"a1", "a2"}} {
		if err := resolved.Validate(valid); err != nil {
			t.Fatalf("%v must validate: %v", valid, err)
		}
	}
}

func TestOversizedEnumStaysOffSchema(t *testing.T) {
	var q catalog.HelpQueryParam
	for _, p := range catalog.HelpEntries["activities"]["list"].QueryParams {
		if p.Name == "activityTypes" {
			q = p
		}
	}
	if len(q.Enum) <= maxInlineEnum {
		t.Skipf("activityTypes enum shrank to %d, pick another oversized param", len(q.Enum))
	}

	s := queryParamSchema(q)
	if strings.Contains(s.Description, q.Enum[0]) {
		t.Fatalf("oversized enum must not inline its values, got %q", s.Description)
	}
	if !strings.Contains(s.Description, "--schema") {
		t.Fatalf("description must point at the full list, got %q", s.Description)
	}
}

func TestEndpointSchemaValidatesEndToEnd(t *testing.T) {
	for _, tool := range SpecTools(Options{ReadOnly: true, WorkspaceParam: true}) {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal schema: %v", tool.Name, err)
		}
		var s jsonschema.Schema
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s: unmarshal schema: %v", tool.Name, err)
		}
		if _, err := s.Resolve(nil); err != nil {
			t.Fatalf("%s: schema does not resolve: %v", tool.Name, err)
		}
	}
}

type pagingDoer struct {
	total     int
	lastQuery map[string]any
}

func (d *pagingDoer) Do(_ context.Context, _, _ string, _ map[string]string, _, query, result any) error {
	d.lastQuery, _ = query.(map[string]any)

	requested := d.total
	if raw, ok := d.lastQuery["limit"].(string); ok {
		if n, err := strconv.Atoi(raw); err == nil && n < requested {
			requested = n
		}
	}

	items := make([]any, 0, requested)
	for i := range requested {
		items = append(items, map[string]any{"transactionId": string(rune('a' + i%26))})
	}
	*(result.(*any)) = map[string]any{"transactions": items}
	return nil
}

func TestListMetaTruncationProbe(t *testing.T) {
	entry := catalog.HelpEntries["tx"]["list"]

	cases := []struct {
		name        string
		total       int
		in          map[string]any
		wantLimit   string
		wantHasMore bool
		wantLen     int
	}{
		{"more than a page", 12, map[string]any{"limit": float64(5)}, "6", true, 5},
		{"exactly a page", 5, map[string]any{"limit": float64(5)}, "6", false, 5},
		{"under a page", 3, map[string]any{"limit": float64(5)}, "6", false, 3},
		{"default limit", defaultListLimit + 10, map[string]any{}, "51", true, defaultListLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doer := &pagingDoer{total: c.total}
			in := map[string]any{"workspaceId": "w1"}
			for k, v := range c.in {
				in[k] = v
			}
			result, err := runEndpoint(context.Background(), doer, entry, in, true)
			if err != nil {
				t.Fatalf("runEndpoint: %v", err)
			}
			if got := doer.lastQuery["limit"]; got != c.wantLimit {
				t.Fatalf("wire limit = %v, want %v", got, c.wantLimit)
			}
			envelope := result.(map[string]any)
			items := envelope["transactions"].([]any)
			if len(items) != c.wantLen {
				t.Fatalf("returned %d items, want %d", len(items), c.wantLen)
			}
			embedded := envelope["_embedded"].(map[string]any)
			if embedded["hasMore"] != c.wantHasMore {
				t.Fatalf("hasMore = %v, want %v", embedded["hasMore"], c.wantHasMore)
			}
			if embedded["returned"] != c.wantLen {
				t.Fatalf("returned = %v, want %d", embedded["returned"], c.wantLen)
			}
		})
	}
}

func TestListMetaSkipsGetByIdAndClampsHugeLimits(t *testing.T) {
	if plan := listPagePlan(catalog.HelpEntries["tx"]["get"], map[string]any{}); plan != nil {
		t.Fatalf("get-by-id must not get a paging plan, got %+v", plan)
	}
	plan := listPagePlan(catalog.HelpEntries["tx"]["list"], map[string]any{"limit": float64(maxMetaLimit + 1)})
	if plan == nil || plan.limit != maxMetaLimit {
		t.Fatalf("over-cap limit must clamp to %d and keep the probe, got %+v", maxMetaLimit, plan)
	}
}

func TestProjectionKeepsListMeta(t *testing.T) {
	envelope := map[string]any{
		"transactions": []any{map[string]any{"transactionId": "t1", "junk": "x"}},
		"_embedded":    map[string]any{"returned": 1, "limit": 50, "hasMore": false},
	}
	projected := output.Plain(output.Project(any(envelope), []string{"transactionId"}))
	m := projected.(map[string]any)
	if _, ok := m["_embedded"]; !ok {
		t.Fatalf("_embedded must survive projection, got %v", m)
	}
	items := m["transactions"].([]any)
	if len(items) != 1 {
		t.Fatalf("projection must still unwrap the envelope, got %v", m)
	}
	if _, junk := items[0].(map[string]any)["junk"]; junk {
		t.Fatalf("projection must drop unrequested fields, got %v", items[0])
	}
}
