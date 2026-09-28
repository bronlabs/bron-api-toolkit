package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bronlabs/bron-api-toolkit/output"
)

// APIError is the toolkit's own structured Bron API error. Consumers adapt
// their transport error into it at the boundary (bron-cli maps sdk/http's
// APIError; desktop builds it from its parsed response) so the lib never has to
// import sdk/http — which would pull sdk/auth + JWT into every consumer.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Embedded  map[string]any
}

func envelopeKeyFor(key string) string {
	if untrustedKeys[key] {
		return key
	}

	return ""
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (http %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

func ErrorResult(err error) *mcp.CallToolResult {
	payload := map[string]any{}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.Status
		if apiErr.Code != "" {
			// The agent branches on `code` and `requestId`; an envelope around either
			// would break that discriminator, so they stay bare.
			payload["code"] = output.SanitizeForTerminal(apiErr.Code)
		}
		payload["message"] = envelope("message", output.SanitizeForTerminal(apiErr.Message))
		if apiErr.RequestID != "" {
			payload["requestId"] = output.SanitizeForTerminal(apiErr.RequestID)
		}
		if len(apiErr.Embedded) > 0 {
			embedded := make(map[string]any, len(apiErr.Embedded))
			for k, v := range apiErr.Embedded {
				key := output.SanitizeForTerminal(k)
				embedded[key] = mark(output.SanitizeForTerminal(fmt.Sprint(v)), envelopeKeyFor(key))
			}
			payload["_embedded"] = embedded
		}
	} else {
		payload["message"] = envelope("message", output.SanitizeForTerminal(err.Error()))
	}
	b, _ := json.Marshal(payload)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}
