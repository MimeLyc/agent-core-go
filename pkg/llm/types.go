package llm

// Role represents the role of a message sender.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentType represents the type of content block.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

// StopReason represents why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonStopSeq   StopReason = "stop_sequence"
)

// ContentBlock represents a content block in a message.
type ContentBlock struct {
	Type ContentType `json:"type"`

	// For text content
	Text string `json:"text,omitempty"`

	// For tool_use content
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`

	// For tool_result content
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Message represents a message in the conversation.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// NewTextMessage creates a new text message.
func NewTextMessage(role Role, text string) Message {
	return Message{
		Role: role,
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: text},
		},
	}
}

// NewToolResultMessage creates a new tool result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: RoleUser,
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

// GetText extracts concatenated text from all text content blocks.
func (m Message) GetText() string {
	var result string
	for _, block := range m.Content {
		if block.Type == ContentTypeText {
			if result != "" {
				result += "\n"
			}
			result += block.Text
		}
	}
	return result
}

// GetToolUses extracts all tool use blocks from the message.
func (m Message) GetToolUses() []ContentBlock {
	var uses []ContentBlock
	for _, block := range m.Content {
		if block.Type == ContentTypeToolUse {
			uses = append(uses, block)
		}
	}
	return uses
}

// ToolDefinition defines a tool available to the agent.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AgentRequest represents a request to the agent API.
type AgentRequest struct {
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens"`
	System      string           `json:"system,omitempty"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	StopSeqs    []string         `json:"stop_sequences,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
}

// AgentResponse represents a response from the agent API.
type AgentResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         Role           `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   StopReason     `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage"`
}

// Usage represents token usage information.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ToMessage converts the response to a Message for conversation history.
func (r AgentResponse) ToMessage() Message {
	return Message{
		Role:    r.Role,
		Content: r.Content,
	}
}

// GetText extracts concatenated text from all text content blocks.
func (r AgentResponse) GetText() string {
	return r.ToMessage().GetText()
}

// GetToolUses extracts all tool use blocks from the response.
func (r AgentResponse) GetToolUses() []ContentBlock {
	return r.ToMessage().GetToolUses()
}

// HasToolUse checks if the response contains tool use blocks.
func (r AgentResponse) HasToolUse() bool {
	return r.StopReason == StopReasonToolUse || len(r.GetToolUses()) > 0
}
