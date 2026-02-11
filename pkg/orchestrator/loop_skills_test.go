package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
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
