package mcptools

import (
	"context"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bronlabs/bron-api-toolkit/catalog"
)

// RegisterHelp adds the read-only `bron_help` tool: static data-model help with
// no network access. It is not a spec endpoint, so it registers directly rather
// than through RegisterSpecDriven.
func RegisterHelp(server *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "bron_help",
		Description: helpToolDescription(),
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"topic": {
					Type:        "string",
					Description: "Optional topic id. Omit for the whole catalog. One of: " + strings.Join(helpTopicNames(), ", ") + ".",
				},
			},
			AdditionalProperties: &jsonschema.Schema{},
		},
	}
	mcp.AddTool(server, tool, func(_ context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		if topic := StringValue(in["topic"]); topic != "" {
			entry, ok := catalog.HelpTopicByName(topic)
			if !ok {
				return toolResult(map[string]any{"error": "unknown topic", "topics": helpTopicNames()})
			}
			return toolResult(helpEntryMap(entry))
		}
		entries := make([]any, 0, len(catalog.HelpTopics))
		for _, e := range catalog.HelpTopics {
			entries = append(entries, helpEntryMap(e))
		}
		return toolResult(map[string]any{"topics": entries})
	})
}

func helpEntryMap(e catalog.HelpTopic) map[string]any {
	return map[string]any{"topic": e.Topic, "title": e.Title, "details": e.Details}
}

func helpTopicNames() []string {
	out := make([]string, 0, len(catalog.HelpTopics))
	for _, e := range catalog.HelpTopics {
		out = append(out, e.Topic)
	}
	return out
}

func helpToolDescription() string {
	return "Data-model help for the Bron API tools — read-only, no network. Call with no arguments for the full catalog, or pass `topic` for one entry. Topics: " + strings.Join(helpTopicNames(), ", ") + "."
}
