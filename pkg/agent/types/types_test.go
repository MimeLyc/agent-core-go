package types

import "testing"

func TestMessageAndRoles(t *testing.T) {
	msg := NewTextMessage(RoleUser, "hello")
	if msg.Role != RoleUser {
		t.Fatalf("role = %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Type != ContentTypeText {
		t.Fatalf("content type = %q, want %q", msg.Content[0].Type, ContentTypeText)
	}
}

func TestToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("tool-1", "ok", false)
	if msg.Role != RoleTool {
		t.Fatalf("role = %q, want %q", msg.Role, RoleTool)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Type != ContentTypeToolResult {
		t.Fatalf("content type = %q, want %q", msg.Content[0].Type, ContentTypeToolResult)
	}
	if msg.Content[0].ToolUseID != "tool-1" {
		t.Fatalf("tool_use_id = %q, want tool-1", msg.Content[0].ToolUseID)
	}
}

func TestGetText(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "line1"},
			{Type: ContentTypeText, Text: "line2"},
		},
	}
	if got := msg.GetText(); got != "line1\nline2" {
		t.Fatalf("GetText() = %q, want %q", got, "line1\nline2")
	}
}
