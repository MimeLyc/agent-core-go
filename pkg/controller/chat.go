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
	SystemPrompt string
	SoulFile     string
	DefaultDir   string
}

// ChatRequest is the JSON body for POST /api/chat.
type ChatRequest struct {
	Message string `json:"message"`
	WorkDir string `json:"work_dir,omitempty"`
}

// ChatResponse is the JSON response from POST /api/chat.
type ChatResponse struct {
	Reply    string    `json:"reply"`
	Decision string   `json:"decision"`
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[chat-controller] failed to write response: %v", err)
	}
}

// ContextWithTimeout wraps context.WithTimeout for use in tests/callers.
var ContextWithTimeout = context.WithTimeout
