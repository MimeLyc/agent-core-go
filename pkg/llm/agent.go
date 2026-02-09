package llm

import (
	"context"
	"net/http"
	"time"
)

// AgentRunner is a backward-compatible wrapper around ClaudeProvider.
// It invokes the Claude Messages API with tool support.
// Deprecated: Use ClaudeProvider directly via the LLMProvider interface.
type AgentRunner struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Timeout     time.Duration
	MaxAttempts int
	HTTPClient  *http.Client
	Backoff     func(int) time.Duration
	Sleep       func(time.Duration)
}

// Call sends an AgentRequest to the Claude API and returns the response.
// This method delegates to ClaudeProvider for the actual implementation.
func (r AgentRunner) Call(ctx context.Context, req AgentRequest) (AgentResponse, error) {
	provider := &ClaudeProvider{
		BaseURL:     r.BaseURL,
		APIKey:      r.APIKey,
		Model:       r.Model,
		MaxTokens:   r.MaxTokens,
		Timeout:     r.Timeout,
		MaxAttempts: r.MaxAttempts,
		HTTPClient:  r.HTTPClient,
		Backoff:     r.Backoff,
		Sleep:       r.Sleep,
	}
	return provider.Call(ctx, req)
}

// Name returns the provider name for LLMProvider interface compatibility.
func (r AgentRunner) Name() string {
	return "claude"
}

// BuildToolDefinitions converts tools to ToolDefinition format for the API.
func BuildToolDefinitions(tools []ToolSpec) []ToolDefinition {
	defs := make([]ToolDefinition, len(tools))
	for i, t := range tools {
		defs[i] = ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return defs
}

// ToolSpec is a simplified tool specification for building definitions.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}
