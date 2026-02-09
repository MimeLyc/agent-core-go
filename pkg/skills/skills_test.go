package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
