package catalog

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestExternalTextPathsPinned(t *testing.T) {
	for ref, want := range map[string][]string{
		"AddressBookRecords": {"records.memo", "records.name", "records.tag"},
		"WorkspaceMembers":   {"members._embedded.profile.name"},
		"Activities":         {"activities.description", "activities.title"},
		"Transactions": {
			"transactions.extra.description",
			"transactions.extra.fromWorkspaceName",
			"transactions.extra.fromWorkspaceTag",
			"transactions.extra.memo",
			"transactions.extra.toWorkspaceName",
			"transactions.extra.toWorkspaceTag",
		},
	} {
		if got := slices.Sorted(maps.Keys(ExternalTextPaths(ref))); !slices.Equal(got, want) {
			t.Errorf("%s: paths = %v, want %v", ref, got, want)
		}
	}
}

func TestExternalTextPathsUseWireNames(t *testing.T) {
	for ref, paths := range externalTextPathsByRef {
		for p := range paths {
			if slices.Contains(strings.Split(p, "."), "embedded") {
				t.Errorf("%s: path %q has a bare `embedded` segment, but the wire key is `_embedded`, so it never matches a response", ref, p)
			}
		}
	}
}
