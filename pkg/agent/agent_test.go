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
				{Type: llm.ContentTypeText, Text: `{"decision":"proceed","summary":"done"}`},
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

	agent := NewAPIAgent(runner, registry, APIAgentOptions{})
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
}

func TestBuildUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		req      AgentRequest
		contains []string
	}{
		{
			name: "with task",
			req: AgentRequest{
				Task: "Fix the bug",
			},
			contains: []string{"Fix the bug"},
		},
		{
			name: "with issue context",
			req: AgentRequest{
				Context: AgentContext{
					RepoFullName: "owner/repo",
					IssueNumber:  123,
					IssueTitle:   "Bug in login",
					IssueBody:    "Login fails",
				},
			},
			contains: []string{"owner/repo", "Task Context", "Task ID: issue-123", "Bug in login", "Login fails"},
		},
		{
			name: "with PR context",
			req: AgentRequest{
				Context: AgentContext{
					PRNumber:  456,
					PRTitle:   "Fix login",
					PRHeadRef: "fix-login",
					PRBaseRef: "main",
				},
			},
			contains: []string{"Task Context", "Task ID: pr-456", "Fix login", "fix-login", "main"},
		},
		{
			name: "with generic task context",
			req: AgentRequest{
				Context: AgentContext{
					TaskID:    "task-789",
					TaskTitle: "Migrate SDK",
					TaskBody:  "Copy generic packages",
				},
			},
			contains: []string{"Task Context", "Task ID: task-789", "Migrate SDK", "Copy generic packages"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildUserPrompt(tt.req)
			for _, s := range tt.contains {
				if !containsString(prompt, s) {
					t.Errorf("expected prompt to contain %q, got: %s", s, prompt)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || containsString(s[1:], substr)))
}

func TestParseStructuredResponse(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		decision Decision
		summary  string
	}{
		{
			name:     "proceed decision",
			text:     `{"decision":"proceed","summary":"Changes made"}`,
			decision: DecisionProceed,
			summary:  "Changes made",
		},
		{
			name:     "needs_info decision",
			text:     `{"decision":"needs_info","summary":"Need more details"}`,
			decision: DecisionNeedsInfo,
			summary:  "Need more details",
		},
		{
			name:     "stop decision",
			text:     `{"decision":"stop","summary":"Cannot automate"}`,
			decision: DecisionStop,
			summary:  "Cannot automate",
		},
		{
			name:     "plain text",
			text:     "Just some text without JSON",
			decision: DecisionProceed,
			summary:  "Just some text without JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AgentResult{}
			parseStructuredResponse(&result, tt.text)
			if result.Decision != tt.decision {
				t.Errorf("expected decision %s, got %s", tt.decision, result.Decision)
			}
		})
	}
}

func TestRunnerAdapterConversion(t *testing.T) {
	req := llm.Request{
		Mode:         "issue",
		RepoFullName: "owner/repo",
		TaskID:       "task-123",
		TaskTitle:    "Test task",
		TaskBody:     "Task body",
		IssueNumber:  123,
		IssueTitle:   "Test issue",
		IssueBody:    "Issue body",
		IssueLabels:  []string{"bug"},
		IssueComments: []llm.Comment{
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
	if agentReq.Context.IssueNumber != 123 {
		t.Errorf("expected issue 123, got %d", agentReq.Context.IssueNumber)
	}
	if len(agentReq.Context.IssueComments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(agentReq.Context.IssueComments))
	}
}

func TestConvertToRunResult(t *testing.T) {
	result := AgentResult{
		Success:       true,
		Decision:      DecisionProceed,
		Summary:       "Test summary",
		CommitMessage: "fix: test commit",
		PRTitle:       "Test PR",
		PRBody:        "PR body",
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
