package orchestrator

import (
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

func TestTruncateMessagesPreservesToolPairs(t *testing.T) {
	// Create a conversation with tool_use and tool_result pairs
	messages := []llm.Message{
		// Message 0: Initial user prompt
		llm.NewTextMessage(llm.RoleUser, "Hello"),
		// Message 1: Assistant with tool_use
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeText, Text: "Let me check"},
				{Type: llm.ContentTypeToolUse, ID: "tool1", Name: "read_file", Input: map[string]any{"path": "a.txt"}},
			},
		},
		// Message 2: User with tool_result
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "tool1", Content: "file content"},
			},
		},
		// Message 3: Assistant with another tool_use
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolUse, ID: "tool2", Name: "read_file", Input: map[string]any{"path": "b.txt"}},
			},
		},
		// Message 4: User with tool_result
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "tool2", Content: "another content"},
			},
		},
		// Message 5: Assistant text
		llm.NewTextMessage(llm.RoleAssistant, "Done"),
	}

	// Try to truncate to 4 messages
	result := truncateMessages(messages, 4)

	// Verify that tool_use/tool_result pairs are preserved
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range result {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse {
				toolUseIDs[block.ID] = true
			}
			if block.Type == llm.ContentTypeToolResult {
				toolResultIDs[block.ToolUseID] = true
			}
		}
	}

	// Every tool_result should have a corresponding tool_use
	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			t.Errorf("tool_result with ID %s has no corresponding tool_use", id)
		}
	}

	t.Logf("truncated from %d to %d messages", len(messages), len(result))
}

func TestTruncateMessagesNoTruncationNeeded(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "Hello"),
		llm.NewTextMessage(llm.RoleAssistant, "Hi"),
	}

	result := truncateMessages(messages, 10)

	if len(result) != len(messages) {
		t.Errorf("expected %d messages, got %d", len(messages), len(result))
	}
}

func TestTruncateMessagesKeepsFirstMessage(t *testing.T) {
	messages := make([]llm.Message, 20)
	messages[0] = llm.NewTextMessage(llm.RoleUser, "Initial prompt")
	for i := 1; i < 20; i++ {
		if i%2 == 1 {
			messages[i] = llm.NewTextMessage(llm.RoleAssistant, "Response")
		} else {
			messages[i] = llm.NewTextMessage(llm.RoleUser, "Follow up")
		}
	}

	result := truncateMessages(messages, 10)

	// First message should be preserved
	if result[0].GetText() != "Initial prompt" {
		t.Errorf("first message was not preserved")
	}
}

func TestTruncateMessagesNestedDependencies(t *testing.T) {
	// Test case: nested tool pairs where truncation initially would break a pair,
	// and including that pair exposes another broken pair.
	// Message sequence:
	// 0: user prompt
	// 1: assistant tool_use(A)
	// 2: user tool_result(A)
	// 3: assistant tool_use(B)
	// 4: user tool_result(B)
	// 5: assistant tool_use(C)
	// 6: user tool_result(C)
	// 7: assistant text
	//
	// With maxMessages=4, initial keepFrom=5 (messages 0,5,6,7)
	// But msg 6 has tool_result(C), needs tool_use(C) at msg 5 - OK
	// Now if we had started at keepFrom=4 (messages 0,4,5,6,7=5 messages)
	// Msg 4 has tool_result(B), needs tool_use(B) at msg 3
	// Include msg 3 -> keepFrom=3
	// Now we have (0,3,4,5,6,7=6 messages)

	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "Hello"), // 0
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolUse, ID: "toolA", Name: "read", Input: map[string]any{}},
			},
		}, // 1
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "toolA", Content: "result A"},
			},
		}, // 2
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolUse, ID: "toolB", Name: "read", Input: map[string]any{}},
			},
		}, // 3
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "toolB", Content: "result B"},
			},
		}, // 4
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolUse, ID: "toolC", Name: "read", Input: map[string]any{}},
			},
		}, // 5
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: "toolC", Content: "result C"},
			},
		}, // 6
		llm.NewTextMessage(llm.RoleAssistant, "Done"), // 7
	}

	// Try to truncate to 5 messages (0 + 4 recent)
	// Initial keepFrom = 8 - 5 + 1 = 4
	// Messages would be [0, 4, 5, 6, 7]
	// Msg 4 has tool_result(B), needs tool_use(B) at msg 3
	result := truncateMessages(messages, 5)

	// Verify all tool pairs are preserved
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range result {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse {
				toolUseIDs[block.ID] = true
			}
			if block.Type == llm.ContentTypeToolResult {
				toolResultIDs[block.ToolUseID] = true
			}
		}
	}

	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			t.Errorf("tool_result with ID %s has no corresponding tool_use", id)
		}
	}

	t.Logf("truncated from %d to %d messages, kept tool pairs: %v", len(messages), len(result), toolUseIDs)
}

func TestTruncateMessagesManyToolCalls(t *testing.T) {
	// Simulate a long conversation with many tool calls
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "Initial prompt"), // 0
	}

	// Add 30 tool call pairs
	for i := 0; i < 30; i++ {
		toolID := llm.ContentBlock{
			Type:  llm.ContentTypeToolUse,
			ID:    string(rune('A' + i%26)) + string(rune('0'+i/26)),
			Name:  "read",
			Input: map[string]any{},
		}
		messages = append(messages, llm.Message{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{toolID},
		})
		messages = append(messages, llm.Message{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeToolResult, ToolUseID: toolID.ID, Content: "result"},
			},
		})
	}
	messages = append(messages, llm.NewTextMessage(llm.RoleAssistant, "Final response"))

	// Total: 1 + 30*2 + 1 = 62 messages
	if len(messages) != 62 {
		t.Fatalf("expected 62 messages, got %d", len(messages))
	}

	// Truncate to 20 messages
	result := truncateMessages(messages, 20)

	// Verify all tool pairs are preserved
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range result {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse {
				toolUseIDs[block.ID] = true
			}
			if block.Type == llm.ContentTypeToolResult {
				toolResultIDs[block.ToolUseID] = true
			}
		}
	}

	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			t.Errorf("tool_result with ID %s has no corresponding tool_use", id)
		}
	}

	t.Logf("truncated from %d to %d messages", len(messages), len(result))
}
