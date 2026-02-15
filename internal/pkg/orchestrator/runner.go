package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
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
	log.Printf("[runner] starting orchestrator run: mode=%s workdir=%s",
		req.Mode, workDir)

	// Build initial message from request.
	userPrompt := buildPromptFromRequest(req)
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

// buildPromptFromRequest creates a user task prompt from llm.Request.
// Prompt is preferred. Legacy fields are used only as compatibility fallback.
func buildPromptFromRequest(req llm.Request) string {
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		return prompt
	}

	var parts []string
	title := firstNonEmptyRequest(req.TaskTitle, req.IssueTitle, req.PRTitle)
	if strings.TrimSpace(title) != "" {
		parts = append(parts, fmt.Sprintf("Title: %s", strings.TrimSpace(title)))
	}

	body := firstNonEmptyRequest(req.TaskBody, req.IssueBody, req.PRBody)
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimSpace(body))
	}

	if strings.TrimSpace(req.CommentBody) != "" {
		parts = append(parts, strings.TrimSpace(req.CommentBody))
	}

	if strings.TrimSpace(req.Requirements) != "" {
		parts = append(parts, fmt.Sprintf("Requirements:\n%s", strings.TrimSpace(req.Requirements)))
	}

	if len(parts) == 0 {
		return "Please process the user request."
	}
	return strings.Join(parts, "\n\n")
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
