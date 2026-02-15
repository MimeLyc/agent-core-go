package orchestrator

import (
	"context"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// Orchestrator manages the agent loop with tool calling.
type Orchestrator interface {
	// Run executes the agent loop and returns the final result.
	Run(ctx context.Context, req OrchestratorRequest) (OrchestratorResult, error)
}

// OrchestratorRequest contains all inputs for an orchestrator run.
type OrchestratorRequest struct {
	// SystemPrompt is the system message for the agent.
	SystemPrompt string

	// RepoInstructions contains repository instruction content.
	// If non-empty, this is used directly instead of loading from files.
	RepoInstructions string

	// InstructionFiles overrides the default instruction file names
	// (e.g., []string{"AGENT.md", "AGENTS.md"}) when loading from the repository.
	// Ignored if RepoInstructions is already set.
	InstructionFiles []string

	// InitialMessages are the starting messages for the conversation.
	InitialMessages []llm.Message

	// Tools are the local tools available to the agent.
	Tools []tools.Tool

	// MCPServers are MCP server configurations to load additional tools from.
	MCPServers []MCPServerConfig

	// MaxIterations limits the number of agent loop iterations.
	// Non-positive values mean no iteration cap.
	MaxIterations int

	// DisableIterationLimit forces the loop to run without an iteration cap.
	// This takes precedence over MaxIterations when true.
	DisableIterationLimit bool

	// MaxMessages limits the conversation history size to avoid API limits.
	// When exceeded, older messages (except the first) are truncated.
	// Default: 50
	MaxMessages int

	// CompactConfig configures context compaction (summarization).
	// When enabled, long conversations are summarized instead of truncated.
	CompactConfig CompactConfig

	// EnableStreaming turns on provider streaming if supported.
	EnableStreaming bool

	// SoulFile is an explicit path to the SOUL.md file.
	// If empty, the orchestrator searches for SOUL.md in WorkDir then repo root.
	// Set to a non-existent path to disable SOUL loading entirely.
	SoulFile string

	// WorkDir is the working directory for tool execution.
	WorkDir string

	// ToolContext provides execution context for tools.
	ToolContext *tools.ToolContext

	// Runtime loop input providers. These are polled at key checkpoints.
	GetSteeringMessages LoopInputFetcher
	GetFollowUpMessages LoopInputFetcher

	// Callbacks for monitoring the agent loop.
	OnMessage         func(llm.Message)
	OnToolCall        func(name string, input map[string]any)
	OnToolResult      func(name string, result tools.ToolResult)
	OnSteeringApplied func(messages []llm.Message)
	OnFollowUpApplied func(messages []llm.Message)
	OnStreamDelta     func(delta llm.ContentBlockDelta)
}

// LoopInputSnapshot provides loop state to steering/follow-up providers.
type LoopInputSnapshot struct {
	Iteration      int
	MessageCount   int
	ToolCallCount  int
	LastStopReason llm.StopReason
}

// LoopInputFetcher loads runtime loop input messages.
type LoopInputFetcher func(ctx context.Context, snapshot LoopInputSnapshot) ([]llm.Message, error)

// MCPServerConfig configures an MCP server connection.
type MCPServerConfig struct {
	// Name is a unique identifier for the server.
	Name string

	// Command is the command to start the server (for stdio transport).
	Command string

	// Args are arguments for the server command.
	Args []string

	// URL is the server URL (for HTTP transport).
	URL string

	// Env contains environment variables for the server process.
	Env map[string]string
}

// OrchestratorResult contains the output of an orchestrator run.
type OrchestratorResult struct {
	// FinalMessage is the last assistant message.
	FinalMessage llm.Message

	// Messages contains the full conversation history.
	Messages []llm.Message

	// TotalIterations is the number of loop iterations executed.
	TotalIterations int

	// TotalInputTokens is the cumulative input token count.
	TotalInputTokens int

	// TotalOutputTokens is the cumulative output token count.
	TotalOutputTokens int

	// ToolCalls contains all tool calls made during execution.
	ToolCalls []ToolCallRecord
}

// ToolCallRecord records a single tool call and its result.
type ToolCallRecord struct {
	Name   string
	Input  map[string]any
	Result tools.ToolResult
}

// GetFinalText extracts the final text response from the result.
func (r OrchestratorResult) GetFinalText() string {
	return r.FinalMessage.GetText()
}
