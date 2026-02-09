package mcp

import "encoding/json"

// JSON-RPC 2.0 types for MCP protocol

// Request represents a JSON-RPC request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Notification represents a JSON-RPC notification (no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Error represents a JSON-RPC error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return e.Message
}

// NewRequest creates a new JSON-RPC request.
func NewRequest(id any, method string, params any) (Request, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return Request{}, err
		}
		paramsRaw = data
	}
	return Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

// NewNotification creates a new JSON-RPC notification.
func NewNotification(method string, params any) (Notification, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return Notification{}, err
		}
		paramsRaw = data
	}
	return Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

// MCP-specific types

// InitializeParams are parameters for the initialize request.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      Implementation `json:"clientInfo"`
	Capabilities    Capabilities   `json:"capabilities"`
}

// InitializeResult is the result of the initialize request.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      Implementation `json:"serverInfo"`
	Capabilities    Capabilities   `json:"capabilities"`
}

// Implementation describes a client or server implementation.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities describes client or server capabilities.
type Capabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability describes tool capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes resource capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes prompt capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ListToolsResult is the result of tools/list.
type ListToolsResult struct {
	Tools []ToolInfo `json:"tools"`
}

// ToolInfo describes an MCP tool.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// CallToolParams are parameters for tools/call.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of tools/call.
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem represents content returned by a tool.
type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // base64 for blob/image
}

// Standard MCP methods
const (
	MethodInitialize       = "initialize"
	MethodInitialized      = "notifications/initialized"
	MethodToolsList        = "tools/list"
	MethodToolsCall        = "tools/call"
	MethodResourcesList    = "resources/list"
	MethodResourcesRead    = "resources/read"
	MethodPromptsList      = "prompts/list"
	MethodPromptsGet       = "prompts/get"
	MethodPing             = "ping"
	MethodCancelled        = "notifications/cancelled"
	MethodProgress         = "notifications/progress"
)

// MCP protocol version
const ProtocolVersion = "2024-11-05"
