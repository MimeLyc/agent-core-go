package orchestrator

import (
	"strings"
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

func TestBuildPromptFromRequestUsesGenericTaskContext(t *testing.T) {
	req := llm.Request{
		Mode:         "task",
		RepoFullName: "owner/repo",
		TaskID:       "task-42",
		TaskTitle:    "Refactor prompt builder",
		TaskBody:     "Move platform-specific terms out of default prompt.",
		TaskLabels:   []string{"sdk", "migration"},
		TaskComments: []llm.Comment{
			{User: "alice", Body: "Please keep backward compatibility."},
		},
	}

	prompt := buildPromptFromRequest(req)
	for _, want := range []string{
		"## Task Context",
		"Task ID: task-42",
		"Title: Refactor prompt builder",
		"Body:\nMove platform-specific terms out of default prompt.",
		"Labels: sdk, migration",
		"@alice: Please keep backward compatibility.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got: %q", want, prompt)
		}
	}
}
