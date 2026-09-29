package catalog

import (
	"encoding/json"
	"slices"
	"strings"
)

var externalTextPathsByRef = computeExternalTextPaths()

func ExternalTextPaths(schemaRef string) map[string]bool {
	src := externalTextPathsByRef[schemaRef]
	out := make(map[string]bool, len(src))
	for k := range src {
		out[k] = true
	}
	return out
}

func ExternalTextNames() map[string]bool {
	out := map[string]bool{}
	for _, paths := range externalTextPathsByRef {
		for p := range paths {
			out[p[strings.LastIndex(p, ".")+1:]] = true
		}
	}
	return out
}

func computeExternalTextPaths() map[string]map[string]bool {
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
		paths := map[string]bool{}
		collectExternalText(schemas, schemas[name], "", nil, paths)
		if len(paths) > 0 {
			result[name] = paths
		}
	}
	return result
}

func collectExternalText(schemas map[string]map[string]any, node map[string]any, prefix string, ancestry []string, paths map[string]bool) {
	if node == nil {
		return
	}

	if ref, ok := node["$ref"].(string); ok {
		name := ref[strings.LastIndex(ref, "/")+1:]
		if name == "" || slices.Contains(ancestry, name) {
			return
		}
		collectExternalText(schemas, schemas[name], prefix, append(slices.Clip(ancestry), name), paths)
		return
	}

	if props, ok := node["properties"].(map[string]any); ok {
		for key, raw := range props {
			def, ok := raw.(map[string]any)
			if !ok {
				continue
			}

			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if f, _ := def["format"].(string); f == "external-text" {
				paths[path] = true
			}

			collectExternalText(schemas, def, path, ancestry, paths)
		}
	}

	if items, ok := node["items"].(map[string]any); ok {
		collectExternalText(schemas, items, prefix, ancestry, paths)
	}

	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		arr, ok := node[keyword].([]any)
		if !ok {
			continue
		}
		for _, raw := range arr {
			if sub, ok := raw.(map[string]any); ok {
				collectExternalText(schemas, sub, prefix, ancestry, paths)
			}
		}
	}
}
