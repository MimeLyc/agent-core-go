package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	agenttypes "github.com/MimeLyc/agent-core-go/pkg/agent/types"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

type apiAgentSequentialEndTurnProvider struct {
	responses []llm.AgentResponse
	callCount int
}

func (p *apiAgentSequentialEndTurnProvider) Name() string {
	return "api-agent-sequential-end-turn-provider"
}

func (p *apiAgentSequentialEndTurnProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	if p.callCount >= len(p.responses) {
		return llm.AgentResponse{
			Role:       llm.RoleAssistant,
			StopReason: llm.StopReasonEndTurn,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeText, Text: "done"},
			},
		}, nil
	}
	resp := p.responses[p.callCount]
	p.callCount++
	return resp, nil
}

type apiAgentCallErrorProvider struct {
	err error
}

func (p apiAgentCallErrorProvider) Name() string {
	return "api-agent-call-error-provider"
}

func (p apiAgentCallErrorProvider) Call(_ context.Context, _ llm.AgentRequest) (llm.AgentResponse, error) {
	return llm.AgentResponse{}, p.err
}

func collectStreamResults(
	t *testing.T,
	events <-chan AgentStreamEvent,
	errs <-chan error,
) ([]AgentStreamEvent, []error) {
	t.Helper()

	var streamEvents []AgentStreamEvent
	var streamErrors []error
	timeout := time.After(2 * time.Second)

	for events != nil || errs != nil {
		select {
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			streamEvents = append(streamEvents, evt)
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				streamErrors = append(streamErrors, err)
			}
		case <-timeout:
			t.Fatalf("timed out while collecting stream output")
		}
	}

	return streamEvents, streamErrors
}

func findEventIndex(events []AgentStreamEvent, typ AgentEventType) int {
	for idx, evt := range events {
		if evt.Type == typ {
			return idx
		}
	}
	return -1
}

func TestExecuteStreamBehavior_GivenStreamingDisabled_WhenExecuteStream_ThenReturnsConfigError(t *testing.T) {
	// Given: agent-level streaming is disabled and request does not override it.
	a := NewAPIAgent(apiAgentTestProvider{}, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: false,
	})

	// When: ExecuteStream is called.
	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "stream please",
	})
	streamEvents, streamErrors := collectStreamResults(t, events, errs)

	// Then: no stream events are emitted and configuration error is reported.
	if len(streamEvents) != 0 {
		t.Fatalf("expected no stream events, got %d", len(streamEvents))
	}
	if len(streamErrors) != 1 {
		t.Fatalf("expected exactly one stream error, got %d", len(streamErrors))
	}
	if !strings.Contains(streamErrors[0].Error(), "streaming is disabled") {
		t.Fatalf("expected disabled-streaming error, got %v", streamErrors[0])
	}
}

func TestExecuteStreamBehavior_GivenRequestLevelStreamingOverride_WhenExecuteStream_ThenEmitsDeltaAndEndEvents(t *testing.T) {
	// Given: agent-level streaming is disabled, but request explicitly enables it.
	a := NewAPIAgent(apiAgentStreamingProvider{}, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: false,
	})

	// When: ExecuteStream is called with request-level streaming enabled.
	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "stream please",
		Options: AgentOptions{
			EnableStreaming: true,
		},
	})
	streamEvents, streamErrors := collectStreamResults(t, events, errs)

	// Then: stream should run with delta events and a terminal agent_end event.
	if len(streamErrors) != 0 {
		t.Fatalf("expected no stream errors, got %v", streamErrors)
	}
	if len(streamEvents) == 0 {
		t.Fatalf("expected stream events, got none")
	}

	startIdx := findEventIndex(streamEvents, AgentEventAgentStart)
	deltaIdx := findEventIndex(streamEvents, AgentEventMessageDelta)
	messageEndIdx := findEventIndex(streamEvents, AgentEventMessageEnd)
	agentEndIdx := findEventIndex(streamEvents, AgentEventAgentEnd)

	if startIdx != 0 {
		t.Fatalf("expected first event to be agent_start, got %v", streamEvents)
	}
	if deltaIdx == -1 {
		t.Fatalf("expected at least one message_delta event, got %v", streamEvents)
	}
	if messageEndIdx == -1 {
		t.Fatalf("expected message_end event, got %v", streamEvents)
	}
	if agentEndIdx == -1 {
		t.Fatalf("expected agent_end event, got %v", streamEvents)
	}
	if !(startIdx < deltaIdx && deltaIdx < messageEndIdx && messageEndIdx < agentEndIdx) {
		t.Fatalf("expected start < delta < message_end < agent_end, got %v", streamEvents)
	}
	raw, err := json.Marshal(streamEvents[agentEndIdx])
	if err != nil {
		t.Fatalf("marshal agent_end event: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode agent_end event: %v", err)
	}
	if _, ok := payload["decision"]; ok {
		t.Fatalf("expected no decision in agent_end event, got %+v", payload)
	}
}

