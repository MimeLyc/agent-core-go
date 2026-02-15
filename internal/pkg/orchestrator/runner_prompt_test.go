package orchestrator

import (
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
)

func TestBuildPromptFromRequestUsesPromptAsUserInput(t *testing.T) {
	req := llm.Request{
		Prompt: "User input text",
	}
	prompt := buildPromptFromRequest(req)
	if prompt != "User input text" {
		t.Fatalf("prompt = %q, want %q", prompt, "User input text")
	}
}

func TestBuildPromptFromRequestUsesGenericFallbackFields(t *testing.T) {
	req := llm.Request{
		TaskTitle:    "Refactor prompt builder",
		TaskBody:     "Move platform-specific terms out of default prompt.",
		Requirements: "Keep behavior compatible.",
	}
	prompt := buildPromptFromRequest(req)
	for _, want := range []string{
		"Title: Refactor prompt builder",
		"Move platform-specific terms out of default prompt.",
		"Requirements:\nKeep behavior compatible.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got: %q", want, prompt)
		}
	}
}
