package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/orchestrator"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// APIAgent implements Agent using the local orchestrator with LLM API.
type APIAgent struct {
	// provider is the LLM API provider (Claude, OpenAI, etc.).
	provider llm.LLMProvider

	// registry contains available tools.
	registry *tools.Registry

	// loop is the orchestrator agent loop.
	loop *orchestrator.AgentLoop

	// options configures the agent behavior.
	options APIAgentOptions
}

// APIAgentOptions configures the APIAgent.
type APIAgentOptions struct {
	// MaxIterations limits agent loop iterations.
	MaxIterations int

	// MaxMessages limits conversation history size.
	MaxMessages int

	// MaxTokens limits response token count.
	MaxTokens int

	// MaxContextTokens is the maximum context window size reported in capabilities.
	MaxContextTokens int

	// SystemPrompt is the default system prompt.
	SystemPrompt string

	// CompactConfig configures context compaction.
	CompactConfig *CompactConfig
}

// NewAPIAgent creates a new APIAgent.
// The provider parameter accepts any LLMProvider implementation (ClaudeProvider, OpenAIProvider, etc.)
// or the legacy AgentRunner which implements LLMProvider for backward compatibility.
func NewAPIAgent(provider llm.LLMProvider, registry *tools.Registry, opts APIAgentOptions) *APIAgent {
	if registry == nil {
		registry = tools.NewRegistry()
	}
	loop := orchestrator.NewAgentLoop(provider, registry)

	// Set defaults
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 50
	}
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = 50
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4096
	}

	return &APIAgent{
		provider: provider,
		registry: registry,
		loop:     loop,
		options:  opts,
	}
}

// Execute runs the agent with the given request.
func (a *APIAgent) Execute(ctx context.Context, req AgentRequest) (AgentResult, error) {
	startTime := time.Now()
	log.Printf("[api-agent] starting execution: workdir=%s task_length=%d",
		req.WorkDir, len(req.Task))

	// Convert AgentRequest to OrchestratorRequest
	orchReq := orchestrator.OrchestratorRequest{
		SystemPrompt:     req.SystemPrompt,
		RepoInstructions: req.RepoInstructions,
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, req.Task),
		},
		MaxIterations: a.options.MaxIterations,
		MaxMessages:   a.options.MaxMessages,
		WorkDir:       req.WorkDir,
		ToolContext:   tools.NewToolContext(req.WorkDir),
	}

	// Apply request options
	if req.Options.MaxIterations > 0 {
		orchReq.MaxIterations = req.Options.MaxIterations
	}
	if req.Options.CompactConfig != nil {
		orchReq.CompactConfig = orchestrator.CompactConfig{
			Enabled:    req.Options.CompactConfig.Enabled,
			Threshold:  req.Options.CompactConfig.Threshold,
			KeepRecent: req.Options.CompactConfig.KeepRecent,
		}
	} else if a.options.CompactConfig != nil {
		orchReq.CompactConfig = orchestrator.CompactConfig{
			Enabled:    a.options.CompactConfig.Enabled,
			Threshold:  a.options.CompactConfig.Threshold,
			KeepRecent: a.options.CompactConfig.KeepRecent,
		}
	}

	// Set up callbacks
	if req.Callbacks.OnMessage != nil {
		orchReq.OnMessage = req.Callbacks.OnMessage
	}
	if req.Callbacks.OnToolCall != nil {
		orchReq.OnToolCall = req.Callbacks.OnToolCall
	}
	if req.Callbacks.OnToolResult != nil {
		orchReq.OnToolResult = req.Callbacks.OnToolResult
	}

	// Run the orchestrator
	orchResult, err := a.loop.Run(ctx, orchReq)
	if err != nil {
		log.Printf("[api-agent] ERROR: orchestrator failed: %v", err)
		return AgentResult{
			Success: false,
			Message: fmt.Sprintf("orchestrator error: %v", err),
		}, err
	}

	// Convert OrchestratorResult to AgentResult
	result := convertOrchestratorResult(orchResult, startTime)
	log.Printf("[api-agent] execution complete: success=%v decision=%s iterations=%d",
		result.Success, result.Decision, result.Usage.TotalIterations)

	return result, nil
}

// Capabilities returns the agent's capabilities.
func (a *APIAgent) Capabilities() AgentCapabilities {
	toolList := a.registry.List()
	toolInfos := make([]ToolInfo, len(toolList))
	for i, t := range toolList {
		toolInfos[i] = ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
		}
	}

	return AgentCapabilities{
		SupportsTools:      true,
		AvailableTools:     toolInfos,
		SupportsStreaming:  false,
		SupportsCompaction: true,
		MaxContextTokens:   a.options.MaxContextTokens,
		Provider:           "api",
	}
}

// Close releases resources.
func (a *APIAgent) Close() error {
	return nil
}

// convertOrchestratorResult converts an OrchestratorResult to an AgentResult.
func convertOrchestratorResult(orchResult orchestrator.OrchestratorResult, startTime time.Time) AgentResult {
	finalText := orchResult.GetFinalText()

	result := AgentResult{
		Success:  true,
		Decision: DecisionProceed,
		Summary:  finalText,
		Message:  finalText,
		Usage: ExecutionUsage{
			TotalIterations:   orchResult.TotalIterations,
			TotalInputTokens:  orchResult.TotalInputTokens,
			TotalOutputTokens: orchResult.TotalOutputTokens,
			TotalDuration:     time.Since(startTime),
		},
		RawOutput: orchResult.Messages,
	}

	// Convert tool calls
	for _, tc := range orchResult.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCallRecord{
			Name:    tc.Name,
			Input:   tc.Input,
			Output:  tc.Result.Content,
			IsError: tc.Result.IsError,
		})
	}

	return result
}
