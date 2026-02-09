package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	openaiAPIPath            = "/v1/chat/completions"
	defaultOpenAIMaxAttempts = 5
	defaultOpenAIBackoffSec  = 2
	defaultOpenAIMaxTokens   = 4096
)

// OpenAIProvider implements LLMProvider for OpenAI-compatible APIs.
// This supports OpenAI, OpenRouter, DeepSeek, and other compatible endpoints.
type OpenAIProvider struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Timeout     time.Duration
	MaxAttempts int
	HTTPClient  *http.Client
	Backoff     func(int) time.Duration
	Sleep       func(time.Duration)
}

// NewOpenAIProvider creates a new OpenAI-compatible API provider.
func NewOpenAIProvider(cfg LLMProviderConfig) *OpenAIProvider {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultOpenAIMaxAttempts
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultOpenAIMaxTokens
	}

	return &OpenAIProvider{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		MaxTokens:   maxTokens,
		Timeout:     timeout,
		MaxAttempts: maxAttempts,
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Call sends an AgentRequest to the OpenAI-compatible API and returns the response.
// It converts between Claude message format and OpenAI format.
func (p *OpenAIProvider) Call(ctx context.Context, req AgentRequest) (AgentResponse, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return AgentResponse{}, errors.New("OpenAI API base URL is empty")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return AgentResponse{}, errors.New("OpenAI API key is empty")
	}
	if strings.TrimSpace(p.Model) == "" {
		return AgentResponse{}, errors.New("OpenAI API model is empty")
	}

	// Convert Claude request to OpenAI format
	openaiReq := p.convertToOpenAIRequest(req)

	log.Printf("[openai-provider] calling API: model=%s max_tokens=%d messages=%d tools=%d",
		openaiReq.Model, openaiReq.MaxTokens, len(openaiReq.Messages), len(openaiReq.Tools))

	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	log.Printf("[openai-provider] request payload size: %d bytes", len(payload))

	maxAttempts := p.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultOpenAIMaxAttempts
	}
	backoff := p.Backoff
	if backoff == nil {
		backoff = openaiDefaultBackoff
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: p.Timeout}
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("[openai-provider] API request attempt %d/%d", attempt, maxAttempts)
		respBody, status, err := p.doRequest(ctx, client, payload)
		log.Printf("[openai-provider] API response: status=%d body_size=%d err=%v", status, len(respBody), err)

		if err == nil && status < 400 {
			resp, parseErr := p.parseOpenAIResponse(respBody)
			if parseErr != nil {
				log.Printf("[openai-provider] ERROR: failed to parse response: %v", parseErr)
				log.Printf("[openai-provider] response body: %s", string(respBody))
				return AgentResponse{}, parseErr
			}
			log.Printf("[openai-provider] parsed response: stop_reason=%s content_blocks=%d",
				resp.StopReason, len(resp.Content))
			return resp, nil
		}
		lastErr = wrapOpenAIAPIError(respBody, status, err)
		log.Printf("[openai-provider] ERROR: attempt %d failed: %v", attempt, lastErr)
		if attempt == maxAttempts || !shouldRetryOpenAI(status, err) {
			log.Printf("[openai-provider] giving up after %d attempts", attempt)
			return AgentResponse{}, lastErr
		}
		backoffDuration := backoff(attempt)
		log.Printf("[openai-provider] retrying in %v", backoffDuration)
		sleep(backoffDuration)
	}
	return AgentResponse{}, lastErr
}

