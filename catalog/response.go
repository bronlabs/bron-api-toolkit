package catalog

import "encoding/json"

var responseEnvelopeKeys = computeResponseEnvelopeKeys()

// ResponseEnvelopeArrayKey returns the property name of a list-envelope response
// schema — an object component whose properties contain exactly one array. The
// second result is false for non-envelope schemas (get-by-id DTOs, multi-array
// or non-object shapes). schemaRef is the component name (HelpEntry.ResponseRef).
func ResponseEnvelopeArrayKey(schemaRef string) (string, bool) {
	key, ok := responseEnvelopeKeys[schemaRef]
	return key, ok
}

func computeResponseEnvelopeKeys() map[string]string {
	out := map[string]string{}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Type       string `json:"type"`
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(Spec, &spec); err != nil {
		return out
	}
	for name, schema := range spec.Components.Schemas {
		if schema.Type != "object" {
			continue
		}
		arrayKey := ""
		count := 0
		for prop, def := range schema.Properties {
			if def.Type == "array" {
				arrayKey = prop
				count++
			}
		}
		if count == 1 {
			out[name] = arrayKey
		}
	}
	return out
}
