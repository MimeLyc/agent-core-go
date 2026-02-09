package builtin

import (
	"testing"

	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

func TestRegisterAllSkipsGitHubByDefault(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry)

	for _, name := range []string{"github_get_issue", "github_create_comment", "github_list_issues"} {
		if registry.Has(name) {
			t.Fatalf("expected %s to be excluded from default builtins", name)
		}
	}
}

func TestRegisterAllWithGitHubIncludesGitHubTools(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAllWithGitHub(registry)

	for _, name := range []string{"github_get_issue", "github_create_comment", "github_list_issues"} {
		if !registry.Has(name) {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}
