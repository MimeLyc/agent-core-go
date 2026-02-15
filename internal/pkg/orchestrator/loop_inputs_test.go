package orchestrator

import (
	"context"
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

type loopInputTestProvider struct {
	responses []llm.AgentResponse
	callCount int
}

func (p *loopInputTestProvider) Name() string {
	return "loop-input-test-provider"
}

func (p *loopInputTestProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	if p.callCount >= len(p.responses) {
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
	resp := p.responses[p.callCount]
	p.callCount++
	return resp, nil
}

func TestRunAppliesSteeringAfterEndTurn(t *testing.T) {
	provider := &loopInputTestProvider{
		responses: []llm.AgentResponse{
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "first"},
				},
			},
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "second"},
				},
			},
		},
	}

	loop := NewAgentLoop(provider, tools.NewRegistry())
	steeringCalls := 0
	result, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "start"),
		},
		MaxIterations: 10,
		GetSteeringMessages: func(_ context.Context, snapshot LoopInputSnapshot) ([]llm.Message, error) {
			if snapshot.Iteration == 1 && steeringCalls == 0 {
				steeringCalls++
				return []llm.Message{
					llm.NewTextMessage(llm.RoleUser, "steer now"),
				}, nil
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if provider.callCount != 2 {
		t.Fatalf("expected 2 provider calls, got %d", provider.callCount)
	}
	if result.GetFinalText() != "second" {
		t.Fatalf("expected final response to come from second turn, got %q", result.GetFinalText())
	}
}

func TestRunAppliesSteeringBeforeFollowUp(t *testing.T) {
	provider := &loopInputTestProvider{
		responses: []llm.AgentResponse{
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "first"},
				},
			},
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "second"},
				},
			},
		},
	}

	loop := NewAgentLoop(provider, tools.NewRegistry())
	injected := make([]string, 0, 2)
	result, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "start"),
		},
		MaxIterations: 10,
		GetSteeringMessages: func(_ context.Context, snapshot LoopInputSnapshot) ([]llm.Message, error) {
			if snapshot.Iteration == 1 {
				return []llm.Message{llm.NewTextMessage(llm.RoleUser, "steering")}, nil
			}
			return nil, nil
		},
		GetFollowUpMessages: func(_ context.Context, snapshot LoopInputSnapshot) ([]llm.Message, error) {
			if snapshot.Iteration == 1 {
				return []llm.Message{llm.NewTextMessage(llm.RoleUser, "follow-up")}, nil
			}
			return nil, nil
		},
		OnSteeringApplied: func(messages []llm.Message) {
			for _, m := range messages {
				injected = append(injected, m.GetText())
			}
		},
		OnFollowUpApplied: func(messages []llm.Message) {
			for _, m := range messages {
				injected = append(injected, m.GetText())
			}
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.GetFinalText() != "second" {
		t.Fatalf("expected final response to come from second turn, got %q", result.GetFinalText())
	}
	if len(injected) != 2 {
		t.Fatalf("expected 2 injected messages, got %d (%v)", len(injected), injected)
	}
	if injected[0] != "steering" || injected[1] != "follow-up" {
		t.Fatalf("expected steering then follow-up, got %v", injected)
	}
}

func TestRunChecksLoopInputsAfterEachToolExecution(t *testing.T) {
	provider := &loopInputTestProvider{
		responses: []llm.AgentResponse{
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonToolUse,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeToolUse, ID: "tool-1", Name: "noop", Input: map[string]any{}},
					{Type: llm.ContentTypeToolUse, ID: "tool-2", Name: "noop", Input: map[string]any{}},
				},
			},
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "second"},
				},
			},
		},
	}

	registry := tools.NewRegistry()
	registry.MustRegister(noopTool{})
	loop := NewAgentLoop(provider, registry)

	checkCalls := 0
	result, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "start"),
		},
		MaxIterations: 10,
		GetSteeringMessages: func(_ context.Context, _ LoopInputSnapshot) ([]llm.Message, error) {
			checkCalls++
			if checkCalls == 1 {
				return []llm.Message{llm.NewTextMessage(llm.RoleUser, "interrupt")}, nil
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if provider.callCount != 2 {
		t.Fatalf("expected 2 provider calls, got %d", provider.callCount)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected only one tool call to execute before steering interrupt, got %d", len(result.ToolCalls))
	}
}
