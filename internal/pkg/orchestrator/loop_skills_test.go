package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/skills"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

func TestApplySlashSkillInvocationLoadsSkillIntoInitialMessage(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	mustMkdirAll(t, filepath.Join(skillsDir, "deploy"))
	mustWriteText(t, filepath.Join(skillsDir, "deploy", "SKILL.md"), `---
name: deploy
description: deploy helper
---
Deploy target: $ARGUMENTS`)

	t.Setenv(skills.SkillDirsEnv, skillsDir)

	state := NewState([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "/deploy staging"),
	})
	toolCtx := tools.NewToolContext(root)
	applied, err := applySlashSkillInvocation(state, toolCtx, root)
	if err != nil {
		t.Fatalf("applySlashSkillInvocation() error = %v", err)
	}
	if !applied {
		t.Fatalf("expected slash skill invocation to be applied")
	}

	got := state.Messages[0].GetText()
	if !strings.Contains(got, "Deploy target: staging") {
		t.Fatalf("expected rendered skill instructions, got: %q", got)
	}
	if toolCtx.Env[skills.EnvActiveSkillName] != "deploy" {
		t.Fatalf("expected active skill to be set, got: %q", toolCtx.Env[skills.EnvActiveSkillName])
	}
}

func TestApplySlashSkillInvocationIgnoresUnknownCommand(t *testing.T) {
	root := t.TempDir()
	state := NewState([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "/unknown-skill do something"),
	})
	toolCtx := tools.NewToolContext(root)

	applied, err := applySlashSkillInvocation(state, toolCtx, root)
	if err != nil {
		t.Fatalf("applySlashSkillInvocation() error = %v", err)
	}
	if applied {
		t.Fatalf("expected unknown slash command to be ignored")
	}
}

func TestEnsureToolAllowedByActiveSkill(t *testing.T) {
	toolCtx := tools.NewToolContext(t.TempDir())
	toolCtx.WithEnv(skills.EnvActiveSkillName, "deploy")
	toolCtx.WithEnv(skills.EnvActiveSkillAllowedTools, "Bash\nRead")

	if err := ensureToolAllowedByActiveSkill(toolCtx, "bash"); err != nil {
		t.Fatalf("expected bash to be allowed, got error: %v", err)
	}
	if err := ensureToolAllowedByActiveSkill(toolCtx, "write_file"); err == nil {
		t.Fatalf("expected write_file to be blocked by active skill allowlist")
	}
}

func TestSummarizeSkillDiscoveryByDirGroupsAndSortsSkills(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, ".agents", "skills")
	personalDir := filepath.Join(root, ".codex", "skills")

	discovered := []skills.Skill{
		{
			Name: "zeta",
			Path: filepath.Join(projectDir, "zeta", "SKILL.md"),
		},
		{
			Name: "alpha",
			Path: filepath.Join(projectDir, "alpha", "SKILL.md"),
		},
		{
			Name: "beta",
			Path: filepath.Join(personalDir, "beta", "SKILL.md"),
		},
	}

	entries := summarizeSkillDiscoveryByDir([]string{projectDir, personalDir}, discovered)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Dir != filepath.Clean(projectDir) {
		t.Fatalf("first entry dir = %q, want %q", entries[0].Dir, filepath.Clean(projectDir))
	}
	if len(entries[0].Skills) != 2 {
		t.Fatalf("first entry count = %d, want 2", len(entries[0].Skills))
	}
	if entries[0].Skills[0].Name != "alpha" || entries[0].Skills[1].Name != "zeta" {
		t.Fatalf("expected sorted skill names [alpha zeta], got [%s %s]", entries[0].Skills[0].Name, entries[0].Skills[1].Name)
	}

	if entries[1].Dir != filepath.Clean(personalDir) {
		t.Fatalf("second entry dir = %q, want %q", entries[1].Dir, filepath.Clean(personalDir))
	}
	if len(entries[1].Skills) != 1 || entries[1].Skills[0].Name != "beta" {
		t.Fatalf("unexpected second entry skills: %+v", entries[1].Skills)
	}
}

func TestSummarizeSkillDiscoveryByDirIncludesUnmatchedAndZeroCount(t *testing.T) {
	root := t.TempDir()
	dirWithSkill := filepath.Join(root, "with-skill")
	emptyDir := filepath.Join(root, "empty")
	externalPath := filepath.Join(t.TempDir(), "external", "SKILL.md")

	discovered := []skills.Skill{
		{
			Name: "inside",
			Path: filepath.Join(dirWithSkill, "inside", "SKILL.md"),
		},
		{
			Name: "outside",
			Path: externalPath,
		},
	}

	entries := summarizeSkillDiscoveryByDir([]string{dirWithSkill, emptyDir}, discovered)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (2 dirs + unmatched), got %d", len(entries))
	}

	if entries[0].Dir != filepath.Clean(dirWithSkill) || len(entries[0].Skills) != 1 {
		t.Fatalf("unexpected first entry: dir=%q count=%d", entries[0].Dir, len(entries[0].Skills))
	}
	if entries[1].Dir != filepath.Clean(emptyDir) || len(entries[1].Skills) != 0 {
		t.Fatalf("unexpected second entry: dir=%q count=%d", entries[1].Dir, len(entries[1].Skills))
	}
	if entries[2].Dir != unmatchedSkillDirLabel || len(entries[2].Skills) != 1 || entries[2].Skills[0].Name != "outside" {
		t.Fatalf("unexpected unmatched entry: %+v", entries[2])
	}
}
