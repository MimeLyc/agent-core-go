package types

// MessageRole identifies who produced a message.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleDeveloper MessageRole = "developer"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// ContentType identifies a message content block type.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

// StopReason describes why the model stopped.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonStopSeq   StopReason = "stop_sequence"
)

// ContentBlock is a unit of message content.
type ContentBlock struct {
	Type ContentType `json:"type"`

	// Text block fields.
	Text string `json:"text,omitempty"`

	// Tool use block fields.
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// Tool result block fields.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Message is the public message model for agent callbacks/results.
type Message struct {
	Role    MessageRole    `json:"role"`
	Content []ContentBlock `json:"content"`
}

// LLMMessage is the provider-facing message model after convertToLlm.
// It intentionally aliases Message so existing callers remain source-compatible.
type LLMMessage = Message

// ContentBlockDelta describes streamed content increments.
type ContentBlockDelta struct {
	Type ContentType `json:"type"`
	Text string      `json:"text,omitempty"`
}

// NewTextMessage creates a simple text message.
func NewTextMessage(role MessageRole, text string) Message {
	return Message{
		Role: role,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: text},
		},
	}
}

// NewToolResultMessage creates a tool result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: RoleTool,
		Content: []ContentBlock{
			{
				Type:      ContentTypeToolResult,
				ToolUseID: toolUseID,
				Content:   content,
				IsError:   isError,
			},
		},
	}
}

// GetText concatenates text blocks using newlines.
func (m Message) GetText() string {
	result := ""
	for _, block := range m.Content {
		if block.Type != ContentTypeText {
			continue
		}
		if result != "" {
			result += "\n"
		}
		result += block.Text
	}
	return result
}
