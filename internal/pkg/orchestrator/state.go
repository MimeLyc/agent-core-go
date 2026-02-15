package orchestrator

import (
	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// State manages the conversation state during agent execution.
type State struct {
	// Messages contains the conversation history.
	Messages []llm.Message

	// Iterations tracks the number of loop iterations.
	Iterations int

	// InputTokens tracks cumulative input tokens.
	InputTokens int

	// OutputTokens tracks cumulative output tokens.
	OutputTokens int

	// ToolCalls records all tool calls made.
	ToolCalls []ToolCallRecord

	// LastResponse holds the most recent agent response.
	LastResponse llm.AgentResponse
}

// NewState creates a new conversation state with initial messages.
func NewState(messages []llm.Message) *State {
	return &State{
		Messages:  append([]llm.Message{}, messages...),
		ToolCalls: []ToolCallRecord{},
	}
}

// AddMessage appends a message to the conversation history.
func (s *State) AddMessage(msg llm.Message) {
	s.Messages = append(s.Messages, msg)
}

// AddToolCall records a tool call.
func (s *State) AddToolCall(name string, input map[string]any, result tools.ToolResult) {
	s.ToolCalls = append(s.ToolCalls, ToolCallRecord{
		Name:   name,
		Input:  input,
		Result: result,
	})
}

// UpdateUsage updates token usage statistics.
func (s *State) UpdateUsage(usage llm.Usage) {
	s.InputTokens += usage.InputTokens
	s.OutputTokens += usage.OutputTokens
}

// IncrementIteration increments the iteration counter.
func (s *State) IncrementIteration() {
	s.Iterations++
}

// ToResult converts the state to an OrchestratorResult.
func (s *State) ToResult() OrchestratorResult {
	var finalMessage llm.Message
	if len(s.Messages) > 0 {
		for i := len(s.Messages) - 1; i >= 0; i-- {
			if s.Messages[i].Role == llm.RoleAssistant {
				finalMessage = s.Messages[i]
				break
			}
		}
	}

	return OrchestratorResult{
		FinalMessage:      finalMessage,
		Messages:          s.Messages,
		TotalIterations:   s.Iterations,
		TotalInputTokens:  s.InputTokens,
		TotalOutputTokens: s.OutputTokens,
		ToolCalls:         s.ToolCalls,
	}
}
