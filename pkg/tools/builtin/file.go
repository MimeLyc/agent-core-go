package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// ReadFileTool reads file contents.
type ReadFileTool struct{}

func (t ReadFileTool) Name() string {
	return "read_file"
}

func (t ReadFileTool) Description() string {
	return "Read the contents of a file. Use this to examine source code, configuration files, or any text file in the repository."
}

func (t ReadFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file to read, relative to the working directory",
			},
		},
		"required": []string{"path"},
	}
}

func (t ReadFileTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckFileRead(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	path, ok := input["path"].(string)
	if !ok || path == "" {
		return tools.NewErrorResultf("path is required"), nil
	}

	absPath, err := toolCtx.ValidatePath(path)
	if err != nil {
		return tools.NewErrorResult(err), nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return tools.NewErrorResultf("failed to read file: %v", err), nil
	}

	return tools.NewToolResult(string(content)), nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct{}

func (t WriteFileTool) Name() string {
	return "write_file"
}

func (t WriteFileTool) Description() string {
	return "Write content to a file. Creates the file if it doesn't exist, or overwrites if it does. Parent directories are created automatically."
}

func (t WriteFileTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path to the file to write, relative to the working directory",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t WriteFileTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckFileWrite(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	path, ok := input["path"].(string)
	if !ok || path == "" {
		return tools.NewErrorResultf("path is required"), nil
	}

	content, ok := input["content"].(string)
	if !ok {
		return tools.NewErrorResultf("content is required"), nil
	}

	absPath, err := toolCtx.ValidatePath(path)
	if err != nil {
		return tools.NewErrorResult(err), nil
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tools.NewErrorResultf("failed to create directory: %v", err), nil
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return tools.NewErrorResultf("failed to write file: %v", err), nil
	}

	return tools.NewToolResult(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
}

// ListFilesTool lists files in a directory.
type ListFilesTool struct{}

func (t ListFilesTool) Name() string {
	return "list_files"
}

func (t ListFilesTool) Description() string {
	return "List files and directories in a path. Returns names of entries in the directory."
}

func (t ListFilesTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The directory path to list, relative to the working directory. Use '.' for the current directory.",
			},
		},
		"required": []string{"path"},
	}
}

func (t ListFilesTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckFileRead(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	path, ok := input["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	absPath, err := toolCtx.ValidatePath(path)
	if err != nil {
		return tools.NewErrorResult(err), nil
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return tools.NewErrorResultf("failed to list directory: %v", err), nil
	}

	var result string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		result += name + "\n"
	}

	return tools.NewToolResult(result), nil
}

// RegisterFileTools registers all file tools with the registry.
func RegisterFileTools(registry *tools.Registry) {
	registry.MustRegister(ReadFileTool{})
	registry.MustRegister(WriteFileTool{})
	registry.MustRegister(ListFilesTool{})
}
