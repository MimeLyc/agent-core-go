// Package agent provides a unified interface for different agent implementations.
package agent

import (
	"context"
)

// Agent is the unified interface for all agent implementations.
// It abstracts the differences between local orchestrator and external agents.
type Agent interface {
	// Execute runs the agent with the given request and returns the result.
	Execute(ctx context.Context, req AgentRequest) (AgentResult, error)

	// Capabilities returns the agent's capabilities.
	Capabilities() AgentCapabilities

	// Close releases any resources held by the agent.
	Close() error
}

// AgentCapabilities describes what an agent can do.
type AgentCapabilities struct {
	// SupportsTools indicates if the agent can use tools.
	SupportsTools bool

	// AvailableTools lists the tools the agent can use.
	AvailableTools []ToolInfo

	// SupportsStreaming indicates if the agent supports streaming responses.
	SupportsStreaming bool

	// SupportsCompaction indicates if the agent supports context compaction.
	SupportsCompaction bool

	// MaxContextTokens is the maximum context window size.
	MaxContextTokens int

	// Provider identifies the agent implementation.
	// Examples: "api", "claude-code", "openai"
	Provider string
}

// ToolInfo describes a tool available to the agent.
type ToolInfo struct {
	Name        string
	Description string
}
