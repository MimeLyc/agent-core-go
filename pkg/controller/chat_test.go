package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
)

// stubAgent implements agent.Agent for testing.
type stubAgent struct {
	result    agent.AgentResult
	err       error
	lastReq   agent.AgentRequest
	stream    []agent.AgentStreamEvent
	streamErr error
}

func (s *stubAgent) Execute(_ context.Context, req agent.AgentRequest) (agent.AgentResult, error) {
	s.lastReq = req
	return s.result, s.err
}

func (s *stubAgent) Capabilities() agent.AgentCapabilities {
	return agent.AgentCapabilities{}
}

func (s *stubAgent) ExecuteStream(_ context.Context, req agent.AgentRequest) (<-chan agent.AgentStreamEvent, <-chan error) {
	s.lastReq = req
	eventCh := make(chan agent.AgentStreamEvent, len(s.stream))
	errCh := make(chan error, 1)
	for _, evt := range s.stream {
		eventCh <- evt
	}
	close(eventCh)
	if s.streamErr != nil {
		errCh <- s.streamErr
	}
	close(errCh)
	return eventCh, errCh
}

func (s *stubAgent) Close() error { return nil }

func TestHandleChat_Success(t *testing.T) {
	stub := &stubAgent{
		result: agent.AgentResult{
			Success:  true,
			Decision: agent.DecisionProceed,
			Message:  "Hello back!",
			Usage: agent.ExecutionUsage{
				TotalIterations:   2,
				TotalInputTokens:  100,
				TotalOutputTokens: 50,
			},
		},
	}

	ctrl := NewChatController(stub, ChatConfig{
		SystemPrompt: "test prompt",
		DefaultDir:   "/tmp",
	})

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.HandleChat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Reply != "Hello back!" {
		t.Errorf("expected reply 'Hello back!', got %q", resp.Reply)
	}
	if resp.Decision != "proceed" {
		t.Errorf("expected decision 'proceed', got %q", resp.Decision)
	}
	if resp.Usage.Iterations != 2 {
		t.Errorf("expected 2 iterations, got %d", resp.Usage.Iterations)
	}

	// Verify agent received correct request
	if stub.lastReq.Task != "hello" {
		t.Errorf("expected task 'hello', got %q", stub.lastReq.Task)
	}
	if stub.lastReq.SystemPrompt != "test prompt" {
		t.Errorf("expected system prompt 'test prompt', got %q", stub.lastReq.SystemPrompt)
	}
	if stub.lastReq.WorkDir != "/tmp" {
		t.Errorf("expected work dir '/tmp', got %q", stub.lastReq.WorkDir)
	}
}

func TestHandleChat_EmptyMessage(t *testing.T) {
	ctrl := NewChatController(&stubAgent{}, ChatConfig{})

	body := `{"message":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	ctrl.HandleChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleChat_InvalidJSON(t *testing.T) {
	ctrl := NewChatController(&stubAgent{}, ChatConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	ctrl.HandleChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleChat_CustomWorkDir(t *testing.T) {
	stub := &stubAgent{
		result: agent.AgentResult{Decision: agent.DecisionProceed},
	}
	ctrl := NewChatController(stub, ChatConfig{DefaultDir: "/default"})

	body := `{"message":"hi","work_dir":"/custom"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	ctrl.HandleChat(w, req)

	if stub.lastReq.WorkDir != "/custom" {
		t.Errorf("expected work dir '/custom', got %q", stub.lastReq.WorkDir)
	}
}

func TestHandleChat_AgentError(t *testing.T) {
	stub := &stubAgent{
		err: context.DeadlineExceeded,
	}
	ctrl := NewChatController(stub, ChatConfig{})

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	ctrl.HandleChat(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	ctrl := NewChatController(&stubAgent{}, ChatConfig{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	ctrl.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleChatStream_Success(t *testing.T) {
	stub := &stubAgent{
		stream: []agent.AgentStreamEvent{
			{Type: agent.AgentEventAgentStart},
			{Type: agent.AgentEventMessageDelta, Delta: "Hel"},
			{Type: agent.AgentEventMessageDelta, Delta: "lo"},
			{Type: agent.AgentEventAgentEnd},
		},
	}
	ctrl := NewChatController(stub, ChatConfig{
		DefaultDir:      "/tmp",
		EnableStreaming: true,
	})

	body := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat/stream", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctrl.HandleChatStream(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: message_delta") {
		t.Fatalf("expected SSE stream output, got %q", w.Body.String())
	}
}
