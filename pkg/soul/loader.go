package soul

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultFileName is the default SOUL file name.
	DefaultFileName = "SOUL.md"

	// DefaultMaxBytes caps loaded SOUL content size.
	DefaultMaxBytes = 16 * 1024
)

// LoadOptions controls SOUL file loading.
type LoadOptions struct {
	// File is an explicit path to the SOUL file.
	// If set, only this path is checked (no discovery).
	File string

	// MaxBytes limits the loaded content size.
	// If <= 0, DefaultMaxBytes is used.
	MaxBytes int
}

// LoadResult is the output of SOUL file loading.
type LoadResult struct {
	// Content is the SOUL file content.
	Content string

	// Source is the resolved file path (empty if not found).
	Source string

	// Truncated indicates the content hit MaxBytes.
	Truncated bool
}

// Load reads the SOUL file content.
// If opts.File is set, it reads from that exact path.
// Otherwise it searches for SOUL.md in workDir, then the repo root.
func Load(workDir string, opts LoadOptions) LoadResult {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	if opts.File != "" {
		return readSoulFile(opts.File, maxBytes)
	}

	if strings.TrimSpace(workDir) == "" {
		return LoadResult{}
	}

	absWorkDir, err := filepath.Abs(workDir)
	if err == nil {
		workDir = absWorkDir
	}
	workDir = filepath.Clean(workDir)

	// Try workDir first
	result := readSoulFile(filepath.Join(workDir, DefaultFileName), maxBytes)
	if result.Content != "" {
		return result
	}

	// Try repo root
	root := findRepoRoot(workDir)
	if root != workDir {
		result = readSoulFile(filepath.Join(root, DefaultFileName), maxBytes)
		if result.Content != "" {
			return result
		}
	}

	return LoadResult{}
}

func readSoulFile(path string, maxBytes int) LoadResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return LoadResult{}
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return LoadResult{}
	}

	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		truncated = true
	}

	return LoadResult{
		Content:   content,
		Source:    path,
		Truncated: truncated,
	}
}

func findRepoRoot(workDir string) string {
	dir := workDir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return workDir
		}
		dir = parent
	}
}
