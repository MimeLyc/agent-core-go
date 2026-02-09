package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// OrchestratorRunner adapts the Orchestrator to implement the llm.Runner interface.
// This provides backward compatibility with the existing workflow engine.
type OrchestratorRunner struct {
	// Orchestrator handles the agent loop.
	Orchestrator Orchestrator

	// SystemPrompt is the default system prompt.
	SystemPrompt string

	// MaxIterations limits the agent loop iterations.
	MaxIterations int

	// MaxMessages limits conversation history to avoid API limits.
	MaxMessages int

	// CompactConfig configures context compaction.
	CompactConfig CompactConfig

	// Registry contains available tools.
	Registry *tools.Registry
}

// NewOrchestratorRunner creates a new runner adapter.
func NewOrchestratorRunner(orch Orchestrator, registry *tools.Registry) *OrchestratorRunner {
	return &OrchestratorRunner{
		Orchestrator:  orch,
		MaxIterations: defaultMaxIterations,
		Registry:      registry,
	}
}

// Run implements the llm.Runner interface.
func (r *OrchestratorRunner) Run(ctx context.Context, req llm.Request, workDir string) (llm.RunResult, error) {
	log.Printf("[runner] starting orchestrator run: mode=%s repo=%s workdir=%s",
		req.Mode, req.RepoFullName, workDir)

	// Build initial message from request
	userPrompt := req.Prompt
	if userPrompt == "" {
		// Build prompt from request fields
		userPrompt = buildPromptFromRequest(req)
	}
	log.Printf("[runner] user prompt length: %d chars", len(userPrompt))

	// Create orchestrator request
	orchReq := OrchestratorRequest{
		SystemPrompt:     r.SystemPrompt,
		RepoInstructions: req.RepoInstructions,
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, userPrompt),
		},
		MaxIterations: r.MaxIterations,
		MaxMessages:   r.MaxMessages,
		CompactConfig: r.CompactConfig,
		WorkDir:       workDir,
		ToolContext:   tools.NewToolContext(workDir),
	}

	// Run the orchestrator
	result, err := r.Orchestrator.Run(ctx, orchReq)
	if err != nil {
		log.Printf("[runner] ERROR: orchestrator run failed: %v", err)
		return llm.RunResult{}, fmt.Errorf("orchestrator run failed: %w", err)
	}

	log.Printf("[runner] orchestrator completed: iterations=%d tool_calls=%d input_tokens=%d output_tokens=%d",
		result.TotalIterations, len(result.ToolCalls), result.TotalInputTokens, result.TotalOutputTokens)

	// Extract response from final message
	finalText := result.GetFinalText()
	log.Printf("[runner] final text length: %d chars", len(finalText))

	// Try to parse as llm.Response
	resp, parseErr := llm.ParseResponse([]byte(finalText))
	if parseErr != nil {
		log.Printf("[runner] WARNING: failed to parse response as JSON: %v", parseErr)
		// If parsing fails, create a response from the text
		resp = llm.Response{
			Decision: llm.DecisionProceed,
			Summary:  finalText,
		}
	}

	log.Printf("[runner] response: decision=%s files_count=%d has_patch=%v",
		resp.Decision, len(resp.Files), resp.Patch != "")

	return llm.RunResult{
		Response: resp,
		Stdout:   finalText,
	}, nil
}

