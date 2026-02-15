package llm

import "testing"

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage(RoleUser, "hello")

	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want user", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Type != ContentTypeText {
		t.Errorf("Content[0].Type = %q, want text", msg.Content[0].Type)
	}
	if msg.Content[0].Text != "hello" {
		t.Errorf("Content[0].Text = %q, want hello", msg.Content[0].Text)
	}
}

func TestNewToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("id-123", "result content", false)

	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want user", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Type != ContentTypeToolResult {
		t.Errorf("Content[0].Type = %q, want tool_result", msg.Content[0].Type)
	}
	if msg.Content[0].ToolUseID != "id-123" {
		t.Errorf("Content[0].ToolUseID = %q, want id-123", msg.Content[0].ToolUseID)
	}
	if msg.Content[0].Content != "result content" {
		t.Errorf("Content[0].Content = %q, want result content", msg.Content[0].Content)
	}
	if msg.Content[0].IsError {
		t.Error("Content[0].IsError = true, want false")
	}
}

func TestMessageGetText(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "Hello"},
			{Type: ContentTypeToolUse, ID: "1", Name: "test"},
			{Type: ContentTypeText, Text: "World"},
		},
	}

	text := msg.GetText()
	if text != "Hello\nWorld" {
		t.Errorf("GetText() = %q, want Hello\\nWorld", text)
	}
}

func TestMessageGetToolUses(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "I'll help you"},
			{Type: ContentTypeToolUse, ID: "1", Name: "read_file", Input: map[string]any{"path": "test.txt"}},
			{Type: ContentTypeToolUse, ID: "2", Name: "write_file", Input: map[string]any{"path": "out.txt"}},
		},
	}

	uses := msg.GetToolUses()
	if len(uses) != 2 {
		t.Fatalf("GetToolUses() len = %d, want 2", len(uses))
	}
	if uses[0].Name != "read_file" {
		t.Errorf("uses[0].Name = %q, want read_file", uses[0].Name)
	}
	if uses[1].Name != "write_file" {
		t.Errorf("uses[1].Name = %q, want write_file", uses[1].Name)
	}
}

func TestAgentResponseToMessage(t *testing.T) {
	resp := AgentResponse{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "Response text"},
		},
		StopReason: StopReasonEndTurn,
	}

	msg := resp.ToMessage()
	if msg.Role != RoleAssistant {
		t.Errorf("Role = %q, want assistant", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Text != "Response text" {
		t.Errorf("Content[0].Text = %q, want Response text", msg.Content[0].Text)
	}
}

func TestAgentResponseHasToolUse(t *testing.T) {
	respWithToolUse := AgentResponse{
		StopReason: StopReasonToolUse,
		Content: []ContentBlock{
			{Type: ContentTypeToolUse, ID: "1", Name: "test"},
		},
	}

	respWithEndTurn := AgentResponse{
		StopReason: StopReasonEndTurn,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "Done"},
		},
	}

	if !respWithToolUse.HasToolUse() {
		t.Error("HasToolUse() = false for tool_use response")
	}
	if respWithEndTurn.HasToolUse() {
		t.Error("HasToolUse() = true for end_turn response")
	}
}
