package orchestrator

import (
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

func TestNewState(t *testing.T) {
	initial := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "hello"),
	}

	state := NewState(initial)

	if len(state.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(state.Messages))
	}
	if state.Messages[0].GetText() != "hello" {
		t.Errorf("Messages[0].GetText() = %q, want hello", state.Messages[0].GetText())
	}
	if state.Iterations != 0 {
		t.Errorf("Iterations = %d, want 0", state.Iterations)
	}
}

func TestStateAddMessage(t *testing.T) {
	state := NewState(nil)

	state.AddMessage(llm.NewTextMessage(llm.RoleUser, "first"))
	state.AddMessage(llm.NewTextMessage(llm.RoleAssistant, "second"))

	if len(state.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(state.Messages))
	}
}

func TestStateAddToolCall(t *testing.T) {
	state := NewState(nil)

	result := tools.NewToolResult("file content")
	state.AddToolCall("read_file", map[string]any{"path": "test.txt"}, result)

	if len(state.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(state.ToolCalls))
	}
	if state.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want read_file", state.ToolCalls[0].Name)
	}
}

func TestStateUpdateUsage(t *testing.T) {
	state := NewState(nil)

	state.UpdateUsage(llm.Usage{InputTokens: 100, OutputTokens: 50})
	state.UpdateUsage(llm.Usage{InputTokens: 200, OutputTokens: 100})

	if state.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", state.InputTokens)
	}
	if state.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", state.OutputTokens)
	}
}

func TestStateIncrementIteration(t *testing.T) {
	state := NewState(nil)

	state.IncrementIteration()
	state.IncrementIteration()

	if state.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2", state.Iterations)
	}
}

func TestStateToResult(t *testing.T) {
	state := NewState(nil)
	state.AddMessage(llm.NewTextMessage(llm.RoleUser, "hello"))
	state.AddMessage(llm.NewTextMessage(llm.RoleAssistant, "world"))
	state.IncrementIteration()
	state.UpdateUsage(llm.Usage{InputTokens: 100, OutputTokens: 50})
	state.AddToolCall("test", map[string]any{}, tools.NewToolResult("ok"))

	result := state.ToResult()

	if result.TotalIterations != 1 {
		t.Errorf("TotalIterations = %d, want 1", result.TotalIterations)
	}
	if result.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", result.TotalInputTokens)
	}
	if result.TotalOutputTokens != 50 {
		t.Errorf("TotalOutputTokens = %d, want 50", result.TotalOutputTokens)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("ToolCalls len = %d, want 1", len(result.ToolCalls))
	}
	if result.FinalMessage.GetText() != "world" {
		t.Errorf("FinalMessage.GetText() = %q, want world", result.FinalMessage.GetText())
	}
}

func TestStateToResultNoAssistantMessage(t *testing.T) {
	state := NewState(nil)
	state.AddMessage(llm.NewTextMessage(llm.RoleUser, "hello"))

	result := state.ToResult()

	if result.FinalMessage.GetText() != "" {
		t.Errorf("FinalMessage.GetText() = %q, want empty", result.FinalMessage.GetText())
	}
}
