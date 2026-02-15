package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewLLMProvider(t *testing.T) {
	tests := []struct {
		name        string
		cfg         LLMProviderConfig
		wantName    string
		wantErr     bool
		errContains string
	}{
		{
			name: "claude provider",
			cfg: LLMProviderConfig{
				Type:    ProviderClaude,
				BaseURL: "https://api.anthropic.com",
				APIKey:  "test-key",
				Model:   "claude-3-sonnet",
			},
			wantName: "claude",
			wantErr:  false,
		},
		{
			name: "openai provider",
			cfg: LLMProviderConfig{
				Type:    ProviderOpenAI,
				BaseURL: "https://api.openai.com",
				APIKey:  "test-key",
				Model:   "gpt-4",
			},
			wantName: "openai",
			wantErr:  false,
		},
		{
			name: "default to claude",
			cfg: LLMProviderConfig{
				Type:    "",
				BaseURL: "https://api.anthropic.com",
				APIKey:  "test-key",
				Model:   "claude-3-sonnet",
			},
			wantName: "claude",
			wantErr:  false,
		},
		{
			name: "unknown provider",
			cfg: LLMProviderConfig{
				Type:    "unknown",
				BaseURL: "https://example.com",
				APIKey:  "test-key",
				Model:   "model",
			},
			wantErr:     true,
			errContains: "unknown LLM provider type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewLLMProvider(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewLLMProvider() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewLLMProvider() error = %v", err)
				return
			}
			if provider.Name() != tt.wantName {
				t.Errorf("provider.Name() = %v, want %v", provider.Name(), tt.wantName)
			}
		})
	}
}

func TestClaudeProviderCall(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		// Return mock response
		resp := AgentResponse{
			ID:         "msg_123",
			Type:       "message",
			Role:       RoleAssistant,
			Model:      "claude-3-sonnet",
			StopReason: StopReasonEndTurn,
			Content: []ContentBlock{
				{Type: ContentTypeText, Text: "Hello, world!"},
			},
			Usage: Usage{InputTokens: 10, OutputTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewClaudeProvider(LLMProviderConfig{
		Type:           ProviderClaude,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "claude-3-sonnet",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Hello"),
		},
	}

	resp, err := provider.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if resp.ID != "msg_123" {
		t.Errorf("resp.ID = %v, want msg_123", resp.ID)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("resp.StopReason = %v, want end_turn", resp.StopReason)
	}
	if resp.GetText() != "Hello, world!" {
		t.Errorf("resp.GetText() = %v, want 'Hello, world!'", resp.GetText())
	}
}

func TestClaudeProviderValidation(t *testing.T) {
	tests := []struct {
		name        string
		provider    *ClaudeProvider
		errContains string
	}{
		{
			name:        "empty base URL",
			provider:    &ClaudeProvider{BaseURL: "", APIKey: "key", Model: "model"},
			errContains: "base URL is empty",
		},
		{
			name:        "empty API key",
			provider:    &ClaudeProvider{BaseURL: "http://example.com", APIKey: "", Model: "model"},
			errContains: "API key is empty",
		},
		{
			name:        "empty model",
			provider:    &ClaudeProvider{BaseURL: "http://example.com", APIKey: "key", Model: ""},
			errContains: "model is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.provider.Call(context.Background(), AgentRequest{})
			if err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
				t.Errorf("error = %v, want to contain %v", err, tt.errContains)
			}
		})
	}
}

func TestOpenAIProviderCall(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header with Bearer token")
		}

		// Return mock OpenAI response
		resp := map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from OpenAI!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(LLMProviderConfig{
		Type:           ProviderOpenAI,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "gpt-4",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Hello"),
		},
	}

	resp, err := provider.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("resp.StopReason = %v, want end_turn", resp.StopReason)
	}
	if resp.GetText() != "Hello from OpenAI!" {
		t.Errorf("resp.GetText() = %v, want 'Hello from OpenAI!'", resp.GetText())
	}
}

