package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/skills"
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
	prompt := buildSystemPrompt("", "", "## AGENT.md\nrules")
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

	t.Setenv(skills.SkillDirsEnv, skillsDir)
	got := readRepoInstructions(repo, nil)
	if !strings.Contains(got, "Available Skills") {
		t.Fatalf("expected Available Skills block in instructions, got: %q", got)
	}
	if !strings.Contains(got, "test-skill") {
		t.Fatalf("expected discovered skill metadata, got: %q", got)
	}
}

func TestBuildSystemPromptIncludesSoulBeforeRepoInstructions(t *testing.T) {
	prompt := buildSystemPrompt("base", "Be a pirate.", "## AGENT.md\nrules")
	if !strings.Contains(prompt, "## Soul") {
		t.Fatalf("expected Soul section in prompt, got: %q", prompt)
	}
	if !strings.Contains(prompt, "Be a pirate.") {
		t.Fatalf("expected soul content in prompt, got: %q", prompt)
	}
	soulPos := strings.Index(prompt, "## Soul")
	repoPos := strings.Index(prompt, "## Repository Instructions")
	if soulPos >= repoPos {
		t.Fatalf("expected Soul before Repository Instructions, soul=%d repo=%d", soulPos, repoPos)
	}
}

func TestBuildSystemPromptNoSoul(t *testing.T) {
	prompt := buildSystemPrompt("base", "", "repo stuff")
	if strings.Contains(prompt, "Soul") {
		t.Fatalf("expected no Soul section when content is empty, got: %q", prompt)
	}
}

func TestBuildSystemPromptEmptyWhenNoInputs(t *testing.T) {
	prompt := buildSystemPrompt("", "", "")
	if strings.TrimSpace(prompt) != "" {
		t.Fatalf("expected empty system prompt when no inputs are provided, got: %q", prompt)
	}
}

func TestReadSoulContentFromWorkDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteText(t, filepath.Join(dir, "SOUL.md"), "You are helpful.")
	content := readSoulContent(dir, "")
	if content != "You are helpful." {
		t.Fatalf("expected soul content, got: %q", content)
	}
}

func TestReadSoulContentExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	mustWriteText(t, path, "Custom soul.")
	content := readSoulContent("", path)
	if content != "Custom soul." {
		t.Fatalf("expected custom soul content, got: %q", content)
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
