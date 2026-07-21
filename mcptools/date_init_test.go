package mcptools

import (
	"strings"
	"testing"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/output"
	"github.com/bronlabs/bron-api-toolkit/qparam"
)

// The lib self-initializes date keys from the embedded catalog; no consumer
// calls SetDateKeys. Both input coercion and output humanization must work.
func TestDateKeysSelfInitializeFromCatalog(t *testing.T) {
	if !catalog.DateKeys()["createdAtFrom"] {
		t.Fatalf("catalog.DateKeys() missing createdAtFrom: %v", catalog.DateKeys())
	}

	const iso = "2026-04-01T00:00:00Z"
	got, err := qparam.MaybeDate("createdAtFrom", iso)
	if err != nil {
		t.Fatalf("MaybeDate err: %v", err)
	}
	if got == iso || got == "" {
		t.Fatalf("createdAtFrom not coerced to epoch-millis: %q", got)
	}

	humanized := output.HumanizeDates(map[string]any{"createdAtFrom": "1777305600000"})
	m, ok := humanized.(map[string]any)
	if !ok {
		t.Fatalf("HumanizeDates returned %T", humanized)
	}
	s, _ := m["createdAtFrom"].(string)
	if s == "1777305600000" || !strings.Contains(s, "T") || !strings.Contains(s, "Z") {
		t.Fatalf("createdAtFrom not humanized to ISO-8601: %q", s)
	}
}
