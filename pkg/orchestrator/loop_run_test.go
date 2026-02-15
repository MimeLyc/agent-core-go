package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

type loopTestProvider struct {
	toolIterations int
	callCount      int
}

func (p *loopTestProvider) Name() string {
	return "loop-test-provider"
}

func (p *loopTestProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
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
			{
				Type: llm.ContentTypeText,
				Text: "done",
			},
		},
	}, nil
}

type noopTool struct{}

func (noopTool) Name() string {
	return "noop"
}

func (noopTool) Description() string {
	return "noop tool for loop tests"
}

func (noopTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
	}
}

func (noopTool) Execute(_ context.Context, _ *tools.ToolContext, _ map[string]any) (tools.ToolResult, error) {
	return tools.NewToolResult("ok"), nil
}

func TestRunNoIterationCapWhenMaxIterationsNonPositive(t *testing.T) {
	provider := &loopTestProvider{
		toolIterations: 51,
	}

	registry := tools.NewRegistry()
	registry.MustRegister(noopTool{})

	loop := NewAgentLoop(provider, registry)
	result, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "keep going"),
		},
		MaxIterations: 0,
		MaxMessages:   500,
	})
	if err != nil {
		t.Fatalf("expected run to complete without iteration cap, got error: %v", err)
	}

	// 51 tool-use iterations + 1 final end_turn iteration.
	wantIterations := 52
	if result.TotalIterations != wantIterations {
		t.Fatalf("expected %d iterations, got %d", wantIterations, result.TotalIterations)
	}
	if provider.callCount != wantIterations {
		t.Fatalf("expected provider call count %d, got %d", wantIterations, provider.callCount)
	}
}
