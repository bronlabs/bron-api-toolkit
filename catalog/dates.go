package catalog

import "encoding/json"

var dateKeys = computeDateKeys()

// DateKeys returns the set of field/parameter names the spec marks with
// `format: "date-time-millis"` (the OpenAPI translation of the backend's
// `@EpochMillis` marker). Consumers wire it into request-side coercion
// (qparam) and response-side humanization (output). Returns a fresh copy so
// callers can't mutate the cached set.
func DateKeys() map[string]bool {
	out := make(map[string]bool, len(dateKeys))
	for k := range dateKeys {
		out[k] = true
	}
	return out
}

func computeDateKeys() map[string]bool {
	keys := map[string]bool{}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Format string `json:"format"`
					Items  struct {
						Format string `json:"format"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name   string `json:"name"`
				Schema struct {
					Format string `json:"format"`
					Items  struct {
						Format string `json:"format"`
					} `json:"items"`
				} `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(Spec, &spec); err != nil {
		return keys
	}
	for _, schema := range spec.Components.Schemas {
		for name, prop := range schema.Properties {
			if prop.Format == "date-time-millis" || prop.Items.Format == "date-time-millis" {
				keys[name] = true
			}
		}
	}
	for _, methods := range spec.Paths {
		for _, op := range methods {
			for _, p := range op.Parameters {
				if p.Schema.Format == "date-time-millis" || p.Schema.Items.Format == "date-time-millis" {
					keys[p.Name] = true
				}
			}
		}
	}
	return keys
}
