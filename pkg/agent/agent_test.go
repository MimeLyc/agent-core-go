package agent

import (
	"context"
	"testing"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// mockAgentRunner is a mock implementation of llm.AgentRunner for testing.
type mockAgentRunner struct {
	responses []llm.AgentResponse
	callCount int
}

func (m *mockAgentRunner) Call(ctx context.Context, req llm.AgentRequest) (llm.AgentResponse, error) {
	if m.callCount >= len(m.responses) {
		return llm.AgentResponse{
			StopReason: llm.StopReasonEndTurn,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeText, Text: "done"},
			},
		}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func TestAPIAgentCapabilities(t *testing.T) {
	registry := tools.NewRegistry()

	runner := llm.AgentRunner{
		BaseURL:   "https://api.example.com",
		APIKey:    "test-key",
		Model:     "test-model",
		MaxTokens: 4096,
	}

	agent := NewAPIAgent(runner, registry, APIAgentOptions{
		MaxContextTokens: 128000,
	})
	caps := agent.Capabilities()

	if !caps.SupportsTools {
		t.Error("expected SupportsTools to be true")
	}
	if caps.Provider != "api" {
		t.Errorf("expected provider 'api', got %s", caps.Provider)
	}
	if !caps.SupportsCompaction {
		t.Error("expected SupportsCompaction to be true")
	}
	if caps.MaxContextTokens != 128000 {
		t.Errorf("expected MaxContextTokens 128000, got %d", caps.MaxContextTokens)
	}
}

func TestAPIAgentCapabilitiesDefaultContextTokens(t *testing.T) {
	registry := tools.NewRegistry()

	runner := llm.AgentRunner{
		BaseURL:   "https://api.example.com",
		APIKey:    "test-key",
		Model:     "test-model",
		MaxTokens: 4096,
	}

	agent := NewAPIAgent(runner, registry, APIAgentOptions{})
	caps := agent.Capabilities()

	if caps.MaxContextTokens != 0 {
		t.Errorf("expected MaxContextTokens 0 (unset), got %d", caps.MaxContextTokens)
	}
}

func TestRunnerAdapterConversion(t *testing.T) {
	req := llm.Request{
		Mode:         "task",
		RepoFullName: "owner/repo",
		TaskID:       "task-123",
		TaskTitle:    "Test task",
		TaskBody:     "Task body",
		TaskComments: []llm.Comment{
			{User: "user1", Body: "Comment 1"},
		},
	}

	agentReq := convertLLMRequest(req, "/tmp/workdir", "system prompt")

	if agentReq.WorkDir != "/tmp/workdir" {
		t.Errorf("expected workdir /tmp/workdir, got %s", agentReq.WorkDir)
	}
	if agentReq.Context.RepoFullName != "owner/repo" {
		t.Errorf("expected repo owner/repo, got %s", agentReq.Context.RepoFullName)
	}
	if agentReq.Context.TaskID != "task-123" {
		t.Errorf("expected task id task-123, got %s", agentReq.Context.TaskID)
	}
	if agentReq.Context.TaskTitle != "Test task" {
		t.Errorf("expected task title Test task, got %s", agentReq.Context.TaskTitle)
	}
	if len(agentReq.Context.TaskComments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(agentReq.Context.TaskComments))
	}
}

func TestConvertToRunResult(t *testing.T) {
	result := AgentResult{
		Success:  true,
		Decision: DecisionProceed,
		Summary:  "Test summary",
		FileChanges: []FileChange{
			{Path: "file.go", Content: "package main", Operation: FileOpModify},
		},
	}

	runResult := convertToRunResult(result)

	if runResult.Response.Decision != llm.DecisionProceed {
		t.Errorf("expected decision proceed, got %s", runResult.Response.Decision)
	}
	if runResult.Response.Summary != "Test summary" {
		t.Errorf("expected summary 'Test summary', got %s", runResult.Response.Summary)
	}
	if runResult.Response.Files["file.go"] != "package main" {
		t.Errorf("expected file content 'package main', got %s", runResult.Response.Files["file.go"])
	}
}

func TestAgentTypes(t *testing.T) {
	tests := []struct {
		agentType AgentType
		expected  string
	}{
		{AgentTypeAPI, "api"},
		{AgentTypeClaudeCode, "claude-code"},
		{AgentTypeAuto, "auto"},
	}

	for _, tt := range tests {
		if string(tt.agentType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.agentType)
		}
	}
}

func TestCompactConfig(t *testing.T) {
	cfg := &CompactConfig{
		Enabled:    true,
		Threshold:  30,
		KeepRecent: 10,
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.Threshold != 30 {
		t.Errorf("expected Threshold 30, got %d", cfg.Threshold)
	}
	if cfg.KeepRecent != 10 {
		t.Errorf("expected KeepRecent 10, got %d", cfg.KeepRecent)
	}
}

func TestExecutionUsage(t *testing.T) {
	usage := ExecutionUsage{
		TotalIterations:   5,
		TotalInputTokens:  1000,
		TotalOutputTokens: 500,
		TotalDuration:     10 * time.Second,
	}

	if usage.TotalIterations != 5 {
		t.Errorf("expected 5 iterations, got %d", usage.TotalIterations)
	}
	if usage.TotalInputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", usage.TotalInputTokens)
	}
	if usage.TotalOutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", usage.TotalOutputTokens)
	}
}

func TestFileChange(t *testing.T) {
	fc := FileChange{
		Path:      "pkg/main.go",
		Content:   "package main\n\nfunc main() {}",
		Operation: FileOpCreate,
	}

	if fc.Path != "pkg/main.go" {
		t.Errorf("expected path pkg/main.go, got %s", fc.Path)
	}
	if fc.Operation != FileOpCreate {
		t.Errorf("expected operation create, got %s", fc.Operation)
	}
}

func TestToolCallRecord(t *testing.T) {
	record := ToolCallRecord{
		Name:     "read_file",
		Input:    map[string]any{"path": "main.go"},
		Output:   "file contents",
		IsError:  false,
		Duration: 100 * time.Millisecond,
	}

	if record.Name != "read_file" {
		t.Errorf("expected name read_file, got %s", record.Name)
	}
	if record.IsError {
		t.Error("expected IsError to be false")
	}
}

func TestAgentCallbacks(t *testing.T) {
	var messageCalled, toolCallCalled, toolResultCalled bool

	callbacks := AgentCallbacks{
		OnMessage: func(msg llm.Message) {
			messageCalled = true
		},
		OnToolCall: func(name string, input map[string]any) {
			toolCallCalled = true
		},
		OnToolResult: func(name string, result tools.ToolResult) {
			toolResultCalled = true
		},
	}

	// Simulate callbacks
	callbacks.OnMessage(llm.Message{})
	callbacks.OnToolCall("test", nil)
	callbacks.OnToolResult("test", tools.ToolResult{})

	if !messageCalled {
		t.Error("OnMessage was not called")
	}
	if !toolCallCalled {
		t.Error("OnToolCall was not called")
	}
	if !toolResultCalled {
		t.Error("OnToolResult was not called")
	}
}
