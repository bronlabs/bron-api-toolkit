package mcptools

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrorResult must preserve the structured fields both adapters feed it
// (bron-cli's sdk/http mapping and desktop's apiCallError), including through a
// wrapped error chain like the WS composites produce.
func TestErrorResultPreservesStructuredFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"direct", &APIError{Status: 429, Code: "rate-limited", Message: "slow down", RequestID: "req-123"}},
		{"wrapped", fmt.Errorf("subscribe: %w", &APIError{Status: 429, Code: "rate-limited", Message: "slow down", RequestID: "req-123"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := ErrorResult(tc.err)
			if !res.IsError {
				t.Fatal("expected IsError")
			}
			text, ok := res.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
			}
			var p map[string]any
			if err := json.Unmarshal([]byte(text.Text), &p); err != nil {
				t.Fatalf("payload not JSON: %v", err)
			}
			if p["status"] != float64(429) {
				t.Fatalf("status = %v, want 429", p["status"])
			}
			if p["code"] != "rate-limited" {
				t.Fatalf("code = %v, want rate-limited", p["code"])
			}
			if p["message"] != "slow down" {
				t.Fatalf("message = %v, want slow down", p["message"])
			}
			if p["requestId"] != "req-123" {
				t.Fatalf("requestId = %v, want req-123", p["requestId"])
			}
		})
	}
}
