package orchestrator

import (
	"context"
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

type pipelineTestProvider struct {
	lastReq llm.AgentRequest
}

func (p *pipelineTestProvider) Name() string { return "pipeline-test-provider" }

func (p *pipelineTestProvider) Call(_ context.Context, req llm.AgentRequest) (llm.AgentResponse, error) {
	p.lastReq = req
	return llm.AgentResponse{
		Role:       llm.RoleAssistant,
		StopReason: llm.StopReasonEndTurn,
		Content: []llm.ContentBlock{
			{Type: llm.ContentTypeText, Text: "done"},
		},
	}, nil
}

func TestPipelineTransformRunsBeforeConvert(t *testing.T) {
	provider := &pipelineTestProvider{}
	loop := NewAgentLoop(provider, tools.NewRegistry())

	transformCalled := false
	convertCalled := false
	result, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "original"),
		},
		TransformContext: func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
			transformCalled = true
			return append(msgs, llm.NewTextMessage(llm.RoleUser, "transform marker")), nil
		},
		ConvertToLlm: func(_ context.Context, msgs []llm.Message, _ string) ([]llm.Message, error) {
			convertCalled = true
			foundMarker := false
			for _, m := range msgs {
				if m.GetText() == "transform marker" {
					foundMarker = true
					break
				}
			}
			if !foundMarker {
				t.Fatalf("convertToLlm called before transformContext output applied")
			}
			return []llm.Message{llm.NewTextMessage(llm.RoleUser, "converted only")}, nil
		},
		MaxIterations: 1,
		MaxMessages:   10,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.TotalIterations != 1 {
		t.Fatalf("iterations = %d, want 1", result.TotalIterations)
	}
	if !transformCalled {
		t.Fatalf("expected transformContext to be called")
	}
	if !convertCalled {
		t.Fatalf("expected convertToLlm to be called")
	}
	if len(provider.lastReq.Messages) != 1 || provider.lastReq.Messages[0].GetText() != "converted only" {
		t.Fatalf("provider received unexpected messages: %+v", provider.lastReq.Messages)
	}
}

func TestPipelineCanDisableDefaultContextRules(t *testing.T) {
	provider := &pipelineTestProvider{}
	loop := NewAgentLoop(provider, tools.NewRegistry())

	_, err := loop.Run(context.Background(), OrchestratorRequest{
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "m1"),
			llm.NewTextMessage(llm.RoleUser, "m2"),
			llm.NewTextMessage(llm.RoleUser, "m3"),
		},
		MaxIterations:              1,
		MaxMessages:                1,
		DisableDefaultContextRules: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := len(provider.lastReq.Messages); got != 3 {
		t.Fatalf("provider message count = %d, want 3 when default rules are disabled", got)
	}
}
