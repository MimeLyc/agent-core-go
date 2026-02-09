package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// MCPTool wraps an MCP tool to implement the tools.Tool interface.
type MCPTool struct {
	client     *Client
	info       ToolInfo
	serverName string
}

// NewMCPTool creates a new MCP tool wrapper.
func NewMCPTool(client *Client, info ToolInfo, serverName string) *MCPTool {
	return &MCPTool{
		client:     client,
		info:       info,
		serverName: serverName,
	}
}

// Name returns the tool name prefixed with the server name.
func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", t.serverName, t.info.Name)
}

// Description returns the tool description.
func (t *MCPTool) Description() string {
	return t.info.Description
}

// InputSchema returns the tool's input schema.
func (t *MCPTool) InputSchema() map[string]any {
	return t.info.InputSchema
}

// Execute calls the MCP tool and returns the result.
func (t *MCPTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	result, err := t.client.CallTool(ctx, t.info.Name, input)
	if err != nil {
		return tools.NewErrorResult(err), nil
	}

	// Convert MCP result to tools.ToolResult
	var content strings.Builder
	for _, item := range result.Content {
		if item.Type == "text" && item.Text != "" {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString(item.Text)
		}
	}

	return tools.ToolResult{
		Content: content.String(),
		IsError: result.IsError,
	}, nil
}

// MCPServer manages an MCP server connection and its tools.
type MCPServer struct {
	name      string
	client    *Client
	transport Transport
	tools     []*MCPTool
}

// NewMCPServer creates a new MCP server connection.
func NewMCPServer(name, command string, args []string, env map[string]string, workDir string) (*MCPServer, error) {
	// Build environment slice
	var envSlice []string
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}

	transport, err := NewStdioTransport(command, args, envSlice, workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	client := NewClient(transport)

	return &MCPServer{
		name:      name,
		client:    client,
		transport: transport,
	}, nil
}

// Initialize initializes the server connection and loads tools.
func (s *MCPServer) Initialize(ctx context.Context) error {
	if err := s.client.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize MCP server %s: %w", s.name, err)
	}

	toolInfos, err := s.client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools from MCP server %s: %w", s.name, err)
	}

	s.tools = make([]*MCPTool, len(toolInfos))
	for i, info := range toolInfos {
		s.tools[i] = NewMCPTool(s.client, info, s.name)
	}

	return nil
}

// Tools returns the MCP tools as tools.Tool interfaces.
func (s *MCPServer) Tools() []tools.Tool {
	result := make([]tools.Tool, len(s.tools))
	for i, t := range s.tools {
		result[i] = t
	}
	return result
}

// RegisterTools registers all MCP tools with the given registry.
func (s *MCPServer) RegisterTools(registry *tools.Registry) error {
	for _, t := range s.tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("failed to register MCP tool %s: %w", t.Name(), err)
		}
	}
	return nil
}

// Close closes the server connection.
func (s *MCPServer) Close() error {
	return s.client.Close()
}

// Name returns the server name.
func (s *MCPServer) Name() string {
	return s.name
}

// ServerInfo returns the MCP server implementation info.
func (s *MCPServer) ServerInfo() Implementation {
	return s.client.ServerInfo()
}
