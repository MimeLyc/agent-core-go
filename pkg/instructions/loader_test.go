package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrefersAGENTOverCLAUDEInSameDirectory(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWriteFile(t, filepath.Join(repo, "AGENT.md"), "agent instructions")
	mustWriteFile(t, filepath.Join(repo, "CLAUDE.md"), "claude instructions")

	result := Load(repo, LoadOptions{})
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d (%v)", len(result.Sources), result.Sources)
	}
	if result.Sources[0] != "AGENT.md" {
		t.Fatalf("expected AGENT.md source, got %s", result.Sources[0])
	}
	if strings.Contains(result.Content, "claude instructions") {
		t.Fatalf("unexpected CLAUDE.md content in result: %q", result.Content)
	}
	if !strings.Contains(result.Content, "agent instructions") {
		t.Fatalf("expected AGENT.md content in result: %q", result.Content)
	}
}

func TestLoadAggregatesInstructionsFromRootToLeaf(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, ".git"))
	nested := filepath.Join(repo, "services", "api")
	mustMkdir(t, nested)

	mustWriteFile(t, filepath.Join(repo, "AGENT.md"), "root rules")
	mustWriteFile(t, filepath.Join(repo, "services", "AGENT.md"), "services rules")
	mustWriteFile(t, filepath.Join(nested, "AGENT.md"), "api rules")

	result := Load(nested, LoadOptions{})
	if len(result.Sources) != 3 {
		t.Fatalf("expected 3 sources, got %d (%v)", len(result.Sources), result.Sources)
	}
	wantSources := []string{"AGENT.md", "services/AGENT.md", "services/api/AGENT.md"}
	for i, want := range wantSources {
		if result.Sources[i] != want {
			t.Fatalf("source %d mismatch: want %s, got %s", i, want, result.Sources[i])
		}
	}
	rootPos := strings.Index(result.Content, "root rules")
	servicesPos := strings.Index(result.Content, "services rules")
	apiPos := strings.Index(result.Content, "api rules")
	if !(rootPos >= 0 && servicesPos > rootPos && apiPos > servicesPos) {
		t.Fatalf("expected root->services->api ordering, got: %q", result.Content)
	}
}

func TestLoadDeduplicatesSymlinkedInstructionFiles(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWriteFile(t, filepath.Join(repo, "AGENT.md"), "canonical rules")
	if err := os.Symlink("./AGENT.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result := Load(repo, LoadOptions{CandidateFiles: []string{"CLAUDE.md", "AGENT.md"}})
	if len(result.Sources) != 1 {
		t.Fatalf("expected deduped single source, got %d (%v)", len(result.Sources), result.Sources)
	}
	if strings.Count(result.Content, "canonical rules") != 1 {
		t.Fatalf("expected one copy of canonical rules, got: %q", result.Content)
	}
}

func TestLoadRespectsMaxBytes(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWriteFile(t, filepath.Join(repo, "AGENT.md"), strings.Repeat("x", 200))

	result := Load(repo, LoadOptions{MaxBytes: 80})
	if !result.Truncated {
		t.Fatalf("expected truncated=true")
	}
	if len(result.Content) > 80 {
		t.Fatalf("expected content length <= 80, got %d", len(result.Content))
	}
}

func TestLoadStopsAtRepositoryRoot(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	nested := filepath.Join(repo, "sub")
	mustMkdir(t, nested)
	mustMkdir(t, filepath.Join(repo, ".git"))

	mustWriteFile(t, filepath.Join(parent, "AGENT.md"), "parent rules")
	mustWriteFile(t, filepath.Join(repo, "AGENT.md"), "repo rules")

	result := Load(nested, LoadOptions{})
	if strings.Contains(result.Content, "parent rules") {
		t.Fatalf("expected parent instructions to be ignored, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "repo rules") {
		t.Fatalf("expected repo instructions to be included, got %q", result.Content)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
