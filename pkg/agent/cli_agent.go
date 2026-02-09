package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// CLIAgentClient defines the interface for CLI agent communication.
// This interface allows different CLI tools (Claude Code, aider, etc.) to be used.
type CLIAgentClient interface {
	// Execute sends a request to the CLI agent and returns the response.
	Execute(ctx context.Context, req CLIRequest) (CLIResponse, error)

	// GetCapabilities returns the CLI agent's capabilities.
	GetCapabilities(ctx context.Context) (AgentCapabilities, error)

	// Close releases any resources.
	Close() error
}

// CLIRequest is the request format for CLI agents.
type CLIRequest struct {
	// Task is the task description.
	Task string

	// SystemPrompt is the system message.
	SystemPrompt string

	// WorkDir is the working directory.
	WorkDir string

	// Context provides additional context.
	Context map[string]any

	// AllowedTools restricts which tools can be used.
	AllowedTools []string

	// Timeout in seconds.
	TimeoutSeconds int
}

// CLIResponse is the response format from CLI agents.
type CLIResponse struct {
	// Success indicates if the execution completed.
	Success bool

	// Decision is the agent's decision.
	Decision string

	// Summary is a brief description.
	Summary string

	// Message is the detailed response.
	Message string

	// FileChanges lists file modifications.
	FileChanges []FileChange

	// Error contains any error message.
	Error string
}

// CLIAgent wraps external CLI tools (like Claude Code) to implement the Agent interface.
type CLIAgent struct {
	client CLIAgentClient
	config CLIAgentConfig
}

// CLIAgentConfig configures a CLI agent.
type CLIAgentConfig struct {
	// Name identifies the agent.
	Name string

	// Command is the CLI command to execute.
	Command string

	// Args are additional command-line arguments.
	Args []string

	// Timeout is the execution timeout.
	Timeout time.Duration

	// AllowedTools restricts which tools the agent can use.
	AllowedTools []string
}

// ClaudeCodeConfig configures the Claude Code CLI agent.
// This is an alias for CLIAgentConfig for backward compatibility.
type ClaudeCodeConfig struct {
	// Command is the path to the claude binary.
	Command string

	// Args are additional command-line arguments.
	Args []string

	// Timeout is the execution timeout.
	Timeout time.Duration

	// AllowedTools restricts which tools claude can use.
	AllowedTools []string
}

// NewCLIAgent creates a new CLIAgent.
func NewCLIAgent(client CLIAgentClient, config CLIAgentConfig) *CLIAgent {
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Minute
	}
	return &CLIAgent{
		client: client,
		config: config,
	}
}

// Execute runs the CLI agent.
func (a *CLIAgent) Execute(ctx context.Context, req AgentRequest) (AgentResult, error) {
	// Build CLI request
	cliReq := CLIRequest{
		Task:           req.Task,
		SystemPrompt:   req.SystemPrompt,
		WorkDir:        req.WorkDir,
		AllowedTools:   a.config.AllowedTools,
		TimeoutSeconds: int(a.config.Timeout.Seconds()),
	}

	// Build task from context if not provided
	if req.Task == "" {
		cliReq.Task = buildUserPrompt(req)
	}

	// Execute
	cliResp, err := a.client.Execute(ctx, cliReq)
	if err != nil {
		return AgentResult{
			Success: false,
			Message: err.Error(),
		}, err
	}

	// Convert response
	return convertCLIResponse(cliResp), nil
}

// Capabilities returns the agent's capabilities.
func (a *CLIAgent) Capabilities() AgentCapabilities {
	ctx := context.Background()
	caps, err := a.client.GetCapabilities(ctx)
	if err != nil {
		return AgentCapabilities{
			Provider: a.config.Name,
		}
	}
	return caps
}

// Close releases resources.
func (a *CLIAgent) Close() error {
	return a.client.Close()
}

// convertCLIResponse converts a CLIResponse to an AgentResult.
func convertCLIResponse(resp CLIResponse) AgentResult {
	var decision Decision
	switch resp.Decision {
	case "proceed":
		decision = DecisionProceed
	case "needs_info":
		decision = DecisionNeedsInfo
	case "stop":
		decision = DecisionStop
	default:
		decision = DecisionProceed
	}

	return AgentResult{
		Success:     resp.Success,
		Decision:    decision,
		Summary:     resp.Summary,
		Message:     resp.Message,
		FileChanges: resp.FileChanges,
	}
}

// ClaudeCodeClient communicates with Claude Code CLI.
// It implements CLIAgentClient for the Claude Code tool.
type ClaudeCodeClient struct {
	// Command is the claude binary path.
	Command string

	// Args are additional command-line arguments.
	Args []string

	// Timeout is the execution timeout.
	Timeout time.Duration
}

// NewClaudeCodeClient creates a new ClaudeCodeClient.
func NewClaudeCodeClient(cfg CLIAgentConfig) *ClaudeCodeClient {
	cmd := cfg.Command
	if cmd == "" {
		cmd = "claude"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &ClaudeCodeClient{
		Command: cmd,
		Args:    cfg.Args,
		Timeout: timeout,
	}
}

// Execute sends a request to Claude Code CLI.
func (c *ClaudeCodeClient) Execute(ctx context.Context, req CLIRequest) (CLIResponse, error) {
	log.Printf("[claude-code] executing: workdir=%s task_length=%d", req.WorkDir, len(req.Task))

	// Build command arguments
	args := make([]string, 0, len(c.Args)+4)
	args = append(args, c.Args...)

	// Add output format for structured response
	args = append(args, "--output-format", "json")

	// Add prompt
	args = append(args, "-p", req.Task)

	// Create command with timeout
	timeout := c.Timeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Command, args...)
	cmd.Dir = req.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("[claude-code] running: %s %s", c.Command, strings.Join(args, " "))
	startTime := time.Now()

	err := cmd.Run()
	duration := time.Since(startTime)
	log.Printf("[claude-code] completed in %v: stdout=%d stderr=%d err=%v",
		duration, stdout.Len(), stderr.Len(), err)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CLIResponse{
				Success: false,
				Error:   "execution timeout",
			}, fmt.Errorf("claude code execution timeout after %v", timeout)
		}
		return CLIResponse{
			Success: false,
			Error:   fmt.Sprintf("execution error: %v\nstderr: %s", err, stderr.String()),
		}, err
	}

	// Parse output
	return c.parseOutput(stdout.Bytes())
}

