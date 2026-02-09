package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolContextValidatePath(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := NewToolContext(tmpDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative path", "subdir/file.txt", false},
		{"current dir", ".", false},
		{"parent escape", "../outside", true},
		{"absolute in workdir", filepath.Join(tmpDir, "file.txt"), false},
		{"absolute outside", "/etc/passwd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ctx.ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestToolContextValidatePathNoWorkDir(t *testing.T) {
	ctx := &ToolContext{}

	_, err := ctx.ValidatePath("file.txt")
	if err != ErrNoWorkDir {
		t.Errorf("expected ErrNoWorkDir, got %v", err)
	}
}

func TestToolContextPermissions(t *testing.T) {
	ctx := NewToolContext("/tmp")

	// Default permissions should allow everything
	if err := ctx.CheckBash(); err != nil {
		t.Errorf("CheckBash() = %v, want nil", err)
	}
	if err := ctx.CheckFileRead(); err != nil {
		t.Errorf("CheckFileRead() = %v, want nil", err)
	}
	if err := ctx.CheckFileWrite(); err != nil {
		t.Errorf("CheckFileWrite() = %v, want nil", err)
	}
	if err := ctx.CheckGit(); err != nil {
		t.Errorf("CheckGit() = %v, want nil", err)
	}
	if err := ctx.CheckGitHub(); err != nil {
		t.Errorf("CheckGitHub() = %v, want nil", err)
	}

	// Restricted permissions
	ctx.WithPermissions(RestrictedPermissions())

	if err := ctx.CheckBash(); err != ErrBashNotAllowed {
		t.Errorf("CheckBash() = %v, want ErrBashNotAllowed", err)
	}
	if err := ctx.CheckFileRead(); err != nil {
		t.Errorf("CheckFileRead() = %v, want nil", err)
	}
	if err := ctx.CheckFileWrite(); err != ErrFileWriteNotAllowed {
		t.Errorf("CheckFileWrite() = %v, want ErrFileWriteNotAllowed", err)
	}
}

func TestToolContextFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := NewToolContext(tmpDir)

	if !ctx.FileExists("exists.txt") {
		t.Error("FileExists() = false for existing file")
	}
	if ctx.FileExists("nonexistent.txt") {
		t.Error("FileExists() = true for nonexistent file")
	}
}

func TestToolContextIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test directory
	testDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := NewToolContext(tmpDir)

	if !ctx.IsDir("subdir") {
		t.Error("IsDir() = false for directory")
	}
	if ctx.IsDir("file.txt") {
		t.Error("IsDir() = true for file")
	}
}

func TestToolContextChaining(t *testing.T) {
	ctx := NewToolContext("/tmp").
		WithPermissions(RestrictedPermissions()).
		WithGitHub("token", "owner", "repo").
		WithEnv("KEY", "value").
		WithBashTimeout(120)

	if ctx.GitHubToken != "token" {
		t.Errorf("GitHubToken = %q, want token", ctx.GitHubToken)
	}
	if ctx.RepoOwner != "owner" {
		t.Errorf("RepoOwner = %q, want owner", ctx.RepoOwner)
	}
	if ctx.RepoName != "repo" {
		t.Errorf("RepoName = %q, want repo", ctx.RepoName)
	}
	if ctx.Env["KEY"] != "value" {
		t.Errorf("Env[KEY] = %q, want value", ctx.Env["KEY"])
	}
	if ctx.BashTimeout != 120 {
		t.Errorf("BashTimeout = %d, want 120", ctx.BashTimeout)
	}
}
