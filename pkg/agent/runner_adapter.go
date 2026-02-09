package agent

import (
	"context"
	"fmt"
	"log"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
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
	log.Printf("[runner-adapter] starting run: mode=%s repo=%s workdir=%s",
		req.Mode, req.RepoFullName, workDir)

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
	log.Printf("[runner-adapter] run complete: decision=%s files=%d",
		runResult.Response.Decision, len(runResult.Response.Files))

	return runResult, nil
}

// convertLLMRequest converts an llm.Request to an AgentRequest.
func convertLLMRequest(req llm.Request, workDir, systemPrompt string) AgentRequest {
	// Convert comments
	var comments []TaskComment
	for _, c := range req.TaskComments {
		comments = append(comments, TaskComment{
			User: c.User,
			Body: c.Body,
		})
	}
	if len(comments) == 0 {
		for _, c := range req.IssueComments {
			comments = append(comments, TaskComment{
				User: c.User,
				Body: c.Body,
			})
		}
	}

	agentReq := AgentRequest{
		Task:             req.Prompt,
		SystemPrompt:     systemPrompt,
		RepoInstructions: req.RepoInstructions,
		WorkDir:          workDir,
		Context: AgentContext{
			RepoFullName: req.RepoFullName,
			RepoPath:     req.RepoPath,
			TaskID:       req.TaskID,
			TaskTitle:    req.TaskTitle,
			TaskBody:     req.TaskBody,
			TaskLabels:   req.TaskLabels,
			TaskComments: comments,
			Metadata:     req.Metadata,
			CommentBody:  req.CommentBody,
			SlashCommand: req.SlashCommand,
			Requirements: req.Requirements,
		},
	}

	return agentReq
}

// convertToRunResult converts an AgentResult to an llm.RunResult.
func convertToRunResult(result AgentResult) llm.RunResult {
	// Map decision
	var decision llm.Decision
	switch result.Decision {
	case DecisionProceed:
		decision = llm.DecisionProceed
	case DecisionNeedsInfo:
		decision = llm.DecisionNeedsInfo
	case DecisionStop:
		decision = llm.DecisionStop
	default:
		decision = llm.DecisionProceed
	}

	// Convert file changes to map
	files := make(map[string]string)
	for _, fc := range result.FileChanges {
		files[fc.Path] = fc.Content
	}

	return llm.RunResult{
		Response: llm.Response{
			Decision: decision,
			Files:    files,
			Summary:  result.Summary,
		},
		Stdout: result.Message,
	}
}