// parseOutput parses Claude Code's JSON output.
func (c *ClaudeCodeClient) parseOutput(output []byte) (CLIResponse, error) {
	// Claude Code outputs JSON with specific structure
	var rawResp struct {
		Result    string `json:"result"`
		Error     string `json:"error"`
		Cost      any    `json:"cost"`
		Duration  any    `json:"duration"`
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal(output, &rawResp); err != nil {
		// If not JSON, treat as plain text response
		log.Printf("[claude-code] output is not JSON, treating as text")
		return c.parseTextOutput(string(output))
	}

	if rawResp.Error != "" {
		return CLIResponse{
			Success: false,
			Error:   rawResp.Error,
		}, nil
	}

	// Parse the result content
	return c.parseResultContent(rawResp.Result)
}

// parseTextOutput parses plain text output from Claude Code.
func (c *ClaudeCodeClient) parseTextOutput(text string) (CLIResponse, error) {
	// Try to extract JSON from text
	if idx := strings.Index(text, "{"); idx != -1 {
		jsonText := text[idx:]
		// Find matching brace
		depth := 0
		end := -1
		for i, ch := range jsonText {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		if end > 0 {
			return c.parseResultContent(jsonText[:end])
		}
	}

	// No JSON found, return as summary
	return CLIResponse{
		Success:  true,
		Decision: "proceed",
		Summary:  text,
		Message:  text,
	}, nil
}

// parseResultContent parses the result content which may contain structured response.
func (c *ClaudeCodeClient) parseResultContent(content string) (CLIResponse, error) {
	// Try to parse as our expected response format
	var resp struct {
		Decision         string            `json:"decision"`
		NeedsInfoComment string            `json:"needs_info_comment"`
		CommitMessage    string            `json:"commit_message"`
		PRTitle          string            `json:"pr_title"`
		PRBody           string            `json:"pr_body"`
		Summary          string            `json:"summary"`
		Files            map[string]string `json:"files"`
	}

	if err := json.Unmarshal([]byte(content), &resp); err == nil && resp.Decision != "" {
		// Convert files to FileChanges
		var fileChanges []FileChange
		for path, content := range resp.Files {
			fileChanges = append(fileChanges, FileChange{
				Path:      path,
				Content:   content,
				Operation: FileOpModify,
			})
		}

		return CLIResponse{
			Success:     true,
			Decision:    resp.Decision,
			Summary:     resp.Summary,
			Message:     content,
			FileChanges: fileChanges,
		}, nil
	}

	// Return as-is
	return CLIResponse{
		Success:  true,
		Decision: "proceed",
		Summary:  content,
		Message:  content,
	}, nil
}

// GetCapabilities returns Claude Code's capabilities.
func (c *ClaudeCodeClient) GetCapabilities(ctx context.Context) (AgentCapabilities, error) {
	return AgentCapabilities{
		SupportsTools:      true,
		SupportsStreaming:  true,
		SupportsCompaction: true,
		MaxContextTokens:   200000,
		Provider:           "cli",
	}, nil
}

// Close releases resources.
func (c *ClaudeCodeClient) Close() error {
	return nil
}

// NewClaudeCodeAgent creates a new CLI agent configured for Claude Code.
// Deprecated: Use NewCLIAgent with ClaudeCodeClient instead.
func NewClaudeCodeAgent(cfg ClaudeCodeConfig) *CLIAgent {
	client := NewClaudeCodeClient(CLIAgentConfig{
		Command: cfg.Command,
		Args:    cfg.Args,
		Timeout: cfg.Timeout,
	})
	return NewCLIAgent(client, CLIAgentConfig{
		Name:         "claude-code",
		Command:      cfg.Command,
		Args:         cfg.Args,
		Timeout:      cfg.Timeout,
		AllowedTools: cfg.AllowedTools,
	})
}

// Backward compatibility aliases

// ExternalAgentClient is an alias for CLIAgentClient.
// Deprecated: Use CLIAgentClient instead.
type ExternalAgentClient = CLIAgentClient

// ExternalRequest is an alias for CLIRequest.
// Deprecated: Use CLIRequest instead.
type ExternalRequest = CLIRequest

// ExternalResponse is an alias for CLIResponse.
// Deprecated: Use CLIResponse instead.
type ExternalResponse = CLIResponse

// ExternalAgent is an alias for CLIAgent.
// Deprecated: Use CLIAgent instead.
type ExternalAgent = CLIAgent

// ExternalAgentConfig is an alias for CLIAgentConfig.
// Deprecated: Use CLIAgentConfig instead.
type ExternalAgentConfig = CLIAgentConfig

// NewExternalAgent creates a new CLIAgent (backward compatibility).
// Deprecated: Use NewCLIAgent instead.
func NewExternalAgent(client CLIAgentClient, config CLIAgentConfig) *CLIAgent {
	return NewCLIAgent(client, config)
}
