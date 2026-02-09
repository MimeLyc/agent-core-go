package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

func TestE2EAgentModeAPIClaude(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var req llm.AgentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) == 0 {
			t.Fatalf("expected at least one message")
		}

		resp := llm.AgentResponse{
			ID:         "msg_e2e_claude",
			Type:       "message",
			Role:       llm.RoleAssistant,
			Model:      "claude-test",
			StopReason: llm.StopReasonEndTurn,
			Content: []llm.ContentBlock{
				{Type: llm.ContentTypeText, Text: `{"decision":"proceed","summary":"api claude mode e2e"}`},
			},
			Usage: llm.Usage{InputTokens: 10, OutputTokens: 5},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAPI,
		API: &agent.APIConfig{
			ProviderType: llm.ProviderClaude,
			BaseURL:      server.URL,
			APIKey:       "test-key",
			Model:        "claude-test",
			Timeout:      5 * time.Second,
			MaxAttempts:  1,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(api/claude) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for api claude mode",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(api/claude) error: %v", err)
	}

	if result.Decision != agent.DecisionProceed {
		t.Fatalf("expected decision proceed, got %s", result.Decision)
	}
	if result.Summary == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func TestE2EAgentModeAPIOpenAI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		resp := map[string]any{
			"id":      "chatcmpl_e2e_openai",
			"object":  "chat.completion",
			"created": int64(1),
			"model":   "gpt-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"decision":"proceed","summary":"api openai mode e2e"}`,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 6,
				"total_tokens":      16,
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAPI,
		API: &agent.APIConfig{
			ProviderType: llm.ProviderOpenAI,
			BaseURL:      server.URL,
			APIKey:       "test-key",
			Model:        "gpt-test",
			Timeout:      5 * time.Second,
			MaxAttempts:  1,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(api/openai) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for api openai mode",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(api/openai) error: %v", err)
	}

	if result.Decision != agent.DecisionProceed {
		t.Fatalf("expected decision proceed, got %s", result.Decision)
	}
	if result.Summary == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func TestE2EAgentModeCLI(t *testing.T) {
	t.Parallel()

	cli := writeFakeCLI(t, `{"result":"cli mode e2e output","error":"","session_id":"sess_cli"}`)

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeCLI,
		CLI: &agent.CLIAgentConfig{
			Command: cli,
			Timeout: 5 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(cli) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for cli mode",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(cli) error: %v", err)
	}

	if result.Decision != agent.DecisionProceed {
		t.Fatalf("expected decision proceed, got %s", result.Decision)
	}
}

func TestE2EAgentModeClaudeCodeAlias(t *testing.T) {
	t.Parallel()

	cli := writeFakeCLI(t, `{"result":"claude-code alias mode e2e","error":"","session_id":"sess_alias"}`)

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeClaudeCode,
		CLI: &agent.CLIAgentConfig{
			Command: cli,
			Timeout: 5 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(claude-code alias) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for claude-code alias mode",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(claude-code alias) error: %v", err)
	}

	if result.Decision != agent.DecisionProceed {
		t.Fatalf("expected decision proceed, got %s", result.Decision)
	}
}

func TestE2EAgentModeAutoPrefersAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"id":      "chatcmpl_e2e_auto_api",
			"object":  "chat.completion",
			"created": int64(1),
			"model":   "gpt-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"decision":"proceed","summary":"auto mode picked api"}`,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 6,
				"total_tokens":      16,
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAuto,
		API: &agent.APIConfig{
			ProviderType: llm.ProviderOpenAI,
			BaseURL:      server.URL,
			APIKey:       "test-key",
			Model:        "gpt-test",
			Timeout:      5 * time.Second,
			MaxAttempts:  1,
		},
		CLI: &agent.CLIAgentConfig{
			Command: "/path/that/does/not/exist",
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(auto with api) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for auto mode api preference",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(auto/api) error: %v", err)
	}

	if result.Summary == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func TestE2EAgentModeAutoFallsBackToCLI(t *testing.T) {
	t.Parallel()

	cli := writeFakeCLI(t, `{"result":"auto mode picked cli","error":"","session_id":"sess_auto_cli"}`)

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAuto,
		CLI: &agent.CLIAgentConfig{
			Command: cli,
			Timeout: 5 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(auto with cli fallback) error: %v", err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "run e2e for auto mode cli fallback",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute(auto/cli) error: %v", err)
	}

	if result.Decision != agent.DecisionProceed {
		t.Fatalf("expected decision proceed, got %s", result.Decision)
	}
}

func writeFakeCLI(t *testing.T, jsonLine string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude")
	script := "#!/bin/sh\nset -eu\nprintf '%s\\n' '" + jsonLine + "'\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake cli: %v", err)
	}
	return path
}
