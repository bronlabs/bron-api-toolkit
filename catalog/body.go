package catalog

import "encoding/json"

var bodySchemas = computeBodySchemas()

type BodySchema struct {
	Properties map[string]bool
	Required   []string
}

func BodySchemaFor(schemaRef string) (BodySchema, bool) {
	s, ok := bodySchemas[schemaRef]
	return s, ok
}

func computeBodySchemas() map[string]BodySchema {
	out := map[string]BodySchema{}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
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
		props := make(map[string]bool, len(schema.Properties))
		for prop := range schema.Properties {
			props[prop] = true
		}
		out[name] = BodySchema{Properties: props, Required: schema.Required}
	}
	return out
}
