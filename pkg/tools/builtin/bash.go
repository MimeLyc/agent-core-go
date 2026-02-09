package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// BashTool executes bash commands.
type BashTool struct{}

func (t BashTool) Name() string {
	return "bash"
}

func (t BashTool) Description() string {
	return "Execute a bash command. Use this for running tests, building projects, or any shell operations. Commands run in the working directory."
}

func (t BashTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default: 60, max: 300)",
			},
		},
		"required": []string{"command"},
	}
}

func (t BashTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckBash(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	command, ok := input["command"].(string)
	if !ok || command == "" {
		return tools.NewErrorResultf("command is required"), nil
	}

	// Validate command for security
	if err := validateCommand(command); err != nil {
		return tools.NewErrorResult(err), nil
	}

	// Get timeout
	timeout := toolCtx.BashTimeout
	if t, ok := input["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
	}
	if timeout > 300 {
		timeout = 300
	}
	if timeout < 1 {
		timeout = 60
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Execute command
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = toolCtx.WorkDir

	// Set up environment
	cmd.Env = buildEnv(toolCtx)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Build result
	result := strings.Builder{}
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("STDERR:\n")
		result.WriteString(stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return tools.ToolResult{
				Content: fmt.Sprintf("Command timed out after %d seconds\n%s", timeout, result.String()),
				IsError: true,
			}, nil
		}
		return tools.ToolResult{
			Content: fmt.Sprintf("Command failed: %v\n%s", err, result.String()),
			IsError: true,
		}, nil
	}

	output := result.String()
	if output == "" {
		output = "(no output)"
	}
	return tools.NewToolResult(output), nil
}

// validateCommand checks for potentially dangerous commands.
func validateCommand(command string) error {
	// Block commands that could be dangerous
	dangerous := []string{
		"rm -rf /",
		"rm -rf ~",
		"mkfs",
		"dd if=/dev/",
		":(){:|:&};:",
		"> /dev/sd",
		"chmod -R 777 /",
	}

	lower := strings.ToLower(command)
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return fmt.Errorf("potentially dangerous command blocked: %s", d)
		}
	}

	return nil
}

// buildEnv creates the environment for command execution.
func buildEnv(toolCtx *tools.ToolContext) []string {
	// Use real HOME from system, not WorkDir.
	// Setting HOME to WorkDir causes Go toolchain, npm, etc. to write
	// cache/config files inside the repo (e.g., go/pkg/, Library/, .npm/).
	home := os.Getenv("HOME")
	if home == "" {
		home = "/tmp"
	}

	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + home,
	}

	// Add custom environment variables
	for k, v := range toolCtx.Env {
		env = append(env, k+"="+v)
	}

	return env
}

// RegisterBashTools registers the bash tool with the registry.
func RegisterBashTools(registry *tools.Registry) {
	registry.MustRegister(BashTool{})
}