// buildPromptFromRequest creates a user prompt from the llm.Request fields.
func buildPromptFromRequest(req llm.Request) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Mode: %s", req.Mode))
	parts = append(parts, fmt.Sprintf("Repository: %s", req.RepoFullName))

	taskID := req.TaskID
	if taskID == "" {
		switch {
		case req.IssueNumber > 0:
			taskID = fmt.Sprintf("issue-%d", req.IssueNumber)
		case req.PRNumber > 0:
			taskID = fmt.Sprintf("pr-%d", req.PRNumber)
		}
	}
	taskTitle := firstNonEmptyRequest(req.TaskTitle, req.IssueTitle, req.PRTitle)
	taskBody := firstNonEmptyRequest(req.TaskBody, req.IssueBody, req.PRBody)
	taskLabels := req.TaskLabels
	if len(taskLabels) == 0 {
		taskLabels = req.IssueLabels
	}
	taskComments := req.TaskComments
	if len(taskComments) == 0 {
		taskComments = req.IssueComments
	}

	if taskID != "" || taskTitle != "" || taskBody != "" || len(taskLabels) > 0 || len(taskComments) > 0 || req.PRHeadRef != "" || req.PRBaseRef != "" {
		parts = append(parts, "\n## Task Context")
		if taskID != "" {
			parts = append(parts, fmt.Sprintf("Task ID: %s", taskID))
		}
		if taskTitle != "" {
			parts = append(parts, fmt.Sprintf("Title: %s", taskTitle))
		}
		if taskBody != "" {
			parts = append(parts, fmt.Sprintf("Body:\n%s", taskBody))
		}
		if len(taskLabels) > 0 {
			parts = append(parts, fmt.Sprintf("Labels: %s", strings.Join(taskLabels, ", ")))
		}
		if req.PRHeadRef != "" {
			parts = append(parts, fmt.Sprintf("Source Branch: %s", req.PRHeadRef))
		}
		if req.PRBaseRef != "" {
			parts = append(parts, fmt.Sprintf("Target Branch: %s", req.PRBaseRef))
		}
		if len(taskComments) > 0 {
			parts = append(parts, "\n### Comments:")
			for _, c := range taskComments {
				parts = append(parts, fmt.Sprintf("@%s: %s", c.User, c.Body))
			}
		}
	}

	if req.CommentBody != "" {
		parts = append(parts, fmt.Sprintf("\n## Trigger Input\n%s", req.CommentBody))
	}

	if req.SlashCommand != "" {
		parts = append(parts, fmt.Sprintf("\nTrigger Command: %s", req.SlashCommand))
	}

	if req.Requirements != "" {
		parts = append(parts, fmt.Sprintf("\n## Requirements\n%s", req.Requirements))
	}

	if len(req.Metadata) > 0 {
		parts = append(parts, "\n## Metadata")
		keys := make([]string, 0, len(req.Metadata))
		for k := range req.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("- %s: %s", key, req.Metadata[key]))
		}
	}

	parts = append(parts, "\n## Instructions")
	parts = append(parts, "Analyze the context and make the necessary code changes.")
	parts = append(parts, "Use the available tools to read files, make changes, and run commands.")
	parts = append(parts, "When complete, output a JSON object with the following fields:")
	parts = append(parts, "- decision: 'proceed' (changes ready), 'needs_info' (need more info), or 'stop' (cannot automate)")
	parts = append(parts, "- needs_info_comment: explanation if decision is needs_info")
	parts = append(parts, "- commit_message: commit message for changes")
	parts = append(parts, "- pr_title: title for the proposed code change (legacy field name)")
	parts = append(parts, "- pr_body: body for the proposed code change (legacy field name)")
	parts = append(parts, "- files: map of file paths to their complete new content")
	parts = append(parts, "- summary: summary of what was done")

	return strings.Join(parts, "\n")
}

func firstNonEmptyRequest(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ExtractFilesFromResult attempts to extract file changes from the orchestrator result.
func ExtractFilesFromResult(result OrchestratorResult) (map[string]string, error) {
	// Look for file writes in tool calls
	files := make(map[string]string)
	for _, tc := range result.ToolCalls {
		if tc.Name == "write_file" {
			path, ok := tc.Input["path"].(string)
			if !ok {
				continue
			}
			content, ok := tc.Input["content"].(string)
			if !ok {
				continue
			}
			files[path] = content
		}
	}

	// Also try to parse files from the final response
	finalText := result.GetFinalText()
	if strings.Contains(finalText, `"files"`) {
		var resp struct {
			Files map[string]string `json:"files"`
		}
		if err := json.Unmarshal([]byte(finalText), &resp); err == nil && len(resp.Files) > 0 {
			for k, v := range resp.Files {
				files[k] = v
			}
		}
	}

	return files, nil
}
