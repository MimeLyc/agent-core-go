package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRepoInstructionsAggregatesRootToLeafAndPrefersAgent(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	leaf := filepath.Join(repo, "services", "api")
	mustMkdirAll(t, leaf)

	mustWriteText(t, filepath.Join(repo, "AGENT.md"), "root agent rules")
	mustWriteText(t, filepath.Join(repo, "CLAUDE.md"), "root claude rules")
	mustWriteText(t, filepath.Join(repo, "services", "AGENT.md"), "services rules")
	mustWriteText(t, filepath.Join(leaf, "AGENT.md"), "api rules")

	got := readRepoInstructions(leaf, nil)
	if strings.Contains(got, "root claude rules") {
		t.Fatalf("expected AGENT.md to win over CLAUDE.md in same directory, got: %q", got)
	}
	for _, want := range []string{
		"root agent rules",
		"services rules",
		"api rules",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected merged instructions to contain %q, got: %q", want, got)
		}
	}
	rootPos := strings.Index(got, "root agent rules")
	servicesPos := strings.Index(got, "services rules")
	apiPos := strings.Index(got, "api rules")
	if !(rootPos >= 0 && servicesPos > rootPos && apiPos > servicesPos) {
		t.Fatalf("expected root->leaf order, got: %q", got)
	}
}

func TestBuildSystemPromptIncludesLayerPrecedenceHint(t *testing.T) {
	prompt := buildSystemPrompt("", "## AGENT.md\nrules")
	if !strings.Contains(prompt, "More specific instructions should override broader ones.") {
		t.Fatalf("expected precedence guidance in system prompt, got: %q", prompt)
	}
}

func TestReadRepoInstructionsIncludesSkillMetadataBlock(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	mustWriteText(t, filepath.Join(repo, "AGENT.md"), "repo rules")

	skillsDir := filepath.Join(t.TempDir(), "skills")
	mustMkdirAll(t, filepath.Join(skillsDir, "test-skill"))
	mustWriteText(t, filepath.Join(skillsDir, "test-skill", "SKILL.md"), `---
name: test-skill
description: test description
---
`)

	t.Setenv("CODEX_SKILL_DIRS", skillsDir)
	got := readRepoInstructions(repo, nil)
	if !strings.Contains(got, "Available Skills") {
		t.Fatalf("expected Available Skills block in instructions, got: %q", got)
	}
	if !strings.Contains(got, "test-skill") {
		t.Fatalf("expected discovered skill metadata, got: %q", got)
	}
}

func mustWriteText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
