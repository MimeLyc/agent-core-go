package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

const githubAPIBase = "https://api.github.com"

// GitHubGetIssueTool retrieves issue details.
type GitHubGetIssueTool struct{}

func (t GitHubGetIssueTool) Name() string {
	return "github_get_issue"
}

func (t GitHubGetIssueTool) Description() string {
	return "Get details of a GitHub issue including title, body, labels, and state."
}

func (t GitHubGetIssueTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner": map[string]any{
				"type":        "string",
				"description": "Repository owner (defaults to current repo owner)",
			},
			"repo": map[string]any{
				"type":        "string",
				"description": "Repository name (defaults to current repo name)",
			},
			"number": map[string]any{
				"type":        "integer",
				"description": "Issue number",
			},
		},
		"required": []string{"number"},
	}
}

func (t GitHubGetIssueTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGitHub(); err != nil {
		return tools.NewErrorResult(err), nil
	}
	if toolCtx.GitHubToken == "" {
		return tools.NewErrorResultf("GitHub token not configured"), nil
	}

	owner := toolCtx.RepoOwner
	if o, ok := input["owner"].(string); ok && o != "" {
		owner = o
	}
	repo := toolCtx.RepoName
	if r, ok := input["repo"].(string); ok && r != "" {
		repo = r
	}
	if owner == "" || repo == "" {
		return tools.NewErrorResultf("owner and repo are required"), nil
	}

	number, ok := input["number"].(float64)
	if !ok || number <= 0 {
		return tools.NewErrorResultf("number is required"), nil
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", githubAPIBase, owner, repo, int(number))
	body, err := githubRequest(ctx, "GET", url, toolCtx.GitHubToken, nil)
	if err != nil {
		return tools.NewErrorResultf("failed to get issue: %v", err), nil
	}

	// Parse and format response
	var issue map[string]any
	if err := json.Unmarshal(body, &issue); err != nil {
		return tools.NewErrorResultf("failed to parse response: %v", err), nil
	}

	// Format output
	result := formatIssue(issue)
	return tools.NewToolResult(result), nil
}

// GitHubCreateCommentTool creates a comment on an issue or PR.
type GitHubCreateCommentTool struct{}

func (t GitHubCreateCommentTool) Name() string {
	return "github_create_comment"
}

func (t GitHubCreateCommentTool) Description() string {
	return "Create a comment on a GitHub issue or pull request."
}

func (t GitHubCreateCommentTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner": map[string]any{
				"type":        "string",
				"description": "Repository owner (defaults to current repo owner)",
			},
			"repo": map[string]any{
				"type":        "string",
				"description": "Repository name (defaults to current repo name)",
			},
			"number": map[string]any{
				"type":        "integer",
				"description": "Issue or PR number",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Comment body (supports markdown)",
			},
		},
		"required": []string{"number", "body"},
	}
}

func (t GitHubCreateCommentTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGitHub(); err != nil {
		return tools.NewErrorResult(err), nil
	}
	if toolCtx.GitHubToken == "" {
		return tools.NewErrorResultf("GitHub token not configured"), nil
	}

	owner := toolCtx.RepoOwner
	if o, ok := input["owner"].(string); ok && o != "" {
		owner = o
	}
	repo := toolCtx.RepoName
	if r, ok := input["repo"].(string); ok && r != "" {
		repo = r
	}
	if owner == "" || repo == "" {
		return tools.NewErrorResultf("owner and repo are required"), nil
	}

	number, ok := input["number"].(float64)
	if !ok || number <= 0 {
		return tools.NewErrorResultf("number is required"), nil
	}

	body, ok := input["body"].(string)
	if !ok || body == "" {
		return tools.NewErrorResultf("body is required"), nil
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", githubAPIBase, owner, repo, int(number))
	payload := map[string]string{"body": body}
	payloadBytes, _ := json.Marshal(payload)

	respBody, err := githubRequest(ctx, "POST", url, toolCtx.GitHubToken, payloadBytes)
	if err != nil {
		return tools.NewErrorResultf("failed to create comment: %v", err), nil
	}

	var comment map[string]any
	if err := json.Unmarshal(respBody, &comment); err != nil {
		return tools.NewErrorResultf("failed to parse response: %v", err), nil
	}

	htmlURL := ""
	if u, ok := comment["html_url"].(string); ok {
		htmlURL = u
	}

	return tools.NewToolResult(fmt.Sprintf("Comment created: %s", htmlURL)), nil
}

// GitHubListIssuesTool lists issues in a repository.
type GitHubListIssuesTool struct{}

func (t GitHubListIssuesTool) Name() string {
	return "github_list_issues"
}

func (t GitHubListIssuesTool) Description() string {
	return "List issues in a GitHub repository. Can filter by state and labels."
}

func (t GitHubListIssuesTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"owner": map[string]any{
				"type":        "string",
				"description": "Repository owner (defaults to current repo owner)",
			},
			"repo": map[string]any{
				"type":        "string",
				"description": "Repository name (defaults to current repo name)",
			},
			"state": map[string]any{
				"type":        "string",
				"enum":        []string{"open", "closed", "all"},
				"description": "Filter by state (default: open)",
			},
			"labels": map[string]any{
				"type":        "string",
				"description": "Comma-separated list of labels to filter by",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of issues to return (default: 10, max: 30)",
			},
		},
	}
}

