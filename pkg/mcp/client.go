package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Client is an MCP client for communicating with MCP servers.
type Client struct {
	transport  Transport
	serverInfo Implementation
	tools      []ToolInfo
	initialized bool
}

// NewClient creates a new MCP client with the given transport.
func NewClient(transport Transport) *Client {
	return &Client{
		transport: transport,
	}
}

// Initialize performs the MCP initialization handshake.
func (c *Client) Initialize(ctx context.Context) error {
	if c.initialized {
		return nil
	}

	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo: Implementation{
			Name:    "git-sonic",
			Version: "1.0.0",
		},
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
	}

	req, err := NewRequest(1, MethodInitialize, params)
	if err != nil {
		return fmt.Errorf("failed to create initialize request: %w", err)
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send initialize request: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	c.serverInfo = result.ServerInfo

	// Send initialized notification
	notif, err := NewNotification(MethodInitialized, nil)
	if err != nil {
		return fmt.Errorf("failed to create initialized notification: %w", err)
	}

	if err := c.transport.Notify(ctx, notif); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	c.initialized = true
	return nil
}

// ListTools retrieves the list of available tools from the server.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	req, err := NewRequest(nil, MethodToolsList, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list tools request: %w", err)
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to send list tools request: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools error: %s", resp.Error.Message)
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse list tools result: %w", err)
	}

	c.tools = result.Tools
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return CallToolResult{}, err
		}
	}

	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	req, err := NewRequest(nil, MethodToolsCall, params)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to create call tool request: %w", err)
	}

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to send call tool request: %w", err)
	}

	if resp.Error != nil {
		return CallToolResult{}, fmt.Errorf("call tool error: %s", resp.Error.Message)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return CallToolResult{}, fmt.Errorf("failed to parse call tool result: %w", err)
	}

	return result, nil
}

// Close closes the client and its transport.
func (c *Client) Close() error {
	return c.transport.Close()
}

// ServerInfo returns the server implementation info.
func (c *Client) ServerInfo() Implementation {
	return c.serverInfo
}

// Tools returns the cached list of tools.
func (c *Client) Tools() []ToolInfo {
	return c.tools
}