func TestExecuteStreamBehavior_GivenProviderWithoutStreamingSupport_WhenExecuteStream_ThenFallsBackGracefully(t *testing.T) {
	// Given: streaming is enabled but provider only implements Call (no Stream).
	provider := &apiAgentSequentialEndTurnProvider{
		responses: []llm.AgentResponse{
			{
				Role:       llm.RoleAssistant,
				StopReason: llm.StopReasonEndTurn,
				Content: []llm.ContentBlock{
					{Type: llm.ContentTypeText, Text: "fallback"},
				},
			},
		},
	}
	a := NewAPIAgent(provider, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: true,
	})

	// When: ExecuteStream is called.
	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "fallback stream",
	})
	streamEvents, streamErrors := collectStreamResults(t, events, errs)

	// Then: we still get coarse stream events and no error.
	if len(streamErrors) != 0 {
		t.Fatalf("expected no stream errors, got %v", streamErrors)
	}
	if len(streamEvents) != 3 {
		t.Fatalf("expected 3 coarse events (start, message_end, end), got %d (%v)", len(streamEvents), streamEvents)
	}
	if streamEvents[0].Type != AgentEventAgentStart {
		t.Fatalf("expected first event agent_start, got %s", streamEvents[0].Type)
	}
	if streamEvents[1].Type != AgentEventMessageEnd || streamEvents[1].Message != "fallback" {
		t.Fatalf("expected message_end fallback, got %+v", streamEvents[1])
	}
	if streamEvents[2].Type != AgentEventAgentEnd {
		t.Fatalf("expected final event agent_end, got %s", streamEvents[2].Type)
	}
}

func TestExecuteStreamBehavior_GivenRuntimeSteeringAndFollowUp_WhenExecuteStream_ThenEmitsAppliedEventsInOrder(t *testing.T) {
	// Given: provider ends turn twice and request injects steering + follow-up after first turn.
	provider := &apiAgentSequentialEndTurnProvider{
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
	a := NewAPIAgent(provider, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: true,
	})

	// When: ExecuteStream runs with loop-input providers.
	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "inject runtime guidance",
		Options: AgentOptions{
			GetSteeringMessages: func(_ context.Context, snapshot LoopInputSnapshot) ([]agenttypes.Message, error) {
				if snapshot.Iteration == 1 {
					return []agenttypes.Message{agenttypes.NewTextMessage(agenttypes.RoleUser, "steer now")}, nil
				}
				return nil, nil
			},
			GetFollowUpMessages: func(_ context.Context, snapshot LoopInputSnapshot) ([]agenttypes.Message, error) {
				if snapshot.Iteration == 1 {
					return []agenttypes.Message{agenttypes.NewTextMessage(agenttypes.RoleUser, "follow up now")}, nil
				}
				return nil, nil
			},
		},
	})
	streamEvents, streamErrors := collectStreamResults(t, events, errs)

	// Then: steering is emitted before follow-up, and execution still ends normally.
	if len(streamErrors) != 0 {
		t.Fatalf("expected no stream errors, got %v", streamErrors)
	}
	steeringIdx := findEventIndex(streamEvents, AgentEventSteeringApplied)
	followUpIdx := findEventIndex(streamEvents, AgentEventFollowUpApplied)
	agentEndIdx := findEventIndex(streamEvents, AgentEventAgentEnd)
	if steeringIdx == -1 || followUpIdx == -1 {
		t.Fatalf("expected steering and follow-up events, got %v", streamEvents)
	}
	if steeringIdx > followUpIdx {
		t.Fatalf("expected steering before follow-up, got %v", streamEvents)
	}
	if agentEndIdx == -1 {
		t.Fatalf("expected agent_end event, got %v", streamEvents)
	}
}

func TestExecuteStreamBehavior_GivenExecutionFails_WhenExecuteStream_ThenEmitsErrorWithoutAgentEnd(t *testing.T) {
	// Given: provider execution returns an error.
	expectedErr := errors.New("provider boom")
	a := NewAPIAgent(apiAgentCallErrorProvider{err: expectedErr}, tools.NewRegistry(), APIAgentOptions{
		EnableStreaming: true,
	})

	// When: ExecuteStream is called.
	events, errs := a.ExecuteStream(context.Background(), AgentRequest{
		Task: "failing stream",
	})
	streamEvents, streamErrors := collectStreamResults(t, events, errs)

	// Then: an error is surfaced and no agent_end event is emitted.
	if len(streamErrors) != 1 {
		t.Fatalf("expected one stream error, got %d (%v)", len(streamErrors), streamErrors)
	}
	if !errors.Is(streamErrors[0], expectedErr) {
		t.Fatalf("expected stream error %v, got %v", expectedErr, streamErrors[0])
	}
	if findEventIndex(streamEvents, AgentEventAgentEnd) != -1 {
		t.Fatalf("did not expect agent_end on failure, got %v", streamEvents)
	}
}
