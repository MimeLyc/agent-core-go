package soul

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_ExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-soul.md")
	os.WriteFile(path, []byte("You are a helpful pirate."), 0644)

	result := Load("", LoadOptions{File: path})
	if result.Content != "You are a helpful pirate." {
		t.Errorf("expected pirate soul, got %q", result.Content)
	}
	if result.Source != path {
		t.Errorf("expected source %q, got %q", path, result.Source)
	}
}

func TestLoad_ExplicitFileMissing(t *testing.T) {
	result := Load("", LoadOptions{File: "/nonexistent/SOUL.md"})
	if result.Content != "" {
		t.Errorf("expected empty content for missing file, got %q", result.Content)
	}
}

func TestLoad_DiscoverInWorkDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("Be concise."), 0644)

	result := Load(dir, LoadOptions{})
	if result.Content != "Be concise." {
		t.Errorf("expected 'Be concise.', got %q", result.Content)
	}
}

func TestLoad_DiscoverInRepoRoot(t *testing.T) {
	root := t.TempDir()
	// Create .git to mark repo root
	os.Mkdir(filepath.Join(root, ".git"), 0755)
	os.WriteFile(filepath.Join(root, DefaultFileName), []byte("Root soul."), 0644)

	subdir := filepath.Join(root, "sub", "dir")
	os.MkdirAll(subdir, 0755)

	result := Load(subdir, LoadOptions{})
	if result.Content != "Root soul." {
		t.Errorf("expected 'Root soul.', got %q", result.Content)
	}
}

func TestLoad_WorkDirTakesPrecedence(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".git"), 0755)
	os.WriteFile(filepath.Join(root, DefaultFileName), []byte("Root soul."), 0644)

	subdir := filepath.Join(root, "sub")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, DefaultFileName), []byte("Sub soul."), 0644)

	result := Load(subdir, LoadOptions{})
	if result.Content != "Sub soul." {
		t.Errorf("expected 'Sub soul.', got %q", result.Content)
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("   \n  "), 0644)

	result := Load(dir, LoadOptions{})
	if result.Content != "" {
		t.Errorf("expected empty content for whitespace-only file, got %q", result.Content)
	}
}

func TestLoad_EmptyWorkDir(t *testing.T) {
	result := Load("", LoadOptions{})
	if result.Content != "" {
		t.Errorf("expected empty content for empty workdir, got %q", result.Content)
	}
}

func TestLoad_Truncation(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 100)
	os.WriteFile(filepath.Join(dir, DefaultFileName), []byte(content), 0644)

	result := Load(dir, LoadOptions{MaxBytes: 50})
	if len(result.Content) != 50 {
		t.Errorf("expected 50 bytes, got %d", len(result.Content))
	}
	if !result.Truncated {
		t.Error("expected Truncated=true")
	}
}

func TestLoad_NoTruncationUnderLimit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("short"), 0644)

	result := Load(dir, LoadOptions{MaxBytes: 100})
	if result.Truncated {
		t.Error("expected Truncated=false for small content")
	}
}
