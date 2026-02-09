package llm

import (
	"context"
	"fmt"
)

// LLMProvider is the unified interface for LLM API calls.
// It abstracts the differences between Claude API, OpenAI API, and other LLM providers.
type LLMProvider interface {
	// Call sends a request to the LLM and returns the response.
	// This is equivalent to the existing AgentRunner.Call() method.
	Call(ctx context.Context, req AgentRequest) (AgentResponse, error)

	// Name returns the provider name (e.g., "claude", "openai").
	Name() string
}

// LLMProviderType identifies the LLM provider backend.
type LLMProviderType string

const (
	// ProviderClaude uses the Claude API (Anthropic).
	ProviderClaude LLMProviderType = "claude"

	// ProviderOpenAI uses OpenAI-compatible APIs (OpenAI, OpenRouter, DeepSeek, etc.).
	ProviderOpenAI LLMProviderType = "openai"
)

// LLMProviderConfig contains configuration for creating an LLM provider.
type LLMProviderConfig struct {
	// Type specifies which provider to use.
	Type LLMProviderType

	// BaseURL is the API base URL.
	BaseURL string

	// APIKey is the API authentication key.
	APIKey string

	// Model is the model identifier.
	Model string

	// MaxTokens limits the response token count.
	MaxTokens int

	// Timeout is the request timeout in seconds.
	TimeoutSeconds int

	// MaxAttempts is the maximum retry count.
	MaxAttempts int
}

// NewLLMProvider creates an LLM provider based on the configuration.
func NewLLMProvider(cfg LLMProviderConfig) (LLMProvider, error) {
	switch cfg.Type {
	case ProviderClaude:
		return NewClaudeProvider(cfg), nil
	case ProviderOpenAI:
		return NewOpenAIProvider(cfg), nil
	case "":
		// Default to Claude if not specified
		return NewClaudeProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider type: %s", cfg.Type)
	}
}
