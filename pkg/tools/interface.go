package tools

import (
	"context"
	"fmt"
)

// Tool defines the interface for all tools available to the agent.
type Tool interface {
	// Name returns the unique name of the tool.
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// InputSchema returns the JSON Schema for the tool's input parameters.
	InputSchema() map[string]any

	// Execute runs the tool with the given input and returns the result.
	Execute(ctx context.Context, toolCtx *ToolContext, input map[string]any) (ToolResult, error)
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	// Content is the output of the tool execution.
	Content string

	// IsError indicates if the execution resulted in an error.
	IsError bool

	// Metadata contains additional information about the execution.
	Metadata map[string]any
}

// NewToolResult creates a successful tool result.
func NewToolResult(content string) ToolResult {
	return ToolResult{Content: content}
}

// NewErrorResult creates an error tool result.
func NewErrorResult(err error) ToolResult {
	return ToolResult{
		Content: err.Error(),
		IsError: true,
	}
}

// NewErrorResultf creates an error tool result with a formatted message.
func NewErrorResultf(format string, args ...any) ToolResult {
	return ToolResult{
		Content: formatMessage(format, args...),
		IsError: true,
	}
}

// WithMetadata adds metadata to a tool result.
func (r ToolResult) WithMetadata(key string, value any) ToolResult {
	if r.Metadata == nil {
		r.Metadata = make(map[string]any)
	}
	r.Metadata[key] = value
	return r
}

func formatMessage(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}
