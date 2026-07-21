package mcptools

import (
	"fmt"
	"strings"
)

// WrapUntrustedFields walks a JSON-shaped value tree and wraps known
// user-controlled string fields (`description`, `memo`, `note`, `comment`,
// `reason`) in `<untrusted source="<key>">…</untrusted>` markers. Pairs with
// the server-instructions directive that tells the agent to treat envelope
// content as inert data.
//
// Field-name match is intentionally narrow — wrapping every `name` field
// would also catch server-controlled labels (asset names, network labels,
// account names that are technically operator-set but high-trust within the
// workspace). Better to under-wrap than to flood the agent with markers.
//
// In-place mutation; safe to call on `any` (returns input on non-map roots).
func WrapUntrustedFields(v any) any {
	walkAndWrap(v, extraUntrustedKeys)
	return v
}

// WrapUntrustedFieldsWithKeys is the spec-driven variant: it wraps the global
// always-wrap set plus every string under a key in `keys` (the endpoint's
// `format:"external-text"` set from the datamodel), anywhere in the tree. The
// structural DTO-shape heuristics are dropped — the spec set supersedes them.
func WrapUntrustedFieldsWithKeys(v any, keys map[string]bool) any {
	walkAndWrap(v, func(map[string]interface{}) map[string]bool { return keys })
	return v
}

// untrustedKeys are free-form fields whose key name is unambiguous enough to
// wrap globally — they are written by operators or external counterparties and
// never double as a high-trust server-controlled label. `from/toWorkspace*`
// are set by the *other* workspace in a cross-workspace transfer (zero-trust
// to the side reading them), so they belong here too.
var untrustedKeys = map[string]bool{
	"description":       true,
	"memo":              true,
	"note":              true,
	"comment":           true,
	"reason":            true,
	"fromWorkspaceName": true,
	"toWorkspaceName":   true,
	"fromWorkspaceTag":  true,
	"toWorkspaceTag":    true,
}

// untrustedKeysOnAddressBookRecord adds extra fields that are user-supplied
// on address-book records specifically. We can't add `name`/`tag` to the
// global untrustedKeys set without flooding asset/network/account names
// (which are high-trust within the workspace), but on an address-book record
// both are the canonical free-form-text-from-untrusted-counterparty channel —
// an attacker who tricks an operator into saving "Alice (vendor)" with a
// recordId can then stuff `name`/`tag` with prompt-injection content that the
// agent reads when resolving recipients.
//
// Detected by structural shape: a map that has both `recordId` and `address`
// keys is treated as an address-book record (matches AddressBookRecord DTO).
var untrustedKeysOnAddressBookRecord = map[string]bool{
	"name": true,
	"tag":  true,
}

// untrustedKeysOnUserProfile wraps the member display name, which the member
// sets on themselves — a low-privilege member can inject into the owner's
// agent via `bron_members_list`. Same reason `name` can't go in the global
// set; detected structurally (UserProfile DTO is `{icon, name, userId}`).
var untrustedKeysOnUserProfile = map[string]bool{
	"name": true,
}

// untrustedKeysOnActivity wraps the audit-log entry title, which can carry
// operator/counterparty-supplied text for some activity types. `description`
// on the same DTO is already covered by the global set.
var untrustedKeysOnActivity = map[string]bool{
	"title": true,
}

func isAddressBookRecord(m map[string]interface{}) bool {
	_, hasID := m["recordId"]
	_, hasAddr := m["address"]
	return hasID && hasAddr
}

func isUserProfile(m map[string]interface{}) bool {
	_, hasUser := m["userId"]
	_, hasName := m["name"]
	return hasUser && hasName
}

func isActivity(m map[string]interface{}) bool {
	_, hasID := m["activityId"]
	_, hasType := m["activityType"]
	return hasID && hasType
}

// extraUntrustedKeys returns the per-shape set of additional free-form keys to
// wrap for one map node, detected structurally. The global untrustedKeys set
// stays narrow (it can't include `name`/`title`/`tag` without flooding
// high-trust server labels), so shapes carrying a counterparty-controlled
// free-form field under one of those generic keys opt in here.
func extraUntrustedKeys(m map[string]interface{}) map[string]bool {
	switch {
	case isAddressBookRecord(m):
		return untrustedKeysOnAddressBookRecord
	case isUserProfile(m):
		return untrustedKeysOnUserProfile
	case isActivity(m):
		return untrustedKeysOnActivity
	}
	return nil
}

// untrustedReplacer neutralizes the angle brackets a field value would need to
// forge an `<untrusted>` delimiter. Entity-encoding `<`/`>` (and `&` first, so
// the encoding is unambiguous) means a hostile value can neither close its own
// envelope early nor open a fake one — the agent always sees exactly one inert
// `<untrusted>…</untrusted>` pair regardless of the value's content.
var untrustedReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func walkAndWrap(v any, keysFor func(map[string]interface{}) map[string]bool) {
	switch x := v.(type) {
	case map[string]interface{}:
		extra := keysFor(x)
		for k, val := range x {
			if s, ok := val.(string); ok && (untrustedKeys[k] || extra[k]) && s != "" {
				x[k] = fmt.Sprintf("<untrusted source=%q>%s</untrusted>", k, untrustedReplacer.Replace(s))
				continue
			}
			walkAndWrap(val, keysFor)
		}
	case []interface{}:
		for _, item := range x {
			walkAndWrap(item, keysFor)
		}
	}
}
