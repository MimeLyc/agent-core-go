package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

type apiAgentTestProvider struct{}

func (apiAgentTestProvider) Name() string {
	return "api-agent-test-provider"
}

type apiAgentLoopProvider struct {
	toolIterations int
	callCount      int
}

func (p *apiAgentLoopProvider) Name() string {
	return "api-agent-loop-provider"
}

func (p *apiAgentLoopProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	p.callCount++
	if p.callCount <= p.toolIterations {
		return llm.AgentResponse{
			Role:       llm.RoleAssistant,
			StopReason: llm.StopReasonToolUse,
			Content: []llm.ContentBlock{
				{
					Type:  llm.ContentTypeToolUse,
					ID:    fmt.Sprintf("tool-%d", p.callCount),
					Name:  "noop",
					Input: map[string]any{},
				},
			},
		}, nil
	}
	return llm.AgentResponse{
		Role:       llm.RoleAssistant,
		StopReason: llm.StopReasonEndTurn,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "done"},
		},
	}, nil
}

type apiAgentNoopTool struct{}

func (apiAgentNoopTool) Name() string {
	return "noop"
}

func (apiAgentNoopTool) Description() string {
	return "noop tool for api agent tests"
}

func (apiAgentNoopTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}

func (apiAgentNoopTool) Execute(_ context.Context, _ *tools.ToolContext, _ map[string]any) (tools.ToolResult, error) {
	return tools.NewToolResult("ok"), nil
}

func (apiAgentTestProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	return llm.AgentResponse{
		Role:       llm.RoleAssistant,
		StopReason: llm.StopReasonEndTurn,
		Content: []llm.ContentBlock{
			{
				Type: llm.ContentTypeText,
				Text: "done",
			},
		},
	}, nil
}

func TestNewAPIAgentPreservesNonPositiveMaxIterations(t *testing.T) {
	tests := []struct {
		name          string
		maxIterations int
	}{
		{
			name:          "zero",
			maxIterations: 0,
		},
		{
			name:          "negative",
			maxIterations: -3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAPIAgent(apiAgentTestProvider{}, tools.NewRegistry(), APIAgentOptions{
				MaxIterations: tt.maxIterations,
			})

			if a.options.MaxIterations != tt.maxIterations {
				t.Fatalf("expected MaxIterations to stay %d, got %d", tt.maxIterations, a.options.MaxIterations)
			}
		})
	}
}

func TestAPIAgentExecuteRequestOptionsCanDisableIterationLimit(t *testing.T) {
	provider := &apiAgentLoopProvider{
		toolIterations: 2,
	}
	registry := tools.NewRegistry()
	registry.MustRegister(apiAgentNoopTool{})
	a := NewAPIAgent(provider, registry, APIAgentOptions{
		MaxIterations: 1,
	})

	result, err := a.Execute(context.Background(), AgentRequest{
		Task: "run",
		Options: AgentOptions{
			DisableIterationLimit: true,
		},
	})
	if err != nil {
		t.Fatalf("expected no error when disabling iteration limit, got %v", err)
	}
	if result.Usage.TotalIterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", result.Usage.TotalIterations)
	}
}

type apiAgentStreamingProvider struct{}

func (apiAgentStreamingProvider) Name() string {
	return "api-agent-streaming-provider"
}

func (apiAgentStreamingProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	return llm.AgentResponse{
		Role:       llm.RoleAssistant,
		StopReason: llm.StopReasonEndTurn,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "done"},
		},
	}, nil
}

func (apiAgentStreamingProvider) Stream(_ context.Context, _ llm.AgentRequest, onDelta func(llm.ContentBlockDelta)) (llm.AgentResponse, error) {
	if onDelta != nil {
		onDelta(llm.ContentBlockDelta{Type: llm.ContentTypeText, Text: "he"})
		onDelta(llm.ContentBlockDelta{Type: llm.ContentTypeText, Text: "llo"})
	}
	return llm.AgentResponse{
		Role:       llm.RoleAssistant,
		StopReason: llm.StopReasonEndTurn,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "hello"},
		},
	}, nil
}

func TestAPIAgentExecuteStreamEmitsDeltaEvents(t *testing.T) {
	a := NewAPIAgent(apiAgentStreamingProvider{}, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: true,
	})

	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "stream please",
	})

	var sawDelta bool
	var deltaText string
	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if evt.Type == AgentEventMessageDelta {
				sawDelta = true
				deltaText += evt.Delta
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	if !sawDelta {
		t.Fatalf("expected at least one message_delta event")
	}
	if deltaText != "hello" {
		t.Fatalf("expected merged delta text hello, got %q", deltaText)
	}
}
