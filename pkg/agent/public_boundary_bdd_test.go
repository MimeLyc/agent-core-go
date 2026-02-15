package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPublicBoundaryDoesNotExposeLLMPackage(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	publicLLMPath := filepath.Join(repoRoot, "pkg", "llm")
	if _, err := os.Stat(publicLLMPath); err == nil {
		t.Fatalf("expected %s to be absent (llm must be internal only)", publicLLMPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("failed to stat %s: %v", publicLLMPath, err)
	}
}

func TestPublicBoundaryDoesNotExposeOrchestratorPackage(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	publicPath := filepath.Join(repoRoot, "pkg", "orchestrator")
	if _, err := os.Stat(publicPath); err == nil {
		t.Fatalf("expected %s to be absent (orchestrator must be internal only)", publicPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("failed to stat %s: %v", publicPath, err)
	}
}
