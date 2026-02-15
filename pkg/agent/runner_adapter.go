package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
)

// RunnerAdapter adapts an Agent to implement the llm.Runner interface.
// This provides backward compatibility with the existing workflow engine.
type RunnerAdapter struct {
	// Agent is the underlying agent implementation.
	Agent Agent

	// SystemPrompt is the default system prompt.
	SystemPrompt string
}

// NewRunnerAdapter creates a new RunnerAdapter.
func NewRunnerAdapter(agent Agent, systemPrompt string) *RunnerAdapter {
	return &RunnerAdapter{
		Agent:        agent,
		SystemPrompt: systemPrompt,
	}
}

// Run implements the llm.Runner interface.
func (a *RunnerAdapter) Run(ctx context.Context, req llm.Request, workDir string) (llm.RunResult, error) {
	log.Printf("[runner-adapter] starting run: mode=%s workdir=%s",
		req.Mode, workDir)

	// Convert llm.Request to AgentRequest
	agentReq := convertLLMRequest(req, workDir, a.SystemPrompt)

	// Execute the agent
	result, err := a.Agent.Execute(ctx, agentReq)
	if err != nil {
		log.Printf("[runner-adapter] ERROR: agent execution failed: %v", err)
		return llm.RunResult{}, fmt.Errorf("agent execution failed: %w", err)
	}

	// Convert AgentResult to llm.RunResult
	runResult := convertToRunResult(result)
	log.Printf("[runner-adapter] run complete: files=%d",
		len(runResult.Response.Files))

	return runResult, nil
}

// convertLLMRequest converts an llm.Request to an AgentRequest.
// AgentRequest only keeps a single user input task text.
func convertLLMRequest(req llm.Request, workDir, systemPrompt string) AgentRequest {
	task := strings.TrimSpace(req.Prompt)
	if task == "" {
		task = strings.TrimSpace(firstNonEmpty(req.TaskBody, req.IssueBody, req.PRBody, req.CommentBody))
	}
	if task == "" {
		task = strings.TrimSpace(firstNonEmpty(req.TaskTitle, req.IssueTitle, req.PRTitle))
	}
	if task == "" {
		task = "Please process the user request."
	}

	agentReq := AgentRequest{
		Task:             task,
		SystemPrompt:     systemPrompt,
		RepoInstructions: req.RepoInstructions,
		WorkDir:          workDir,
	}

	return agentReq
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// convertToRunResult converts an AgentResult to an llm.RunResult.
func convertToRunResult(result AgentResult) llm.RunResult {
	// Convert file changes to map
	files := make(map[string]string)
	for _, fc := range result.FileChanges {
		files[fc.Path] = fc.Content
	}

	return llm.RunResult{
		Response: llm.Response{
			// Internal legacy runner contract still requires decision.
			// Public agent APIs no longer expose decision semantics.
			Decision: llm.DecisionProceed,
			Files:    files,
			Summary:  result.Summary,
		},
		Stdout: result.Message,
	}
}
