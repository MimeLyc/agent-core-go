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
	// Non-positive values mean no iteration cap.
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

	// EnableStreaming enables stream-mode execution paths.
	EnableStreaming bool
}

// NewAPIAgent creates a new APIAgent.
// The provider parameter accepts any LLMProvider implementation (ClaudeProvider, OpenAIProvider, etc.)
// or the legacy AgentRunner which implements LLMProvider for backward compatibility.
func NewAPIAgent(provider llm.LLMProvider, registry *tools.Registry, opts APIAgentOptions) *APIAgent {
	if registry == nil {
		registry = tools.NewRegistry()
	}
	loop := orchestrator.NewAgentLoop(provider, registry)

	// Set defaults. Non-positive MaxIterations means unbounded.
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

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = a.options.SystemPrompt
	}

	// Convert AgentRequest to OrchestratorRequest
	orchReq := orchestrator.OrchestratorRequest{
		SystemPrompt:     systemPrompt,
		RepoInstructions: req.RepoInstructions,
		SoulFile:         req.SoulFile,
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, req.Task),
		},
		MaxIterations:         a.options.MaxIterations,
		MaxMessages:           a.options.MaxMessages,
		WorkDir:               req.WorkDir,
		ToolContext:           tools.NewToolContext(req.WorkDir),
		EnableStreaming:       a.options.EnableStreaming || req.Options.EnableStreaming,
		DisableIterationLimit: req.Options.DisableIterationLimit,
	}

	// Apply request options
	if req.Options.MaxIterations > 0 {
		orchReq.MaxIterations = req.Options.MaxIterations
	}
	if req.Options.DisableIterationLimit {
		orchReq.MaxIterations = 0
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
	if req.Callbacks.OnSteeringApplied != nil {
		orchReq.OnSteeringApplied = req.Callbacks.OnSteeringApplied
	}
	if req.Callbacks.OnFollowUpApplied != nil {
		orchReq.OnFollowUpApplied = req.Callbacks.OnFollowUpApplied
	}
	if req.Callbacks.OnStreamDelta != nil {
		orchReq.OnStreamDelta = req.Callbacks.OnStreamDelta
	}
	if req.Options.GetSteeringMessages != nil {
		orchReq.GetSteeringMessages = func(ctx context.Context, snapshot orchestrator.LoopInputSnapshot) ([]llm.Message, error) {
			return req.Options.GetSteeringMessages(ctx, LoopInputSnapshot{
				Iteration:      snapshot.Iteration,
				MessageCount:   snapshot.MessageCount,
				ToolCallCount:  snapshot.ToolCallCount,
				LastStopReason: snapshot.LastStopReason,
			})
		}
	}
	if req.Options.GetFollowUpMessages != nil {
		orchReq.GetFollowUpMessages = func(ctx context.Context, snapshot orchestrator.LoopInputSnapshot) ([]llm.Message, error) {
			return req.Options.GetFollowUpMessages(ctx, LoopInputSnapshot{
				Iteration:      snapshot.Iteration,
				MessageCount:   snapshot.MessageCount,
				ToolCallCount:  snapshot.ToolCallCount,
				LastStopReason: snapshot.LastStopReason,
			})
		}
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

// ExecuteStream runs the agent and emits structured stream events.
func (a *APIAgent) ExecuteStream(
	ctx context.Context, req AgentRequest) (<-chan AgentStreamEvent, <-chan error) {
	eventCh := make(chan AgentStreamEvent, 128)
	errCh := make(chan error, 1)

	if !a.options.EnableStreaming && !req.Options.EnableStreaming {
		close(eventCh)
		errCh <- fmt.Errorf("streaming is disabled by configuration")
		close(errCh)
		return eventCh, errCh
	}

	go func() {
		defer close(eventCh)
		defer close(errCh)

		emit := func(evt AgentStreamEvent) bool {
			select {
			case <-ctx.Done():
				return false
			case eventCh <- evt:
				return true
			}
		}

		if !emit(AgentStreamEvent{Type: AgentEventAgentStart}) {
			return
		}

		streamReq := req
		streamReq.Options.EnableStreaming = true
		cbs := streamReq.Callbacks

		prevMessage := cbs.OnMessage
		cbs.OnMessage = func(msg llm.Message) {
			if prevMessage != nil {
				prevMessage(msg)
			}
			_ = emit(AgentStreamEvent{
				Type:    AgentEventMessageEnd,
				Message: msg.GetText(),
			})
		}

		prevToolCall := cbs.OnToolCall
		cbs.OnToolCall = func(name string, input map[string]any) {
			if prevToolCall != nil {
				prevToolCall(name, input)
			}
			_ = emit(AgentStreamEvent{
				Type:     AgentEventToolCall,
				ToolName: name,
			})
		}

		prevToolResult := cbs.OnToolResult
		cbs.OnToolResult = func(name string, result tools.ToolResult) {
			if prevToolResult != nil {
				prevToolResult(name, result)
			}
			_ = emit(AgentStreamEvent{
				Type:     AgentEventToolResult,
				ToolName: name,
				Message:  result.Content,
				IsError:  result.IsError,
			})
		}

		prevSteering := cbs.OnSteeringApplied
		cbs.OnSteeringApplied = func(messages []llm.Message) {
			if prevSteering != nil {
				prevSteering(messages)
			}
			_ = emit(AgentStreamEvent{
				Type: AgentEventSteeringApplied,
			})
		}

		prevFollowUp := cbs.OnFollowUpApplied
		cbs.OnFollowUpApplied = func(messages []llm.Message) {
			if prevFollowUp != nil {
				prevFollowUp(messages)
			}
			_ = emit(AgentStreamEvent{
				Type: AgentEventFollowUpApplied,
			})
		}

		prevDelta := cbs.OnStreamDelta
		cbs.OnStreamDelta = func(delta llm.ContentBlockDelta) {
			if prevDelta != nil {
				prevDelta(delta)
			}
			_ = emit(AgentStreamEvent{
				Type:  AgentEventMessageDelta,
				Delta: delta.Text,
			})
		}

		streamReq.Callbacks = cbs
		result, err := a.Execute(ctx, streamReq)
		if err != nil {
			errCh <- err
			return
		}

		usage := result.Usage
		_ = emit(AgentStreamEvent{
			Type:     AgentEventAgentEnd,
			Decision: result.Decision,
			Message:  result.Message,
			Usage:    &usage,
		})
	}()

	return eventCh, errCh
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
		SupportsStreaming:  a.options.EnableStreaming,
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
