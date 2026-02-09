package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultMaxBytes caps loaded instruction size to avoid prompt bloat.
	DefaultMaxBytes = 32 * 1024
)

var defaultCandidateFiles = []string{
	"AGENT.md",
	"AGENTS.md",
	"CLAUDE.md",
}

// LoadOptions controls repository instruction discovery.
type LoadOptions struct {
	// CandidateFiles are checked in order for each directory layer.
	// At most one file is loaded per directory.
	CandidateFiles []string

	// MaxBytes limits the total serialized instruction content.
	// If <= 0, DefaultMaxBytes is used.
	MaxBytes int
}

// LoadResult is the output of instruction discovery.
type LoadResult struct {
	// Content contains merged markdown sections.
	Content string

	// Sources are source file paths relative to repository root, in load order.
	Sources []string

	// Truncated indicates the content hit MaxBytes.
	Truncated bool
}

// Load discovers and merges repository instructions from root to workDir.
// For each directory layer, only the first non-empty candidate file is loaded.
func Load(workDir string, opts LoadOptions) LoadResult {
	if strings.TrimSpace(workDir) == "" {
		return LoadResult{}
	}

	absWorkDir, err := filepath.Abs(workDir)
	if err == nil {
		workDir = absWorkDir
	}
	workDir = filepath.Clean(workDir)

	root := findRepoRoot(workDir)
	dirs := dirsFromRoot(root, workDir)

	candidates := opts.CandidateFiles
	if len(candidates) == 0 {
		candidates = append([]string{}, defaultCandidateFiles...)
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	remaining := maxBytes
	parts := make([]string, 0, len(dirs))
	sources := make([]string, 0, len(dirs))
	seenResolved := map[string]struct{}{}
	truncated := false

	for _, dir := range dirs {
		for _, filename := range candidates {
			path := filepath.Join(dir, filename)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			content := strings.TrimSpace(string(data))
			if content == "" {
				continue
			}

			resolved := filepath.Clean(path)
			if p, err := filepath.EvalSymlinks(path); err == nil {
				resolved = filepath.Clean(p)
			}
			if _, ok := seenResolved[resolved]; ok {
				continue
			}

			relPath := relToRoot(root, path)
			section := fmt.Sprintf("## %s\n%s", relPath, content)

			appended, wasTruncated := appendWithinLimit(&parts, section, &remaining)
			if wasTruncated {
				truncated = true
			}
			if appended {
				sources = append(sources, relPath)
				seenResolved[resolved] = struct{}{}
			}
			break
		}
		if truncated || remaining <= 0 {
			break
		}
	}

	return LoadResult{
		Content:   strings.Join(parts, "\n\n"),
		Sources:   sources,
		Truncated: truncated,
	}
}

func appendWithinLimit(parts *[]string, section string, remaining *int) (appended bool, truncated bool) {
	if *remaining <= 0 {
		return false, true
	}

	separatorLen := 0
	if len(*parts) > 0 {
		separatorLen = 2 // "\n\n"
	}
	needed := separatorLen + len(section)

	if needed <= *remaining {
		*parts = append(*parts, section)
		*remaining -= needed
		return true, false
	}

	// Partial section if we have room after separator.
	available := *remaining - separatorLen
	if available > 0 {
		if available > len(section) {
			available = len(section)
		}
		*parts = append(*parts, section[:available])
		*remaining -= separatorLen + available
		return true, true
	}

	return false, true
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

func dirsFromRoot(root, workDir string) []string {
	rel, err := filepath.Rel(root, workDir)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return []string{workDir}
	}
	dirs := []string{root}
	if rel == "." {
		return dirs
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		dirs = append(dirs, cur)
	}
	return dirs
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}
