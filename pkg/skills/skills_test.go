package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillEnvConstantsUseNonCodexPrefix(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "SkillDirsEnv",
			value: SkillDirsEnv,
			want:  "SKILL_DIRS",
		},
		{
			name:  "SystemSkillDirsEnv",
			value: SystemSkillDirsEnv,
			want:  "SYSTEM_SKILL_DIRS",
		},
		{
			name:  "EnvActiveSkillName",
			value: EnvActiveSkillName,
			want:  "ACTIVE_SKILL_NAME",
		},
		{
			name:  "EnvActiveSkillPath",
			value: EnvActiveSkillPath,
			want:  "ACTIVE_SKILL_PATH",
		},
		{
			name:  "EnvActiveSkillAllowedTools",
			value: EnvActiveSkillAllowedTools,
			want:  "ACTIVE_SKILL_ALLOWED_TOOLS",
		},
	}

	for _, tc := range tests {
		if tc.value != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, tc.value, tc.want)
		}
	}
}

func TestDiscoverParsesMetadataFromSKILLFiles(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha", "SKILL.md")
	beta := filepath.Join(root, "beta", "SKILL.md")

	mustWrite(t, alpha, `---
name: alpha-skill
description: Handles alpha workflows.
---

# Alpha
`)
	mustWrite(t, beta, `# Beta Skill

Use this for beta tasks.`)

	skills, err := Discover([]string{root})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	if skills[0].Name != "alpha-skill" {
		t.Fatalf("first skill name = %q, want alpha-skill", skills[0].Name)
	}
	if skills[0].Description != "Handles alpha workflows." {
		t.Fatalf("first skill description = %q", skills[0].Description)
	}

	if skills[1].Name != "beta" {
		t.Fatalf("second skill name = %q, want beta", skills[1].Name)
	}
	if skills[1].Description == "" {
		t.Fatalf("second skill description should not be empty")
	}
}

func TestBuildPromptBlockUsesProgressiveDisclosure(t *testing.T) {
	block := BuildPromptBlock([]Skill{
		{
			Name:        "alpha",
			Description: "Handles alpha tasks",
			Path:        "/tmp/alpha/SKILL.md",
		},
	}, 4096)

	if !strings.Contains(block.Content, "progressive disclosure") {
		t.Fatalf("expected progressive disclosure guidance, got: %q", block.Content)
	}
	if !strings.Contains(block.Content, "alpha") {
		t.Fatalf("expected skill metadata in block, got: %q", block.Content)
	}
	if block.SkillCount != 1 {
		t.Fatalf("expected skill count 1, got %d", block.SkillCount)
	}
}

func TestBuildPromptBlockHonorsMaxBytes(t *testing.T) {
	block := BuildPromptBlock([]Skill{
		{
			Name:        "big",
			Description: strings.Repeat("x", 500),
			Path:        "/tmp/big/SKILL.md",
		},
	}, 120)

	if !block.Truncated {
		t.Fatalf("expected truncated prompt block")
	}
	if len(block.Content) > 120 {
		t.Fatalf("expected content <= 120 bytes, got %d", len(block.Content))
	}
}

func TestBuildPromptBlockSkipsDisableModelInvocationSkills(t *testing.T) {
	block := BuildPromptBlock([]Skill{
		{
			Name:                   "auto-visible",
			Description:            "visible to model",
			Path:                   "/tmp/auto-visible/SKILL.md",
			DisableModelInvocation: false,
		},
		{
			Name:                   "model-hidden",
			Description:            "hidden from model",
			Path:                   "/tmp/model-hidden/SKILL.md",
			DisableModelInvocation: true,
		},
	}, 4096)

	if !strings.Contains(block.Content, "auto-visible") {
		t.Fatalf("expected visible skill in prompt block, got: %q", block.Content)
	}
	if strings.Contains(block.Content, "model-hidden") {
		t.Fatalf("expected disable-model-invocation skill to be excluded, got: %q", block.Content)
	}
}

func TestDefaultSearchDirsIncludesExtraSystemSkillDirs(t *testing.T) {
	extraSystemDir := filepath.Join(t.TempDir(), "managed-system-skills")
	t.Setenv(SystemSkillDirsEnv, extraSystemDir)

	dirs := DefaultSearchDirs(t.TempDir())
	found := false
	for _, dir := range dirs {
		if filepath.Clean(dir) == filepath.Clean(extraSystemDir) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected extra system skills dir %q in search dirs: %v", extraSystemDir, dirs)
	}
}

func TestResolveForInvocationPrefersProjectScope(t *testing.T) {
	resolved, err := ResolveForInvocation([]Skill{
		{
			Name:  "deploy",
			Path:  "/tmp/system/deploy/SKILL.md",
			Scope: ScopeSystem,
		},
		{
			Name:  "deploy",
			Path:  "/tmp/personal/deploy/SKILL.md",
			Scope: ScopePersonal,
		},
		{
			Name:  "deploy",
			Path:  "/tmp/project/deploy/SKILL.md",
			Scope: ScopeProject,
		},
	}, "deploy")
	if err != nil {
		t.Fatalf("ResolveForInvocation() error = %v", err)
	}
	if resolved.Scope != ScopeProject {
		t.Fatalf("expected project scope, got %s", resolved.Scope)
	}
}

func TestRenderForInvocationReplacesArgumentsAndSessionID(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "deploy", "SKILL.md")
	mustWrite(t, skillPath, `---
name: deploy
description: Deploy helper
---
Session=${CLAUDE_SESSION_ID}
Target=$ARGUMENTS`)

	content, truncated, err := RenderForInvocation(Skill{
		Name: "deploy",
		Path: skillPath,
	}, "staging", "sess-123", 4096)
	if err != nil {
		t.Fatalf("RenderForInvocation() error = %v", err)
	}
	if truncated {
		t.Fatalf("expected non-truncated render")
	}
	if !strings.Contains(content, "Session=sess-123") {
		t.Fatalf("expected session id replacement, got: %q", content)
	}
	if !strings.Contains(content, "Target=staging") {
		t.Fatalf("expected argument replacement, got: %q", content)
	}
}

func TestRenderForInvocationAppendsArgumentsWhenPlaceholderMissing(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "lint", "SKILL.md")
	mustWrite(t, skillPath, `---
name: lint
description: lint helper
---
Run lint workflow.`)

	content, _, err := RenderForInvocation(Skill{
		Name: "lint",
		Path: skillPath,
	}, "--fix", "", 4096)
	if err != nil {
		t.Fatalf("RenderForInvocation() error = %v", err)
	}
	if !strings.Contains(content, "ARGUMENTS:") || !strings.Contains(content, "--fix") {
		t.Fatalf("expected appended ARGUMENTS section, got: %q", content)
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
