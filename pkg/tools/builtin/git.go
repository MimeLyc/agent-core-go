package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

const gitTimeout = 60 * time.Second

// GitStatusTool shows git repository status.
type GitStatusTool struct{}

func (t GitStatusTool) Name() string {
	return "git_status"
}

func (t GitStatusTool) Description() string {
	return "Show the working tree status. Displays paths that have differences between the index and the current HEAD commit."
}

func (t GitStatusTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t GitStatusTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	output, err := runGitCommand(ctx, toolCtx.WorkDir, "status", "--porcelain")
	if err != nil {
		return tools.NewErrorResultf("git status failed: %v", err), nil
	}

	if output == "" {
		return tools.NewToolResult("Working tree clean"), nil
	}
	return tools.NewToolResult(output), nil
}

// GitDiffTool shows changes in the working directory.
type GitDiffTool struct{}

func (t GitDiffTool) Name() string {
	return "git_diff"
}

func (t GitDiffTool) Description() string {
	return "Show changes between commits, commit and working tree, etc. By default shows unstaged changes."
}

func (t GitDiffTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"staged": map[string]any{
				"type":        "boolean",
				"description": "If true, show staged changes (--cached)",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Limit diff to specific file or directory",
			},
		},
	}
}

func (t GitDiffTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	args := []string{"diff"}
	if staged, ok := input["staged"].(bool); ok && staged {
		args = append(args, "--cached")
	}
	if path, ok := input["path"].(string); ok && path != "" {
		args = append(args, "--", path)
	}

	output, err := runGitCommand(ctx, toolCtx.WorkDir, args...)
	if err != nil {
		return tools.NewErrorResultf("git diff failed: %v", err), nil
	}

	if output == "" {
		return tools.NewToolResult("No changes"), nil
	}
	return tools.NewToolResult(output), nil
}

// GitLogTool shows commit history.
type GitLogTool struct{}

func (t GitLogTool) Name() string {
	return "git_log"
}

func (t GitLogTool) Description() string {
	return "Show commit history. Returns the last N commits with hash, author, date, and message."
}

func (t GitLogTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of commits to show (default: 10, max: 50)",
			},
			"oneline": map[string]any{
				"type":        "boolean",
				"description": "Show each commit on a single line",
			},
		},
	}
}

func (t GitLogTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	count := 10
	if n, ok := input["count"].(float64); ok && n > 0 {
		count = int(n)
		if count > 50 {
			count = 50
		}
	}

	args := []string{"log", fmt.Sprintf("-n%d", count)}
	if oneline, ok := input["oneline"].(bool); ok && oneline {
		args = append(args, "--oneline")
	} else {
		args = append(args, "--format=%h %an <%ae> %ai%n%s%n")
	}

	output, err := runGitCommand(ctx, toolCtx.WorkDir, args...)
	if err != nil {
		return tools.NewErrorResultf("git log failed: %v", err), nil
	}

	return tools.NewToolResult(output), nil
}

// GitAddTool stages files for commit.
type GitAddTool struct{}

func (t GitAddTool) Name() string {
	return "git_add"
}

func (t GitAddTool) Description() string {
	return "Stage files for the next commit. Can stage specific files or all changes."
}

func (t GitAddTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Files to stage. Use ['.'] to stage all changes.",
			},
		},
		"required": []string{"paths"},
	}
}

func (t GitAddTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	pathsRaw, ok := input["paths"].([]any)
	if !ok || len(pathsRaw) == 0 {
		return tools.NewErrorResultf("paths is required"), nil
	}

	args := []string{"add"}
	for _, p := range pathsRaw {
		if path, ok := p.(string); ok {
			args = append(args, path)
		}
	}

	_, err := runGitCommand(ctx, toolCtx.WorkDir, args...)
	if err != nil {
		return tools.NewErrorResultf("git add failed: %v", err), nil
	}

	return tools.NewToolResult("Files staged successfully"), nil
}

// GitCommitTool creates a new commit.
type GitCommitTool struct{}

func (t GitCommitTool) Name() string {
	return "git_commit"
}

func (t GitCommitTool) Description() string {
	return "Create a new commit with staged changes. Requires a commit message."
}

func (t GitCommitTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "The commit message",
			},
		},
		"required": []string{"message"},
	}
}

func (t GitCommitTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	message, ok := input["message"].(string)
	if !ok || message == "" {
		return tools.NewErrorResultf("message is required"), nil
	}

	output, err := runGitCommand(ctx, toolCtx.WorkDir, "commit", "-m", message)
	if err != nil {
		return tools.NewErrorResultf("git commit failed: %v\n%s", err, output), nil
	}

	return tools.NewToolResult(output), nil
}

// GitBranchTool manages branches.
type GitBranchTool struct{}

func (t GitBranchTool) Name() string {
	return "git_branch"
}

func (t GitBranchTool) Description() string {
	return "List, create, or switch branches."
}

func (t GitBranchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "create", "switch"},
				"description": "Action to perform: list, create, or switch",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Branch name (required for create/switch)",
			},
		},
		"required": []string{"action"},
	}
}

func (t GitBranchTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGit(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	action, ok := input["action"].(string)
	if !ok {
		return tools.NewErrorResultf("action is required"), nil
	}

	var args []string
	switch action {
	case "list":
		args = []string{"branch", "-a"}
	case "create":
		name, ok := input["name"].(string)
		if !ok || name == "" {
			return tools.NewErrorResultf("name is required for create"), nil
		}
		args = []string{"branch", name}
	case "switch":
		name, ok := input["name"].(string)
		if !ok || name == "" {
			return tools.NewErrorResultf("name is required for switch"), nil
		}
		args = []string{"checkout", name}
	default:
		return tools.NewErrorResultf("invalid action: %s", action), nil
	}

	output, err := runGitCommand(ctx, toolCtx.WorkDir, args...)
	if err != nil {
		return tools.NewErrorResultf("git %s failed: %v\n%s", action, err, output), nil
	}

	if output == "" && action == "list" {
		output = "(no branches)"
	}
	return tools.NewToolResult(output), nil
}

// runGitCommand executes a git command and returns the output.
func runGitCommand(ctx context.Context, workDir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		errOutput := strings.TrimSpace(stderr.String())
		if errOutput != "" {
			output = output + "\n" + errOutput
		}
		return output, err
	}

	return output, nil
}

// RegisterGitTools registers all git tools with the registry.
func RegisterGitTools(registry *tools.Registry) {
	registry.MustRegister(GitStatusTool{})
	registry.MustRegister(GitDiffTool{})
	registry.MustRegister(GitLogTool{})
	registry.MustRegister(GitAddTool{})
	registry.MustRegister(GitCommitTool{})
	registry.MustRegister(GitBranchTool{})
}
