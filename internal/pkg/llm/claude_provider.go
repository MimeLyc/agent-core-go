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
	"net/url"
	"strings"
	"time"
)

const (
	claudeAPIPath           = "/v1/messages"
	defaultClaudeMaxAttempts = 5
	defaultClaudeBackoffSec  = 2
	defaultClaudeMaxTokens   = 4096
)

// ClaudeProvider implements LLMProvider for the Claude API.
// This is refactored from the original AgentRunner to implement the unified interface.
type ClaudeProvider struct {
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

// NewClaudeProvider creates a new Claude API provider.
func NewClaudeProvider(cfg LLMProviderConfig) *ClaudeProvider {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultClaudeMaxAttempts
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultClaudeMaxTokens
	}

	return &ClaudeProvider{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		MaxTokens:   maxTokens,
		Timeout:     timeout,
		MaxAttempts: maxAttempts,
	}
}

// Name returns the provider name.
func (p *ClaudeProvider) Name() string {
	return "claude"
}

// Call sends an AgentRequest to the Claude API and returns the response.
// This method preserves all the retry logic from the original AgentRunner.
func (p *ClaudeProvider) Call(ctx context.Context, req AgentRequest) (AgentResponse, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return AgentResponse{}, errors.New("Claude API base URL is empty")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return AgentResponse{}, errors.New("Claude API key is empty")
	}
	if strings.TrimSpace(p.Model) == "" {
		return AgentResponse{}, errors.New("Claude API model is empty")
	}

	// Set defaults
	if req.Model == "" {
		req.Model = p.Model
	}
	if req.MaxTokens == 0 {
		if p.MaxTokens > 0 {
			req.MaxTokens = p.MaxTokens
		} else {
			req.MaxTokens = defaultClaudeMaxTokens
		}
	}

	// Debug: log tool_use and tool_result blocks for debugging
	var toolUseCount, toolResultCount int
	var toolUseIDs, toolResultIDs []string
	for i, msg := range req.Messages {
		for _, block := range msg.Content {
			if block.Type == ContentTypeToolUse {
				toolUseCount++
				id := block.ID
				if id == "" {
					id = "(empty)"
				}
				toolUseIDs = append(toolUseIDs, fmt.Sprintf("msg%d:%s", i, id))
			} else if block.Type == ContentTypeToolResult {
				toolResultCount++
				id := block.ToolUseID
				if id == "" {
					id = "(empty)"
				}
				toolResultIDs = append(toolResultIDs, fmt.Sprintf("msg%d:%s", i, id))
			}
		}
	}
	log.Printf("[claude-provider] tool_use blocks: %d, tool_result blocks: %d", toolUseCount, toolResultCount)
	if len(toolUseIDs) > 0 && len(toolUseIDs) <= 20 {
		log.Printf("[claude-provider] tool_use IDs: %v", toolUseIDs)
	}
	if len(toolResultIDs) > 0 && len(toolResultIDs) <= 20 {
		log.Printf("[claude-provider] tool_result IDs: %v", toolResultIDs)
	}

	log.Printf("[claude-provider] calling API: model=%s max_tokens=%d messages=%d tools=%d",
		req.Model, req.MaxTokens, len(req.Messages), len(req.Tools))

	payload, err := json.Marshal(req)
	if err != nil {
		return AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	log.Printf("[claude-provider] request payload size: %d bytes", len(payload))

	maxAttempts := p.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultClaudeMaxAttempts
	}
	backoff := p.Backoff
	if backoff == nil {
		backoff = claudeDefaultBackoff
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
		log.Printf("[claude-provider] API request attempt %d/%d", attempt, maxAttempts)
		respBody, status, err := p.doRequest(ctx, client, payload)
		log.Printf("[claude-provider] API response: status=%d body_size=%d err=%v", status, len(respBody), err)

		if err == nil && status < 400 {
			resp, parseErr := parseClaudeResponse(respBody)
			if parseErr != nil {
				log.Printf("[claude-provider] ERROR: failed to parse response: %v", parseErr)
				log.Printf("[claude-provider] response body: %s", string(respBody))
				// Treat empty/unparseable response with 2xx status as retriable
				lastErr = parseErr
				if attempt < maxAttempts {
					backoffDuration := backoff(attempt)
					log.Printf("[claude-provider] retrying after parse error in %v", backoffDuration)
					sleep(backoffDuration)
					continue
				}
				return AgentResponse{}, parseErr
			}
			log.Printf("[claude-provider] parsed response: id=%s stop_reason=%s content_blocks=%d",
				resp.ID, resp.StopReason, len(resp.Content))
			return resp, nil
		}
		lastErr = wrapClaudeAPIError(respBody, status, err)
		log.Printf("[claude-provider] ERROR: attempt %d failed: %v", attempt, lastErr)
		if attempt == maxAttempts || !shouldRetryClaude(status, err) {
			log.Printf("[claude-provider] giving up after %d attempts", attempt)
			return AgentResponse{}, lastErr
		}
		backoffDuration := backoff(attempt)
		log.Printf("[claude-provider] retrying in %v", backoffDuration)
		sleep(backoffDuration)
	}
	return AgentResponse{}, lastErr
}

func (p *ClaudeProvider) doRequest(ctx context.Context, client *http.Client, payload []byte) ([]byte, int, error) {
	endpoint, err := buildClaudeEndpoint(p.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	log.Printf("[claude-provider] POST %s", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	// Claude API uses x-api-key header
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[claude-provider] HTTP request failed: %v", err)
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func buildClaudeEndpoint(baseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/")
	base.Path = base.Path + claudeAPIPath
	return base.String(), nil
}

func parseClaudeResponse(body []byte) (AgentResponse, error) {
	if len(body) == 0 {
		return AgentResponse{}, errors.New("API returned empty response body")
	}
	var resp AgentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return AgentResponse{}, fmt.Errorf("parse response: %w (body: %s)", err, truncateForLog(string(body), 500))
	}
	return resp, nil
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func wrapClaudeAPIError(body []byte, status int, err error) error {
	if err != nil {
		return err
	}
	if status == 0 {
		return errors.New("Claude API request failed")
	}

	// Try to parse error response
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("Claude API error %d: %s - %s", status, errResp.Error.Type, errResp.Error.Message)
	}

	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("Claude API error: %d %s", status, msg)
}

func shouldRetryClaude(status int, err error) bool {
	if err != nil {
		return true
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout {
		return true
	}
	// Retry on overload
	if status == 529 {
		return true
	}
	return status >= 500
}

func claudeDefaultBackoff(attempt int) time.Duration {
	base := float64(defaultClaudeBackoffSec) * float64(time.Second)
	factor := math.Pow(2, float64(attempt-1))
	jitter := 0.5 + rand.Float64()
	return time.Duration(base * factor * jitter)
}
