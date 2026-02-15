package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Decision indicates how the workflow should proceed.
type Decision string

const (
	DecisionProceed   Decision = "proceed"
	DecisionNeedsInfo Decision = "needs_info"
	DecisionStop      Decision = "stop"
)

// Request is the payload sent to the LLM tool.
type Request struct {
	Mode             string            `json:"mode"`
	RepoPath         string            `json:"repo_path"`
	RepoFullName     string            `json:"repo_full_name"`
	TaskID           string            `json:"task_id,omitempty"`
	TaskTitle        string            `json:"task_title,omitempty"`
	TaskBody         string            `json:"task_body,omitempty"`
	TaskLabels       []string          `json:"task_labels,omitempty"`
	TaskComments     []Comment         `json:"task_comments,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	IssueNumber      int               `json:"issue_number,omitempty"`
	PRNumber         int               `json:"pr_number,omitempty"`
	IssueTitle       string            `json:"issue_title,omitempty"`
	IssueBody        string            `json:"issue_body,omitempty"`
	IssueLabels      []string          `json:"issue_labels,omitempty"`
	IssueComments    []Comment         `json:"issue_comments,omitempty"`
	PRTitle          string            `json:"pr_title,omitempty"`
	PRBody           string            `json:"pr_body,omitempty"`
	PRHeadRef        string            `json:"pr_head_ref,omitempty"`
	PRBaseRef        string            `json:"pr_base_ref,omitempty"`
	CommentBody      string            `json:"comment_body,omitempty"`
	SlashCommand     string            `json:"slash_command,omitempty"`
	RepoInstructions string            `json:"repo_instructions,omitempty"`
	Requirements     string            `json:"requirements,omitempty"`
	Prompt           string            `json:"-"`
	OutputPath       string            `json:"-"`
}

// Comment represents a user comment attached to task context.
type Comment struct {
	User string `json:"user"`
	Body string `json:"body"`
}

// Response is the parsed LLM output.
type Response struct {
	Decision         Decision          `json:"decision"`
	NeedsInfoComment string            `json:"needs_info_comment,omitempty"`
	CommitMessage    string            `json:"commit_message,omitempty"`
	PRTitle          string            `json:"pr_title,omitempty"`
	PRBody           string            `json:"pr_body,omitempty"`
	Patch            string            `json:"patch,omitempty"`
	Files            map[string]string `json:"files,omitempty"` // map of file path to complete new content
	Summary          string            `json:"summary,omitempty"`
}

// RunResult captures LLM output and parsed response.
type RunResult struct {
	Response Response
	Stdout   string
	Stderr   string
}

// Runner executes an LLM tool.
type Runner interface {
	Run(ctx context.Context, req Request, workDir string) (RunResult, error)
}

// CommandRunner invokes an external command for LLM processing.
type CommandRunner struct {
	Command string
	Args    []string
	Timeout time.Duration
}

// ParseResponse parses and validates the response.
func ParseResponse(data []byte) (Response, error) {
	_, resp, err := extractResponseJSON(data)
	return resp, err
}

func parseResponseJSON(raw []byte) (Response, error) {
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, err
	}
	if resp.Decision == "" {
		return Response{}, errors.New("missing decision")
	}
	return resp, nil
}

func extractResponseJSON(data []byte) ([]byte, Response, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, Response{}, errors.New("LLM output is empty")
	}
	var lastRaw []byte
	var lastErr error
	for idx := bytes.IndexByte(trimmed, '{'); idx != -1; {
		raw, offset, resp, err := decodeResponseFrom(trimmed[idx:])
		if err == nil {
			return raw, resp, nil
		}
		if len(raw) != 0 {
			lastRaw = raw
		}
		lastErr = err
		if offset > 0 {
			nextPos := idx + offset
			if nextPos >= len(trimmed) {
				break
			}
			next := bytes.IndexByte(trimmed[nextPos:], '{')
			if next == -1 {
				break
			}
			idx = nextPos + next
			continue
		}
		next := bytes.IndexByte(trimmed[idx+1:], '{')
		if next == -1 {
			break
		}
		idx = idx + 1 + next
	}
	if lastErr != nil {
		return lastRaw, Response{}, lastErr
	}
	return nil, Response{}, errors.New("LLM output missing JSON object")
}

func decodeResponseFrom(data []byte) ([]byte, int, Response, error) {
	raw, offset, err := decodeFirstJSONObject(data)
	if err != nil {
		return nil, 0, Response{}, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return raw, offset, Response{}, errors.New("LLM output is not a JSON object")
	}
	resp, err := parseResponseJSON(raw)
	if err != nil {
		return raw, offset, Response{}, err
	}
	return raw, offset, resp, nil
}

func decodeFirstJSONObject(data []byte) (json.RawMessage, int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, 0, err
	}
	return raw, int(decoder.InputOffset()), nil
}

// Run executes the LLM command with JSON input on stdin.
func (r CommandRunner) Run(ctx context.Context, req Request, workDir string) (RunResult, error) {
	if r.Command == "" {
		return RunResult{}, errors.New("LLM command is empty")
	}
	input := strings.TrimSpace(req.Prompt)
	if input == "" {
		payload, err := json.Marshal(req)
		if err != nil {
			return RunResult{}, err
		}
		input = string(payload)
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.Command, r.Args...)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return RunResult{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("llm command failed: %w", err)
	}
	output := stdout.String()
	if strings.TrimSpace(req.OutputPath) != "" {
		if data, err := os.ReadFile(req.OutputPath); err == nil {
			if strings.TrimSpace(string(data)) != "" {
				output = string(data)
			}
		}
	}
	raw, resp, err := extractResponseJSON([]byte(output))
	if err != nil {
		return RunResult{Response: resp, Stdout: string(raw), Stderr: stderr.String()}, err
	}
	return RunResult{Response: resp, Stdout: string(raw), Stderr: stderr.String()}, nil
}
