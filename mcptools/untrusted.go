package mcptools

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/bronlabs/bron-api-toolkit/output"
)

type WrapOptions struct {
	Keys     map[string]bool
	Paths    map[string]bool
	SwitchAt map[string]string
}

func WrapUntrustedFields(v any) any {
	return WrapUntrusted(v, WrapOptions{})
}

func WrapUntrustedFieldsWithKeys(v any, keys map[string]bool) any {
	return WrapUntrusted(v, WrapOptions{Keys: keys})
}

func WrapUntrusted(v any, opts WrapOptions) any {
	return transform(v, "", opts)
}

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

var untrustedKeysOnAddressBookRecord = map[string]bool{"name": true, "tag": true}

var untrustedKeysOnUserProfile = map[string]bool{"name": true}

var untrustedKeysOnActivity = map[string]bool{"title": true}

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

var delimiterPattern = regexp.MustCompile(`(?i)<\s*/?\s*untrusted`)

func neutralize(s string) string {
	s = output.StripInvisible(s)
	if !strings.ContainsRune(s, '<') {
		return s
	}

	return delimiterPattern.ReplaceAllStringFunc(s, func(m string) string { return "&lt;" + m[1:] })
}

func envelope(key, s string) string {
	s = neutralize(s)
	if s == "" {
		return s
	}

	return fmt.Sprintf("<untrusted source=%q>%s</untrusted>", neutralize(key), s)
}

func (o WrapOptions) matches(key, path string, node map[string]interface{}) bool {
	if untrustedKeys[key] || o.Keys[key] {
		return true
	}
	if o.Paths[path] {
		return true
	}
	return extraUntrustedKeys(node)[key]
}

func (o WrapOptions) at(path string) (WrapOptions, string) {
	ref, ok := o.SwitchAt[path]
	if !ok {
		return o, path
	}

	return WrapOptions{Keys: o.Keys, Paths: catalog.ExternalTextPaths(ref), SwitchAt: o.SwitchAt}, ""
}

func transform(v any, path string, opts WrapOptions) any {
	switch x := v.(type) {
	case map[string]interface{}:
		// EmbedAssetsIntoTxs attaches one asset map to every transaction referencing
		// it; mutating in place would nest the envelope once per reference.
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			child := path + "." + k
			if path == "" {
				child = k
			}

			wrapAs := ""
			if opts.matches(k, child, x) {
				wrapAs = k
			}
			childOpts, childPath := opts.at(child)

			switch val := val.(type) {
			case string:
				out[k] = mark(val, wrapAs)
			case []interface{}:
				out[k] = transformArray(val, childPath, childOpts, wrapAs)
			default:
				out[k] = transform(val, childPath, childOpts)
			}
		}
		return out

	case []interface{}:
		return transformArray(x, path, opts, "")

	case string:
		return neutralize(x)
	}

	return v
}

func transformArray(arr []interface{}, path string, opts WrapOptions, wrapAs string) []interface{} {
	out := make([]interface{}, len(arr))
	for i, item := range arr {
		switch item := item.(type) {
		case string:
			out[i] = mark(item, wrapAs)
		case []interface{}:
			out[i] = transformArray(item, path, opts, wrapAs)
		default:
			out[i] = transform(item, path, opts)
		}
	}
	return out
}

func mark(s, wrapAs string) string {
	if wrapAs == "" {
		return neutralize(s)
	}

	return envelope(wrapAs, s)
}
