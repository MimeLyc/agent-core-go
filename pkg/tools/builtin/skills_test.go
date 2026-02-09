package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

func TestListSkillsToolReturnsDiscoveredSkills(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agents", "skills")
	mustWrite(t, filepath.Join(skillsDir, "alpha", "SKILL.md"), `---
name: alpha
description: Alpha workflow
---
`)

	tool := ListSkillsTool{}
	toolCtx := tools.NewToolContext(root)
	result, err := tool.Execute(context.Background(), toolCtx, map[string]any{
		"search_paths": []any{skillsDir},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "alpha") {
		t.Fatalf("expected skill name in output, got: %q", result.Content)
	}
}

func TestReadSkillToolReadsSkillByName(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agents", "skills")
	mustWrite(t, filepath.Join(skillsDir, "beta", "SKILL.md"), `---
name: beta
description: Beta workflow
---

# Beta
content`)

	tool := ReadSkillTool{}
	toolCtx := tools.NewToolContext(root)
	result, err := tool.Execute(context.Background(), toolCtx, map[string]any{
		"name":         "beta",
		"search_paths": []any{skillsDir},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "# Beta") {
		t.Fatalf("expected skill body in output, got: %q", result.Content)
	}
}

func TestReadSkillToolFailsOnAmbiguousName(t *testing.T) {
	root := t.TempDir()
	dir1 := filepath.Join(root, "skills-1")
	dir2 := filepath.Join(root, "skills-2")
	mustWrite(t, filepath.Join(dir1, "dup-a", "SKILL.md"), `---
name: dup
description: first
---
`)
	mustWrite(t, filepath.Join(dir2, "dup-b", "SKILL.md"), `---
name: dup
description: second
---
`)

	tool := ReadSkillTool{}
	toolCtx := tools.NewToolContext(root)
	result, err := tool.Execute(context.Background(), toolCtx, map[string]any{
		"name":         "dup",
		"search_paths": []any{dir1, dir2},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error for ambiguous skill name")
	}
	if !strings.Contains(result.Content, "ambiguous") {
		t.Fatalf("expected ambiguous error, got: %q", result.Content)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
