package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MimeLyc/agent-core-go/pkg/skills"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

const (
	defaultListSkillsLimit = 100
	maxListSkillsLimit     = 500
)

// ListSkillsTool lists available SKILL.md metadata for progressive disclosure.
type ListSkillsTool struct{}

func (t ListSkillsTool) Name() string {
	return "list_skills"
}

func (t ListSkillsTool) Description() string {
	return "List discoverable skills (name, description, path) from configured skill directories."
}

func (t ListSkillsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Optional case-insensitive filter applied to skill names and descriptions",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of skills to return (default: 100, max: 500)",
			},
			"search_paths": map[string]any{
				"type":        "array",
				"description": "Optional explicit directories to scan for skills",
				"items":       map[string]any{"type": "string"},
			},
		},
	}
}

func (t ListSkillsTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckFileRead(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	searchPaths := parseSearchPaths(input["search_paths"])
	if len(searchPaths) == 0 {
		searchPaths = skills.DefaultSearchDirs(toolCtx.WorkDir)
	}

	discovered, err := skills.Discover(searchPaths)
	if err != nil {
		return tools.NewErrorResultf("failed to discover skills: %v", err), nil
	}

	query, _ := input["query"].(string)
	filtered := skills.FilterByQuery(discovered, query)

	limit := getInt(input["limit"], defaultListSkillsLimit)
	if limit <= 0 {
		limit = defaultListSkillsLimit
	}
	if limit > maxListSkillsLimit {
		limit = maxListSkillsLimit
	}

	if len(filtered) == 0 {
		return tools.NewToolResult("No skills found."), nil
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d skill(s):\n", len(filtered))
	for _, skill := range filtered {
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "No description."
		}
		fmt.Fprintf(&b, "- %s | %s | %s\n", skill.Name, desc, filepath.ToSlash(skill.Path))
	}
	return tools.NewToolResult(strings.TrimSpace(b.String())), nil
}

// ReadSkillTool reads full SKILL.md content for a selected skill.
type ReadSkillTool struct{}

func (t ReadSkillTool) Name() string {
	return "read_skill"
}

func (t ReadSkillTool) Description() string {
	return "Read the full SKILL.md content by skill name or path."
}

func (t ReadSkillTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name from list_skills output",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Optional explicit path to SKILL.md",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Maximum bytes of SKILL.md content to return (default: 65536)",
			},
			"search_paths": map[string]any{
				"type":        "array",
				"description": "Optional explicit directories to scan for skills",
				"items":       map[string]any{"type": "string"},
			},
		},
	}
}

func (t ReadSkillTool) Execute(ctx context.Context, toolCtx *tools.ToolContext, input map[string]any) (tools.ToolResult, error) {
	if err := toolCtx.CheckFileRead(); err != nil {
		return tools.NewErrorResult(err), nil
	}

	searchPaths := parseSearchPaths(input["search_paths"])
	if len(searchPaths) == 0 {
		searchPaths = skills.DefaultSearchDirs(toolCtx.WorkDir)
	}

	discovered, err := skills.Discover(searchPaths)
	if err != nil {
		return tools.NewErrorResultf("failed to discover skills: %v", err), nil
	}
	if len(discovered) == 0 {
		return tools.NewErrorResultf("no skills available"), nil
	}

	maxBytes := getInt(input["max_bytes"], skills.DefaultSkillReadMaxBytes)
	if maxBytes <= 0 {
		maxBytes = skills.DefaultSkillReadMaxBytes
	}

	var matches []skills.Skill
	if rawPath, ok := input["path"].(string); ok && strings.TrimSpace(rawPath) != "" {
		resolved := rawPath
		if !filepath.IsAbs(resolved) && toolCtx.WorkDir != "" {
			resolved = filepath.Join(toolCtx.WorkDir, resolved)
		}
		matches = skills.ResolveByPath(discovered, resolved)
	} else {
		name, _ := input["name"].(string)
		matches = skills.ResolveByName(discovered, name)
	}

	if len(matches) == 0 {
		return tools.NewErrorResultf("skill not found"), nil
	}
	if len(matches) > 1 {
		return tools.NewErrorResultf("ambiguous skill reference, candidates: %s", skills.JoinAmbiguousPaths(matches)), nil
	}

	selected := matches[0]
	content, truncated, err := skills.ReadFile(selected.Path, maxBytes)
	if err != nil {
		return tools.NewErrorResultf("failed to read skill file: %v", err), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Skill: %s\nPath: %s\n\n", selected.Name, filepath.ToSlash(selected.Path))
	b.WriteString(content)
	if truncated {
		fmt.Fprintf(&b, "\n\n[truncated to %d bytes]", maxBytes)
	}
	return tools.NewToolResult(b.String()), nil
}

// RegisterSkillTools registers skill discovery/read tools.
func RegisterSkillTools(registry *tools.Registry) {
	registry.MustRegister(ListSkillsTool{})
	registry.MustRegister(ReadSkillTool{})
}

func parseSearchPaths(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func getInt(v any, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return def
		}
		return parsed
	default:
		return def
	}
}
