package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/internal/pkg/orchestrator"
	agenttypes "github.com/MimeLyc/agent-core-go/pkg/agent/types"
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
		MaxIterations:              a.options.MaxIterations,
		MaxMessages:                a.options.MaxMessages,
		WorkDir:                    req.WorkDir,
		ToolContext:                tools.NewToolContext(req.WorkDir),
		EnableStreaming:            a.options.EnableStreaming || req.Options.EnableStreaming,
		DisableIterationLimit:      req.Options.DisableIterationLimit,
		DisableDefaultContextRules: req.Options.DisableDefaultContextRules,
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
		orchReq.OnMessage = func(msg llm.Message) {
			req.Callbacks.OnMessage(fromLLMMessage(msg))
		}
	}
	if req.Callbacks.OnToolCall != nil {
		orchReq.OnToolCall = req.Callbacks.OnToolCall
	}
	if req.Callbacks.OnToolResult != nil {
		orchReq.OnToolResult = req.Callbacks.OnToolResult
	}
	if req.Callbacks.OnSteeringApplied != nil {
		orchReq.OnSteeringApplied = func(messages []llm.Message) {
			req.Callbacks.OnSteeringApplied(fromLLMMessages(messages))
		}
	}
	if req.Callbacks.OnFollowUpApplied != nil {
		orchReq.OnFollowUpApplied = func(messages []llm.Message) {
			req.Callbacks.OnFollowUpApplied(fromLLMMessages(messages))
		}
	}
	if req.Callbacks.OnStreamDelta != nil {
		orchReq.OnStreamDelta = func(delta llm.ContentBlockDelta) {
			req.Callbacks.OnStreamDelta(fromLLMContentDelta(delta))
		}
	}
	if req.Options.GetSteeringMessages != nil {
		orchReq.GetSteeringMessages = func(ctx context.Context, snapshot orchestrator.LoopInputSnapshot) ([]llm.Message, error) {
			msgs, err := req.Options.GetSteeringMessages(ctx, LoopInputSnapshot{
				Iteration:      snapshot.Iteration,
				MessageCount:   snapshot.MessageCount,
				ToolCallCount:  snapshot.ToolCallCount,
				LastStopReason: fromLLMStopReason(snapshot.LastStopReason),
			})
			if err != nil {
				return nil, err
			}
			return toLLMMessages(msgs), nil
		}
	}
	if req.Options.GetFollowUpMessages != nil {
		orchReq.GetFollowUpMessages = func(ctx context.Context, snapshot orchestrator.LoopInputSnapshot) ([]llm.Message, error) {
			msgs, err := req.Options.GetFollowUpMessages(ctx, LoopInputSnapshot{
				Iteration:      snapshot.Iteration,
				MessageCount:   snapshot.MessageCount,
				ToolCallCount:  snapshot.ToolCallCount,
				LastStopReason: fromLLMStopReason(snapshot.LastStopReason),
			})
			if err != nil {
				return nil, err
			}
			return toLLMMessages(msgs), nil
		}
	}
	if req.Options.TransformContext != nil {
		orchReq.TransformContext = func(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
			transformed, err := req.Options.TransformContext(ctx, fromLLMMessages(messages))
			if err != nil {
				return nil, err
			}
			return toLLMMessages(transformed), nil
		}
	}
	if req.Options.ConvertToLlm != nil {
		orchReq.ConvertToLlm = func(ctx context.Context, messages []llm.Message, providerName string) ([]llm.Message, error) {
			converted, err := req.Options.ConvertToLlm(ctx, fromLLMMessages(messages), providerName)
			if err != nil {
				return nil, err
			}
			return toLLMMessages(converted), nil
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
	log.Printf("[api-agent] execution complete: success=%v iterations=%d",
		result.Success, result.Usage.TotalIterations)

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
		cbs.OnMessage = func(msg agenttypes.Message) {
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
		cbs.OnSteeringApplied = func(messages []agenttypes.Message) {
			if prevSteering != nil {
				prevSteering(messages)
			}
			_ = emit(AgentStreamEvent{
				Type: AgentEventSteeringApplied,
			})
		}

		prevFollowUp := cbs.OnFollowUpApplied
		cbs.OnFollowUpApplied = func(messages []agenttypes.Message) {
			if prevFollowUp != nil {
				prevFollowUp(messages)
			}
			_ = emit(AgentStreamEvent{
				Type: AgentEventFollowUpApplied,
			})
		}

		prevDelta := cbs.OnStreamDelta
		cbs.OnStreamDelta = func(delta agenttypes.ContentBlockDelta) {
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
			Type:    AgentEventAgentEnd,
			Message: result.Message,
			Usage:   &usage,
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
		Success: true,
		Summary: finalText,
		Message: finalText,
		Usage: ExecutionUsage{
			TotalIterations:   orchResult.TotalIterations,
			TotalInputTokens:  orchResult.TotalInputTokens,
			TotalOutputTokens: orchResult.TotalOutputTokens,
			TotalDuration:     time.Since(startTime),
		},
		RawOutput: fromLLMMessages(orchResult.Messages),
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

func fromLLMStopReason(reason llm.StopReason) agenttypes.StopReason {
	return agenttypes.StopReason(reason)
}

func fromLLMContentType(t llm.ContentType) agenttypes.ContentType {
	return agenttypes.ContentType(t)
}

func toLLMContentType(t agenttypes.ContentType) llm.ContentType {
	return llm.ContentType(t)
}

func fromLLMRole(r llm.Role) agenttypes.MessageRole {
	switch r {
	case llm.RoleAssistant:
		return agenttypes.RoleAssistant
	case llm.RoleUser:
		return agenttypes.RoleUser
	default:
		return agenttypes.MessageRole(r)
	}
}

func toLLMRole(r agenttypes.MessageRole) llm.Role {
	switch r {
	case agenttypes.RoleAssistant:
		return llm.RoleAssistant
	case agenttypes.RoleSystem, agenttypes.RoleDeveloper, agenttypes.RoleUser, agenttypes.RoleTool:
		return llm.RoleUser
	default:
		return llm.RoleUser
	}
}

func fromLLMContentBlock(block llm.ContentBlock) agenttypes.ContentBlock {
	return agenttypes.ContentBlock{
		Type:      fromLLMContentType(block.Type),
		Text:      block.Text,
		ID:        block.ID,
		Name:      block.Name,
		Input:     block.Input,
		ToolUseID: block.ToolUseID,
		Content:   block.Content,
		IsError:   block.IsError,
	}
}

func toLLMContentBlock(block agenttypes.ContentBlock) llm.ContentBlock {
	return llm.ContentBlock{
		Type:      toLLMContentType(block.Type),
		Text:      block.Text,
		ID:        block.ID,
		Name:      block.Name,
		Input:     block.Input,
		ToolUseID: block.ToolUseID,
		Content:   block.Content,
		IsError:   block.IsError,
	}
}

func fromLLMMessage(msg llm.Message) agenttypes.Message {
	content := make([]agenttypes.ContentBlock, 0, len(msg.Content))
	for _, block := range msg.Content {
		content = append(content, fromLLMContentBlock(block))
	}
	return agenttypes.Message{
		Role:    fromLLMRole(msg.Role),
		Content: content,
	}
}

func toLLMMessage(msg agenttypes.Message) llm.Message {
	content := make([]llm.ContentBlock, 0, len(msg.Content))
	for _, block := range msg.Content {
		content = append(content, toLLMContentBlock(block))
	}
	return llm.Message{
		Role:    toLLMRole(msg.Role),
		Content: content,
	}
}

func fromLLMMessages(messages []llm.Message) []agenttypes.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]agenttypes.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, fromLLMMessage(msg))
	}
	return out
}

func toLLMMessages(messages []agenttypes.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, toLLMMessage(msg))
	}
	return out
}

func fromLLMContentDelta(delta llm.ContentBlockDelta) agenttypes.ContentBlockDelta {
	return agenttypes.ContentBlockDelta{
		Type: fromLLMContentType(delta.Type),
		Text: delta.Text,
	}
}