func TestOpenAIProviderToolCalls(t *testing.T) {
	// Create a mock server that returns tool calls
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]any{
									"name":      "read_file",
									"arguments": `{"path": "/tmp/test.txt"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(LLMProviderConfig{
		Type:           ProviderOpenAI,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "gpt-4",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Read a file"),
		},
		Tools: []ToolDefinition{
			{
				Name:        "read_file",
				Description: "Read a file",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	resp, err := provider.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("resp.StopReason = %v, want tool_use", resp.StopReason)
	}

	toolUses := resp.GetToolUses()
	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(toolUses))
	}
	if toolUses[0].Name != "read_file" {
		t.Errorf("tool name = %v, want read_file", toolUses[0].Name)
	}
}

func TestOpenAIProviderToolCallsWithStopFinishReason(t *testing.T) {
	// Some OpenAI-compatible providers return finish_reason=stop even when tool_calls exist.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "resp_05f29ac5928a29dc01698bf0ffe56c8197a963bfbdbfab4829",
			"object":  "chat.completion",
			"created": int64(1770778880),
			"model":   "gpt-5.2-2025-12-11",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1FGKNEreFHX0dKSGkQ0U6p07",
								"type": "function",
								"function": map[string]any{
									"name":      "ping",
									"arguments": `{}`,
								},
							},
						},
						"function_call": nil,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     2605,
				"completion_tokens": 24,
				"total_tokens":      2629,
			},
			"prompt_tokens_details": map[string]int{
				"cached_tokens": 0,
			},
			"completion_tokens_details": map[string]int{
				"reasoning_tokens": 8,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(LLMProviderConfig{
		Type:           ProviderOpenAI,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "gpt-4",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Call ping tool"),
		},
		Tools: []ToolDefinition{
			{
				Name:        "ping",
				Description: "test",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
	}

	resp, err := provider.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("resp.StopReason = %v, want tool_use", resp.StopReason)
	}

	toolUses := resp.GetToolUses()
	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool use, got %d", len(toolUses))
	}
	if toolUses[0].Name != "ping" {
		t.Errorf("tool name = %v, want ping", toolUses[0].Name)
	}
	if toolUses[0].ID != "call_1FGKNEreFHX0dKSGkQ0U6p07" {
		t.Errorf("tool id = %v, want call_1FGKNEreFHX0dKSGkQ0U6p07", toolUses[0].ID)
	}
	if len(toolUses[0].Input) != 0 {
		t.Errorf("tool input = %v, want empty object", toolUses[0].Input)
	}
}

func TestOpenAIProviderStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		streamFlag, _ := payload["stream"].(bool)
		if !streamFlag {
			t.Fatalf("expected stream=true in request payload")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIProvider(LLMProviderConfig{
		Type:           ProviderOpenAI,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "gpt-4",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "hello"),
		},
	}

	deltas := make([]string, 0, 2)
	resp, err := provider.Stream(context.Background(), req, func(delta ContentBlockDelta) {
		deltas = append(deltas, delta.Text)
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "Hel" || deltas[1] != "lo" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
	if resp.GetText() != "Hello" {
		t.Fatalf("expected final text Hello, got %q", resp.GetText())
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Fatalf("expected stop reason end_turn, got %s", resp.StopReason)
	}
}

func TestAgentRunnerBackwardCompatibility(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := AgentResponse{
			ID:         "msg_123",
			Type:       "message",
			Role:       RoleAssistant,
			Model:      "claude-3-sonnet",
			StopReason: StopReasonEndTurn,
			Content: []ContentBlock{
				{Type: ContentTypeText, Text: "Test response"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Use the legacy AgentRunner
	runner := AgentRunner{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "claude-3-sonnet",
		Timeout: 30 * time.Second,
	}

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Hello"),
		},
	}

	resp, err := runner.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	if resp.GetText() != "Test response" {
		t.Errorf("resp.GetText() = %v, want 'Test response'", resp.GetText())
	}

	// Verify AgentRunner implements LLMProvider interface
	var _ LLMProvider = runner
	if runner.Name() != "claude" {
		t.Errorf("runner.Name() = %v, want claude", runner.Name())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestOpenAIProviderReasoningContentRoundTrip(t *testing.T) {
	var capturedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&capturedPayload); err != nil {
			t.Fatalf("decode request payload: %v", err)
		}

		resp := map[string]any{
			"id":      "chatcmpl-reasoning",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":              "assistant",
						"content":           "",
						"reasoning_content": "followed explicit chain",
						"tool_calls": []map[string]any{
							{
								"id":   "call_2",
								"type": "function",
								"function": map[string]any{
									"name":      "lookup",
									"arguments": `{"term":"Trinity"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(LLMProviderConfig{
		Type:           ProviderOpenAI,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "gpt-4",
		TimeoutSeconds: 30,
	})

	req := AgentRequest{
		Messages: []Message{
			NewTextMessage(RoleUser, "Use lookup"),
			{
				Role:             RoleAssistant,
				ReasoningContent: "thought steps",
				Content: []ContentBlock{
					{
						Type:  ContentTypeToolUse,
						ID:    "call_1",
						Name:  "lookup",
						Input: map[string]any{"term": "Neo"},
					},
				},
			},
			NewToolResultMessage("call_1", "ok", false),
		},
		Tools: []ToolDefinition{
			{
				Name:        "lookup",
				Description: "lookup",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}

	resp, err := provider.Call(context.Background(), req)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	messages, ok := capturedPayload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("captured messages missing in payload: %#v", capturedPayload)
	}

	foundAssistantWithToolCall := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] != "assistant" {
			continue
		}
		if _, hasToolCalls := msg["tool_calls"]; !hasToolCalls {
			continue
		}
		if got := msg["reasoning_content"]; got != "thought steps" {
			t.Fatalf("assistant reasoning_content = %#v, want %q", got, "thought steps")
		}
		foundAssistantWithToolCall = true
	}

	if !foundAssistantWithToolCall {
		t.Fatalf("did not find assistant tool_call message in request payload: %#v", messages)
	}

	if resp.ReasoningContent != "followed explicit chain" {
		t.Fatalf("resp.ReasoningContent = %q, want %q", resp.ReasoningContent, "followed explicit chain")
	}
	if msg := resp.ToMessage(); msg.ReasoningContent != "followed explicit chain" {
		t.Fatalf("ToMessage().ReasoningContent = %q, want %q", msg.ReasoningContent, "followed explicit chain")
	}
}
