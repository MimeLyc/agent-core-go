package controller

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
)

// ChatController handles HTTP requests for AI chat.
type ChatController struct {
	agent agent.Agent
	cfg   ChatConfig
}

// ChatConfig holds controller-level configuration.
type ChatConfig struct {
	SystemPrompt    string
	SoulFile        string
	DefaultDir      string
	EnableStreaming bool
}

// ChatRequest is the JSON body for POST /api/chat.
type ChatRequest struct {
	Message string `json:"message"`
	WorkDir string `json:"work_dir,omitempty"`
}

// ChatResponse is the JSON response from POST /api/chat.
type ChatResponse struct {
	Reply    string    `json:"reply"`
	Decision string    `json:"decision"`
	Usage    UsageInfo `json:"usage"`
}

// UsageInfo mirrors token/iteration stats.
type UsageInfo struct {
	Iterations   int `json:"iterations"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ErrorResponse is the JSON error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// NewChatController creates a ChatController.
func NewChatController(a agent.Agent, cfg ChatConfig) *ChatController {
	if cfg.DefaultDir == "" {
		cfg.DefaultDir = "."
	}
	return &ChatController{agent: a, cfg: cfg}
}

// RegisterRoutes wires the controller's handlers onto the given mux.
func (c *ChatController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/chat", c.HandleChat)
	mux.HandleFunc("POST /api/chat/stream", c.HandleChatStream)
	mux.HandleFunc("GET /healthz", c.HandleHealth)
}

// HandleChat processes a single chat request.
func (c *ChatController) HandleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "message is required"})
		return
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = c.cfg.DefaultDir
	}

	agentReq := agent.AgentRequest{
		Task:         req.Message,
		SystemPrompt: c.cfg.SystemPrompt,
		SoulFile:     c.cfg.SoulFile,
		WorkDir:      workDir,
	}

	result, err := c.agent.Execute(r.Context(), agentReq)
	if err != nil {
		log.Printf("[chat-controller] agent error: %v", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "agent execution failed: " + err.Error()})
		return
	}

	resp := ChatResponse{
		Reply:    result.Message,
		Decision: string(result.Decision),
		Usage: UsageInfo{
			Iterations:   result.Usage.TotalIterations,
			InputTokens:  result.Usage.TotalInputTokens,
			OutputTokens: result.Usage.TotalOutputTokens,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleHealth returns a simple health check.
func (c *ChatController) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleChatStream processes a streaming chat request using SSE.
func (c *ChatController) HandleChatStream(w http.ResponseWriter, r *http.Request) {
	if !c.cfg.EnableStreaming {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "streaming is disabled"})
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "message is required"})
		return
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = c.cfg.DefaultDir
	}

	agentReq := agent.AgentRequest{
		Task:         req.Message,
		SystemPrompt: c.cfg.SystemPrompt,
		SoulFile:     c.cfg.SoulFile,
		WorkDir:      workDir,
		Options: agent.AgentOptions{
			EnableStreaming: true,
		},
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "streaming is not supported by this server"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	events, errs := c.agent.ExecuteStream(r.Context(), agentReq)
	for events != nil || errs != nil {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if !writeSSEEvent(w, evt) {
				return
			}
			flusher.Flush()
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err == nil {
				continue
			}
			_ = writeSSEEvent(w, map[string]any{
				"type":  "error",
				"error": err.Error(),
			})
			flusher.Flush()
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[chat-controller] failed to write response: %v", err)
	}
}

func writeSSEEvent(w http.ResponseWriter, event any) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[chat-controller] failed to marshal SSE payload: %v", err)
		return false
	}

	eventName := "message"
	if ev, ok := event.(agent.AgentStreamEvent); ok && ev.Type != "" {
		eventName = string(ev.Type)
	}
	if _, err := w.Write([]byte("event: " + eventName + "\n")); err != nil {
		log.Printf("[chat-controller] failed to write SSE event name: %v", err)
		return false
	}
	if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
		log.Printf("[chat-controller] failed to write SSE data: %v", err)
		return false
	}
	return true
}

// ContextWithTimeout wraps context.WithTimeout for use in tests/callers.
var ContextWithTimeout = context.WithTimeout
