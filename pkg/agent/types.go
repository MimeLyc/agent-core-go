package agent

import (
	"context"
	"time"

	agenttypes "github.com/MimeLyc/agent-core-go/pkg/agent/types"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// AgentRequest contains all inputs for an agent execution.
type AgentRequest struct {
	// Task is the task description or prompt for the agent.
	Task string

	// SystemPrompt is the system message for the agent.
	SystemPrompt string

	// RepoInstructions contains repository instruction content.
	RepoInstructions string

	// SoulFile is the path to a SOUL.md file that defines the agent's character.
	// If empty, searches for SOUL.md in WorkDir then repo root.
	SoulFile string

	// WorkDir is the working directory for tool execution.
	WorkDir string

	// Options configures execution behavior.
	Options AgentOptions

	// Callbacks for monitoring the agent execution.
	Callbacks AgentCallbacks
}

// AgentOptions configures agent execution behavior.
type AgentOptions struct {
	// MaxIterations limits the number of agent loop iterations.
	MaxIterations int

	// DisableIterationLimit removes the loop iteration cap for this request.
	// This takes precedence over MaxIterations when true.
	DisableIterationLimit bool

	// EnableStreaming turns on incremental model output when supported.
	EnableStreaming bool

	// MaxTokens limits the response token count.
	MaxTokens int

	// TransformContext is an optional pre-LLM context transform hook.
	TransformContext func(ctx context.Context, messages []agenttypes.Message) ([]agenttypes.Message, error)

	// ConvertToLlm is an optional final conversion hook before provider call.
	// It converts agent messages into provider-facing LLM messages.
	ConvertToLlm func(ctx context.Context, messages []agenttypes.Message, providerName string) ([]agenttypes.LLMMessage, error)

	// DisableDefaultContextRules disables built-in compaction/truncation/validation.
	DisableDefaultContextRules bool

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// AllowedTools restricts which tools the agent can use.
	// Empty means all tools are allowed.
	AllowedTools []string

	// DeniedTools specifies tools the agent cannot use.
	DeniedTools []string

	// CompactConfig configures context compaction.
	CompactConfig *CompactConfig

	// GetSteeringMessages fetches high-priority runtime messages that can steer
	// the next model turn immediately.
	GetSteeringMessages LoopInputFetcher

	// GetFollowUpMessages fetches runtime follow-up messages appended after steering.
	GetFollowUpMessages LoopInputFetcher
}

// CompactConfig configures context compaction (summarization).
type CompactConfig struct {
	// Enabled turns on context compaction.
	Enabled bool

	// Threshold triggers compaction when message count exceeds this.
	Threshold int

	// KeepRecent is the number of recent messages to preserve.
	KeepRecent int
}

// AgentCallbacks provides hooks for monitoring agent execution.
type AgentCallbacks struct {
	// OnMessage is called when the agent produces a message.
	OnMessage func(agenttypes.Message)

	// OnToolCall is called when the agent invokes a tool.
	OnToolCall func(name string, input map[string]any)

	// OnToolResult is called when a tool returns a result.
	OnToolResult func(name string, result tools.ToolResult)

	// OnSteeringApplied is called when steering messages are injected.
	OnSteeringApplied func(messages []agenttypes.Message)

	// OnFollowUpApplied is called when follow-up messages are injected.
	OnFollowUpApplied func(messages []agenttypes.Message)

	// OnStreamDelta is called for incremental model text output.
	OnStreamDelta func(delta agenttypes.ContentBlockDelta)

	// OnIteration is called at the start of each iteration.
	OnIteration func(iteration int)
}

// LoopInputSnapshot describes the current loop state for runtime input providers.
type LoopInputSnapshot struct {
	Iteration      int
	MessageCount   int
	ToolCallCount  int
	LastStopReason agenttypes.StopReason
}

// LoopInputFetcher fetches runtime steering/follow-up messages.
type LoopInputFetcher func(ctx context.Context, snapshot LoopInputSnapshot) ([]agenttypes.Message, error)

// AgentResult contains the output of an agent execution.
type AgentResult struct {
	// Success indicates if the execution completed without error.
	Success bool

	// Summary is a brief description of what was done.
	Summary string

	// Message is the detailed response or explanation.
	Message string

	// FileChanges lists all file modifications made.
	FileChanges []FileChange

	// ToolCalls records all tool invocations.
	ToolCalls []ToolCallRecord

	// Usage contains token usage statistics.
	Usage ExecutionUsage

	// RawOutput contains the complete conversation (for debugging).
	RawOutput []agenttypes.Message
}

// FileChange represents a file modification.
type FileChange struct {
	// Path is the file path relative to the working directory.
	Path string

	// Content is the new file content.
	Content string

	// Operation describes the change type.
	Operation FileOperation
}

// FileOperation describes the type of file change.
type FileOperation string

const (
	FileOpCreate FileOperation = "create"
	FileOpModify FileOperation = "modify"
	FileOpDelete FileOperation = "delete"
)

// ToolCallRecord records a single tool invocation.
type ToolCallRecord struct {
	// Name is the tool name.
	Name string

	// Input is the tool input parameters.
	Input map[string]any

	// Output is the tool result content.
	Output string

	// IsError indicates if the tool returned an error.
	IsError bool

	// Duration is how long the tool took to execute.
	Duration time.Duration
}

// ExecutionUsage contains resource usage statistics.
type ExecutionUsage struct {
	// TotalIterations is the number of agent loop iterations.
	TotalIterations int

	// TotalInputTokens is the cumulative input token count.
	TotalInputTokens int

	// TotalOutputTokens is the cumulative output token count.
	TotalOutputTokens int

	// TotalDuration is the total execution time.
	TotalDuration time.Duration
}
