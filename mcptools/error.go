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
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (http %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

// ErrorResult wraps a Bron API error (or any error) into an MCP tool-error
// payload — the structured envelope (status, code, message, requestId) survives
// for the agent to branch on without parsing strings. All string fields go
// through output.SanitizeForTerminal because backend error messages can echo
// user-controlled input (e.g. "external id 'foo<script>' already taken") which
// a naive renderer might interpret.
func ErrorResult(err error) *mcp.CallToolResult {
	payload := map[string]any{}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.Status
		if apiErr.Code != "" {
			payload["code"] = output.SanitizeForTerminal(apiErr.Code)
		}
		payload["message"] = output.SanitizeForTerminal(apiErr.Message)
		if apiErr.RequestID != "" {
			payload["requestId"] = output.SanitizeForTerminal(apiErr.RequestID)
		}
	} else {
		payload["message"] = output.SanitizeForTerminal(err.Error())
	}
	b, _ := json.Marshal(payload)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}
