package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bronlabs/bron-api-toolkit/output"
)

type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Embedded  map[string]any
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
			payload["code"] = neutralize(output.SanitizeForTerminal(apiErr.Code))
		}
		payload["message"] = envelope("message", output.SanitizeForTerminal(apiErr.Message))
		if apiErr.RequestID != "" {
			payload["requestId"] = neutralize(output.SanitizeForTerminal(apiErr.RequestID))
		}
		if len(apiErr.Embedded) > 0 {
			if embedded, err := genericTree(apiErr.Embedded); err == nil {
				payload["_embedded"] = transform(embedded, "", WrapOptions{})
			}
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
