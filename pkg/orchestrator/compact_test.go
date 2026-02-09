package orchestrator

import (
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

func TestShouldCompact(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		threshold  int
		msgCount   int
		wantResult bool
	}{
		{"disabled", false, 10, 20, false},
		{"under threshold", true, 30, 20, false},
		{"at threshold", true, 30, 30, false},
		{"over threshold", true, 30, 31, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compactor{
				config: CompactConfig{
					Enabled:   tt.enabled,
					Threshold: tt.threshold,
				},
			}
			messages := make([]llm.Message, tt.msgCount)
			if got := c.ShouldCompact(messages); got != tt.wantResult {
				t.Errorf("ShouldCompact() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestFormatMessagesForSummary(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "Hello"),
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeText, Text: "Let me check"},
				{Type: llm.ContentTypeToolUse, ID: "t1", Name: "read_file"},
			},
		},
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "t1", Content: "file content"},
			},
		},
	}

	result := formatMessagesForSummary(messages)

	// Check that the summary contains expected elements
	if len(result) == 0 {
		t.Error("formatMessagesForSummary returned empty string")
	}
	if !contains(result, "Hello") {
		t.Error("summary should contain user message")
	}
	if !contains(result, "Let me check") {
		t.Error("summary should contain assistant text")
	}
	if !contains(result, "read_file") {
		t.Error("summary should contain tool name")
	}
	if !contains(result, "file content") {
		t.Error("summary should contain tool result")
	}
}

func TestEnsureToolPairsIntact(t *testing.T) {
	olderMessages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "Initial"),
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolUse, ID: "old_tool", Name: "bash"},
			},
		},
	}

	recentMessages := []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "old_tool", Content: "output"},
			},
		},
		llm.NewTextMessage(llm.RoleAssistant, "Done"),
	}

	result := ensureToolPairsIntact(recentMessages, olderMessages)

	// Should include the older message with tool_use
	if len(result) <= len(recentMessages) {
		t.Errorf("expected more messages, got %d", len(result))
	}

	// Verify tool_use is now included
	hasToolUse := false
	for _, msg := range result {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse && block.ID == "old_tool" {
				hasToolUse = true
			}
		}
	}
	if !hasToolUse {
		t.Error("tool_use should be included to match tool_result")
	}
}

func TestDefaultCompactConfig(t *testing.T) {
	cfg := DefaultCompactConfig()
	if !cfg.Enabled {
		t.Error("default should be enabled")
	}
	if cfg.Threshold <= 0 {
		t.Error("threshold should be positive")
	}
	if cfg.KeepRecent <= 0 {
		t.Error("keep_recent should be positive")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (len(s) >= len(substr)) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
