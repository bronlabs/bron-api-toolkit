package qparam

import (
	"strconv"
	"testing"
	"time"
)

func TestMaybeDate(t *testing.T) {
	SetDateKeys(map[string]bool{
		"createdAtFrom":    true,
		"createdAtTo":      true,
		"terminatedAtFrom": true,
		"updatedSince":     true,
	})
	t.Cleanup(func() { SetDateKeys(nil) })

	iso := func(s string) string {
		t.Helper()
		layouts := []string{time.RFC3339, "2006-01-02"}
		for _, l := range layouts {
			if v, err := time.Parse(l, s); err == nil {
				return strconv.FormatInt(v.UnixMilli(), 10)
			}
		}
		t.Fatalf("test setup: cannot parse %q", s)
		return ""
	}

	cases := []struct {
		name, value, want string
		wantErr           bool
	}{
		{"limit", "100", "100", false},
		{"createdAtFrom", "1777311599505", "1777311599505", false},
		{"createdAtFrom", "2026-04-01T00:00:00Z", iso("2026-04-01T00:00:00Z"), false},
		{"createdAtTo", "2026-04-27T12:00:00Z", iso("2026-04-27T12:00:00Z"), false},
		{"updatedSince", "2026-04-01", iso("2026-04-01"), false},
		{"terminatedAtFrom", "not-a-date", "", true},
		{"createdAtFrom", "", "", false},
		{"limit", "not-a-date", "not-a-date", false},
	}
	for _, c := range cases {
		got, err := MaybeDate(c.name, c.value)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s=%q: err=%v wantErr=%v", c.name, c.value, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Fatalf("%s=%q: got %q want %q", c.name, c.value, got, c.want)
		}
	}
}

func TestMaybeDateRelative(t *testing.T) {
	SetDateKeys(map[string]bool{"createdAtFrom": true})
	t.Cleanup(func() { SetDateKeys(nil) })

	millis := func(s string) int64 {
		t.Helper()
		got, err := MaybeDate("createdAtFrom", s)
		if err != nil {
			t.Fatalf("%q: unexpected err %v", s, err)
		}
		n, err := strconv.ParseInt(got, 10, 64)
		if err != nil {
			t.Fatalf("%q: not epoch-millis: %q", s, got)
		}
		return n
	}

	before := time.Now().UTC().UnixMilli()
	now := millis("now")
	after := time.Now().UTC().UnixMilli()
	if now < before || now > after {
		t.Fatalf("now resolved outside call window: %d not in [%d,%d]", now, before, after)
	}

	if d := now - millis("now-7d"); d < 6*24*3600*1000 || d > 8*24*3600*1000 {
		t.Fatalf("now-7d offset off: %d ms", d)
	}
	if d := now - millis("-24h"); d < 23*3600*1000 || d > 25*3600*1000 {
		t.Fatalf("-24h offset off: %d ms", d)
	}
	if _, err := MaybeDate("createdAtFrom", "NOW-1W"); err != nil {
		t.Fatalf("case-insensitive relative rejected: %v", err)
	}

	for _, bad := range []string{"now+1d", "7d", "-1y", "-d", "-1x"} {
		if _, err := MaybeDate("createdAtFrom", bad); err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}