func (t GitHubListIssuesTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckGitHub(); err != nil {
		return tools.NewErrorResult(err), nil
	}
	if toolCtx.GitHubToken == "" {
		return tools.NewErrorResultf("GitHub token not configured"), nil
	}

	owner := toolCtx.RepoOwner
	if o, ok := input["owner"].(string); ok && o != "" {
		owner = o
	}
	repo := toolCtx.RepoName
	if r, ok := input["repo"].(string); ok && r != "" {
		repo = r
	}
	if owner == "" || repo == "" {
		return tools.NewErrorResultf("owner and repo are required"), nil
	}

	state := "open"
	if s, ok := input["state"].(string); ok && s != "" {
		state = s
	}

	limit := 10
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 30 {
			limit = 30
		}
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues?state=%s&per_page=%d", githubAPIBase, owner, repo, state, limit)
	if labels, ok := input["labels"].(string); ok && labels != "" {
		url += "&labels=" + labels
	}

	body, err := githubRequest(ctx, "GET", url, toolCtx.GitHubToken, nil)
	if err != nil {
		return tools.NewErrorResultf("failed to list issues: %v", err), nil
	}

	var issues []map[string]any
	if err := json.Unmarshal(body, &issues); err != nil {
		return tools.NewErrorResultf("failed to parse response: %v", err), nil
	}

	var result strings.Builder
	for _, issue := range issues {
		// Skip pull requests (they appear in issues API)
		if _, isPR := issue["pull_request"]; isPR {
			continue
		}

		number := 0
		if n, ok := issue["number"].(float64); ok {
			number = int(n)
		}
		title := ""
		if t, ok := issue["title"].(string); ok {
			title = t
		}
		result.WriteString(fmt.Sprintf("#%d: %s\n", number, title))
	}

	output := result.String()
	if output == "" {
		output = "No issues found"
	}
	return tools.NewToolResult(output), nil
}

// githubRequest makes an authenticated request to the GitHub API.
func githubRequest(ctx context.Context, method, url, token string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// formatIssue formats an issue for display.
func formatIssue(issue map[string]any) string {
	var result strings.Builder

	if number, ok := issue["number"].(float64); ok {
		result.WriteString(fmt.Sprintf("Issue #%d\n", int(number)))
	}
	if title, ok := issue["title"].(string); ok {
		result.WriteString(fmt.Sprintf("Title: %s\n", title))
	}
	if state, ok := issue["state"].(string); ok {
		result.WriteString(fmt.Sprintf("State: %s\n", state))
	}
	if user, ok := issue["user"].(map[string]any); ok {
		if login, ok := user["login"].(string); ok {
			result.WriteString(fmt.Sprintf("Author: @%s\n", login))
		}
	}
	if labels, ok := issue["labels"].([]any); ok && len(labels) > 0 {
		labelNames := make([]string, 0, len(labels))
		for _, l := range labels {
			if label, ok := l.(map[string]any); ok {
				if name, ok := label["name"].(string); ok {
					labelNames = append(labelNames, name)
				}
			}
		}
		if len(labelNames) > 0 {
			result.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(labelNames, ", ")))
		}
	}
	if body, ok := issue["body"].(string); ok && body != "" {
		result.WriteString(fmt.Sprintf("\nBody:\n%s\n", body))
	}

	return result.String()
}

// RegisterGitHubTools registers all GitHub tools with the registry.
func RegisterGitHubTools(registry *tools.Registry) {
	registry.MustRegister(GitHubGetIssueTool{})
	registry.MustRegister(GitHubCreateCommentTool{})
	registry.MustRegister(GitHubListIssuesTool{})
}
