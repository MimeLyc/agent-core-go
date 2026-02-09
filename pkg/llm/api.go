package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAPIPath        = "/v1/chat/completions"
	defaultMaxAttempts    = 5
	defaultBackoffBaseSec = 10
)

// APIRunner invokes a chat-completions compatible API.
type APIRunner struct {
	BaseURL      string
	APIKey       string
	Model        string
	Path         string
	APIKeyHeader string
	APIKeyPrefix string
	Timeout      time.Duration
	MaxAttempts  int
	HTTPClient   *http.Client
	Backoff      func(int) time.Duration
	Sleep        func(time.Duration)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Run executes the API call and parses the response.
func (r APIRunner) Run(ctx context.Context, req Request, workDir string) (RunResult, error) {
	if strings.TrimSpace(r.BaseURL) == "" {
		return RunResult{}, errors.New("LLM API base URL is empty")
	}
	if strings.TrimSpace(r.APIKey) == "" {
		return RunResult{}, errors.New("LLM API key is empty")
	}
	if strings.TrimSpace(r.Model) == "" {
		return RunResult{}, errors.New("LLM API model is empty")
	}
	path := r.Path
	if strings.TrimSpace(path) == "" {
		path = defaultAPIPath
	}
	messages, err := buildMessages(req)
	if err != nil {
		return RunResult{}, err
	}
	payload, err := json.Marshal(chatRequest{Model: r.Model, Messages: messages})
	if err != nil {
		return RunResult{}, err
	}

	maxAttempts := r.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultMaxAttempts
	}
	backoff := r.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}
	sleep := r.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: r.Timeout}
	}

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		respBody, status, err := r.doRequest(ctx, client, path, payload)
		if err == nil && status < 400 {
			return parseAPIResponse(respBody)
		}
		lastErr = wrapAPIError(respBody, status, err)
		if attempt == maxAttempts || !shouldRetry(status, err) {
			return RunResult{}, lastErr
		}
		sleep(backoff(attempt))
	}
	return RunResult{}, lastErr
}

func (r APIRunner) doRequest(ctx context.Context, client *http.Client, path string, payload []byte) ([]byte, int, error) {
	endpoint, err := buildEndpoint(r.BaseURL, path)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	keyHeader := r.APIKeyHeader
	if keyHeader == "" {
		keyHeader = "Authorization"
	}
	keyPrefix := r.APIKeyPrefix
	if keyPrefix == "" {
		keyPrefix = "Bearer"
	}
	keyValue := r.APIKey
	if keyPrefix != "" {
		keyValue = keyPrefix + " " + r.APIKey
	}
	req.Header.Set(keyHeader, keyValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func parseAPIResponse(body []byte) (RunResult, error) {
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return RunResult{}, err
	}
	if len(parsed.Choices) == 0 {
		return RunResult{}, errors.New("LLM API response missing choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return RunResult{}, errors.New("LLM API response content is empty")
	}
	_, resp, err := extractResponseJSON([]byte(content))
	if err != nil {
		return RunResult{Response: resp, Stdout: string(body)}, err
	}
	return RunResult{Response: resp, Stdout: string(body)}, nil
}

func buildEndpoint(baseURL, path string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/")
	cleanPath := "/" + strings.TrimLeft(path, "/")
	base.Path = base.Path + cleanPath
	return base.String(), nil
}

func wrapAPIError(body []byte, status int, err error) error {
	if err != nil {
		return err
	}
	if status == 0 {
		return errors.New("LLM API request failed")
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("LLM API error: %d %s", status, msg)
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		return true
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout {
		return true
	}
	return status >= 500
}

func defaultBackoff(attempt int) time.Duration {
	base := float64(defaultBackoffBaseSec) * float64(time.Second)
	factor := math.Pow(2, float64(attempt-1))
	jitter := 0.5 + rand.Float64()
	return time.Duration(base * factor * jitter)
}

func buildMessages(req Request) ([]chatMessage, error) {
	if strings.TrimSpace(req.Prompt) != "" {
		return []chatMessage{{Role: "user", Content: req.Prompt}}, nil
	}
	payload, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return nil, err
	}
	system := "You are an autonomous engineering agent. Reply with a single JSON object matching this schema: " +
		"{decision, needs_info_comment, commit_message, pr_title, pr_body, files, summary}. " +
		"The decision field MUST be one of: proceed, needs_info, stop. " +
		"Use the 'files' field (a JSON object mapping file paths to complete new content) for code changes. " +
		"Output only JSON, no markdown."
	user := "Input:\n" + string(payload)
	return []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}, nil
}
