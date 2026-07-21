package catalog

import (
	"encoding/json"
	"strings"
)

var externalTextKeysByRef = computeExternalTextKeys()

// ExternalTextKeys returns the property key names marked format:"external-text"
// anywhere in the response tree rooted at schemaRef (HelpEntry.ResponseRef),
// resolving $refs into components.schemas and descending arrays, nested objects
// and _embedded. These are user/counterparty free-text fields the MCP layer
// wraps in <untrusted> markers. Returns a fresh copy so callers can't mutate the
// cached set.
func ExternalTextKeys(schemaRef string) map[string]bool {
	src := externalTextKeysByRef[schemaRef]
	out := make(map[string]bool, len(src))
	for k := range src {
		out[k] = true
	}
	return out
}

func computeExternalTextKeys() map[string]map[string]bool {
	result := map[string]map[string]bool{}
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(Spec, &doc); err != nil {
		return result
	}
	schemas := make(map[string]map[string]any, len(doc.Components.Schemas))
	for name, raw := range doc.Components.Schemas {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			schemas[name] = m
		}
	}
	for name := range schemas {
		keys := map[string]bool{}
		collectExternalText(schemas, schemas[name], keys, map[string]bool{})
		if len(keys) > 0 {
			result[name] = keys
		}
	}
	return result
}

func collectExternalText(schemas map[string]map[string]any, node map[string]any, keys, visited map[string]bool) {
	if node == nil {
		return
	}
	if ref, ok := node["$ref"].(string); ok {
		name := ref[strings.LastIndex(ref, "/")+1:]
		if name == "" || visited[name] {
			return
		}
		visited[name] = true
		collectExternalText(schemas, schemas[name], keys, visited)
		return
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for key, raw := range props {
			def, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f, _ := def["format"].(string); f == "external-text" {
				keys[key] = true
			}
			collectExternalText(schemas, def, keys, visited)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		collectExternalText(schemas, items, keys, visited)
	}
}