// OpenAI request/response types

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"` // string or []openaiContentPart
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// convertToOpenAIRequest converts a Claude AgentRequest to OpenAI format.
func (p *OpenAIProvider) convertToOpenAIRequest(req AgentRequest) openaiRequest {
	model := req.Model
	if model == "" {
		model = p.Model
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.MaxTokens
	}

	// Convert messages
	messages := make([]openaiMessage, 0, len(req.Messages)+1)

	// Add system message if present
	if req.System != "" {
		messages = append(messages, openaiMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert each message
	for _, msg := range req.Messages {
		openaiMsg := p.convertMessage(msg)
		messages = append(messages, openaiMsg...)
	}

	// Convert tools
	var tools []openaiTool
	for _, t := range req.Tools {
		tools = append(tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	openaiReq := openaiRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}

	if len(tools) > 0 {
		openaiReq.Tools = tools
		openaiReq.ToolChoice = "auto"
	}

	return openaiReq
}

// convertMessage converts a Claude message to OpenAI message(s).
func (p *OpenAIProvider) convertMessage(msg Message) []openaiMessage {
	var result []openaiMessage

	switch msg.Role {
	case RoleUser:
		// Check if this is a tool result message
		var toolResults []ContentBlock
		var otherContent []ContentBlock
		for _, block := range msg.Content {
			if block.Type == ContentTypeToolResult {
				toolResults = append(toolResults, block)
			} else {
				otherContent = append(otherContent, block)
			}
		}

		// Handle tool results as separate messages
		for _, tr := range toolResults {
			result = append(result, openaiMessage{
				Role:       "tool",
				Content:    tr.Content,
				ToolCallID: tr.ToolUseID,
			})
		}

		// Handle other content
		if len(otherContent) > 0 {
			text := ""
			for _, block := range otherContent {
				if block.Type == ContentTypeText {
					text += block.Text
				}
			}
			if text != "" {
				result = append(result, openaiMessage{
					Role:    "user",
					Content: text,
				})
			}
		}

	case RoleAssistant:
		// Check for tool calls
		var toolCalls []openaiToolCall
		var textContent string

		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeText:
				textContent += block.Text
			case ContentTypeToolUse:
				argsJSON, _ := json.Marshal(block.Input)
				toolCalls = append(toolCalls, openaiToolCall{
					ID:   block.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      block.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		assistantMsg := openaiMessage{
			Role:    "assistant",
			Content: textContent,
		}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		result = append(result, assistantMsg)
	}

	return result
}

// parseOpenAIResponse converts an OpenAI response to Claude AgentResponse format.
func (p *OpenAIProvider) parseOpenAIResponse(body []byte) (AgentResponse, error) {
	if len(body) == 0 {
		return AgentResponse{}, errors.New("API returned empty response body")
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return AgentResponse{}, fmt.Errorf("parse response: %w (body: %s)", err, truncateForLog(string(body), 500))
	}

	if len(openaiResp.Choices) == 0 {
		return AgentResponse{}, errors.New("OpenAI response has no choices")
	}

	choice := openaiResp.Choices[0]
	msg := choice.Message

	// Convert to Claude format
	var content []ContentBlock

	// Add text content
	if text, ok := msg.Content.(string); ok && text != "" {
		content = append(content, ContentBlock{
			Type: ContentTypeText,
			Text: text,
		})
	}

	// Add tool calls
	for _, tc := range msg.ToolCalls {
		var input map[string]any
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		content = append(content, ContentBlock{
			Type:  ContentTypeToolUse,
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	// Map finish reason to stop reason
	var stopReason StopReason
	switch choice.FinishReason {
	case "stop":
		stopReason = StopReasonEndTurn
	case "tool_calls":
		stopReason = StopReasonToolUse
	case "length":
		stopReason = StopReasonMaxTokens
	default:
		stopReason = StopReasonEndTurn
	}

	return AgentResponse{
		ID:         openaiResp.ID,
		Type:       "message",
		Role:       RoleAssistant,
		Content:    content,
		Model:      openaiResp.Model,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  openaiResp.Usage.PromptTokens,
			OutputTokens: openaiResp.Usage.CompletionTokens,
		},
	}, nil
}

func (p *OpenAIProvider) doRequest(ctx context.Context, client *http.Client, payload []byte) ([]byte, int, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	// Avoid doubling the path if BaseURL already includes it
	if strings.HasSuffix(base, openaiAPIPath) {
		// BaseURL already contains the full path, use as-is
	} else {
		base = base + openaiAPIPath
	}
	endpoint := base
	log.Printf("[openai-provider] POST %s", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	// OpenAI uses Bearer token authentication
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[openai-provider] HTTP request failed: %v", err)
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func wrapOpenAIAPIError(body []byte, status int, err error) error {
	if err != nil {
		return err
	}
	if status == 0 {
		return errors.New("OpenAI API request failed")
	}

	// Try to parse error response
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("OpenAI API error %d: %s - %s", status, errResp.Error.Type, errResp.Error.Message)
	}

	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("OpenAI API error: %d %s", status, msg)
}

func shouldRetryOpenAI(status int, err error) bool {
	if err != nil {
		return true
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout {
		return true
	}
	return status >= 500
}

func openaiDefaultBackoff(attempt int) time.Duration {
	base := float64(defaultOpenAIBackoffSec) * float64(time.Second)
	factor := math.Pow(2, float64(attempt-1))
	jitter := 0.5 + rand.Float64()
	return time.Duration(base * factor * jitter)
}
