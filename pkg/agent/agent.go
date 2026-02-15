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

	// ExecuteStream runs the agent and emits structured stream events.
	// Implementations may fall back to coarse-grained events if token streaming
	// is not supported by the underlying provider.
	ExecuteStream(ctx context.Context, req AgentRequest) (<-chan AgentStreamEvent, <-chan error)

	// Capabilities returns the agent's capabilities.
	Capabilities() AgentCapabilities

	// Close releases any resources held by the agent.
	Close() error
}

// AgentEventType identifies stream event categories.
type AgentEventType string

const (
	AgentEventAgentStart      AgentEventType = "agent_start"
	AgentEventMessageDelta    AgentEventType = "message_delta"
	AgentEventMessageEnd      AgentEventType = "message_end"
	AgentEventToolCall        AgentEventType = "tool_call"
	AgentEventToolResult      AgentEventType = "tool_result"
	AgentEventSteeringApplied AgentEventType = "steering_applied"
	AgentEventFollowUpApplied AgentEventType = "followup_applied"
	AgentEventAgentEnd        AgentEventType = "agent_end"
)

// AgentStreamEvent is a structured streaming event emitted during execution.
type AgentStreamEvent struct {
	Type     AgentEventType  `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	Message  string          `json:"message,omitempty"`
	ToolName string          `json:"tool_name,omitempty"`
	IsError  bool            `json:"is_error,omitempty"`
	Usage    *ExecutionUsage `json:"usage,omitempty"`
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
