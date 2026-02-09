package skills

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// SkillFileName is the canonical filename for a skill definition.
	SkillFileName = "SKILL.md"
	// SkillDirsEnv overrides default discovery directories when set.
	SkillDirsEnv = "CODEX_SKILL_DIRS"

	// DefaultPromptBlockMaxBytes limits skill metadata injected into prompts.
	DefaultPromptBlockMaxBytes = 8 * 1024
	// DefaultSkillReadMaxBytes limits content returned by read_skill.
	DefaultSkillReadMaxBytes = 64 * 1024
)

// Skill describes one discoverable skill.
type Skill struct {
	Name        string
	Description string
	Path        string
}

// PromptBlock is a rendered metadata block for system prompts.
type PromptBlock struct {
	Content    string
	SkillCount int
	Truncated  bool
}

// Discover scans search directories recursively and returns discovered skills.
func Discover(searchDirs []string) ([]Skill, error) {
	dirs := normalizePaths(searchDirs)
	seenPaths := make(map[string]struct{})
	out := make([]Skill, 0)

	for _, root := range dirs {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() || d.Name() != SkillFileName {
				return nil
			}

			resolved := filepath.Clean(path)
			if rp, err := filepath.EvalSymlinks(path); err == nil {
				resolved = filepath.Clean(rp)
			}
			if _, ok := seenPaths[resolved]; ok {
				return nil
			}

			skill, err := parseSkill(path, root)
			if err != nil {
				return nil
			}
			seenPaths[resolved] = struct{}{}
			out = append(out, skill)
			return nil
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})

	return out, nil
}

// DefaultSearchDirs returns built-in skill search directories for a workdir.
func DefaultSearchDirs(workDir string) []string {
	if raw := strings.TrimSpace(os.Getenv(SkillDirsEnv)); raw != "" {
		return normalizePaths(parsePaths(raw))
	}

	var dirs []string
	if strings.TrimSpace(workDir) != "" {
		root := findRepoRoot(workDir)
		for _, d := range dirsFromRoot(root, workDir) {
			dirs = append(dirs,
				filepath.Join(d, ".agents", "skills"),
				filepath.Join(d, ".codex", "skills"),
			)
		}
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		dirs = append(dirs,
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".codex", "skills"),
			filepath.Join(home, ".codex", "superpowers", "skills"),
			filepath.Join(home, ".claude", "skills"),
		)
	}

	dirs = append(dirs, "/etc/codex/skills")
	return normalizePaths(dirs)
}

// BuildPromptBlock renders skill metadata for prompt injection.
func BuildPromptBlock(skills []Skill, maxBytes int) PromptBlock {
	if len(skills) == 0 {
		return PromptBlock{}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultPromptBlockMaxBytes
	}

	header := strings.Join([]string{
		"## Available Skills",
		"",
		"Skills use progressive disclosure: start from metadata, then call `read_skill` to load full `SKILL.md` only when needed.",
		"Use `list_skills` to refresh discovery if paths change during execution.",
		"",
	}, "\n")

	var builder strings.Builder
	builder.Grow(maxBytes)
	remaining := maxBytes
	truncated := false

	writeCapped := func(text string) {
		if remaining <= 0 {
			truncated = true
			return
		}
		if len(text) <= remaining {
			builder.WriteString(text)
			remaining -= len(text)
			return
		}
		builder.WriteString(text[:remaining])
		remaining = 0
		truncated = true
	}

	writeCapped(header)
	count := 0
	for _, skill := range skills {
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "No description."
		}
		if len(desc) > 180 {
			desc = desc[:180] + "..."
		}
		line := fmt.Sprintf("- `%s`: %s (path: `%s`)\n", skill.Name, desc, filepath.ToSlash(skill.Path))
		if remaining <= 0 {
			truncated = true
			break
		}
		writeCapped(line)
		count++
	}

	return PromptBlock{
		Content:    strings.TrimSpace(builder.String()),
		SkillCount: count,
		Truncated:  truncated,
	}
}

// ReadFile reads a SKILL.md file with size limits.
func ReadFile(path string, maxBytes int) (content string, truncated bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultSkillReadMaxBytes
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	if len(data) <= maxBytes {
		return string(data), false, nil
	}
	return string(data[:maxBytes]), true, nil
}

func parseSkill(path, root string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}

	name, desc, body := parseFrontMatter(data)
	if strings.TrimSpace(name) == "" {
		name = inferSkillName(path, root)
	}
	if strings.TrimSpace(desc) == "" {
		desc = inferDescription(body)
	}
	if strings.TrimSpace(desc) == "" {
		desc = "No description."
	}

	abs := path
	if ap, err := filepath.Abs(path); err == nil {
		abs = ap
	}
	return Skill{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(desc),
		Path:        filepath.Clean(abs),
	}, nil
}

func parseFrontMatter(data []byte) (name, desc, body string) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	body = text

	if !strings.HasPrefix(text, "---\n") {
		return "", "", body
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", "", body
	}

	front := rest[:end]
	body = rest[end+len("\n---\n"):]
	scanner := bufio.NewScanner(strings.NewReader(front))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc, body
}

func inferSkillName(path, root string) string {
	relDir, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return filepath.Base(filepath.Dir(path))
	}
	relDir = filepath.ToSlash(relDir)
	relDir = strings.TrimSpace(relDir)
	if relDir == "." || relDir == "" {
		return filepath.Base(root)
	}
	return relDir
}

func inferDescription(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 160 {
			return line[:160] + "..."
		}
		return line
	}
	return ""
}

func parsePaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var parts []string
	if strings.Contains(raw, ",") || strings.Contains(raw, "\n") {
		parts = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n'
		})
	} else {
		parts = filepath.SplitList(raw)
	}
	return normalizePaths(parts)
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		expanded := p
		if strings.HasPrefix(expanded, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~"))
			}
		}
		expanded = filepath.Clean(expanded)
		if _, ok := seen[expanded]; ok {
			continue
		}
		seen[expanded] = struct{}{}
		out = append(out, expanded)
	}
	return out
}

func findRepoRoot(workDir string) string {
	dir := filepath.Clean(workDir)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Clean(workDir)
		}
		dir = parent
	}
}

func dirsFromRoot(root, workDir string) []string {
	root = filepath.Clean(root)
	workDir = filepath.Clean(workDir)
	rel, err := filepath.Rel(root, workDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []string{workDir}
	}
	out := []string{root}
	if rel == "." {
		return out
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		out = append(out, cur)
	}
	return out
}

// FilterByQuery filters skills by name/description query.
func FilterByQuery(skills []Skill, query string) []Skill {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]Skill{}, skills...)
	}

	out := make([]Skill, 0, len(skills))
	for _, skill := range skills {
		name := strings.ToLower(skill.Name)
		desc := strings.ToLower(skill.Description)
		if strings.Contains(name, query) || strings.Contains(desc, query) {
			out = append(out, skill)
		}
	}
	return out
}

// ResolveByName returns matched skills with exact or case-insensitive fallback.
func ResolveByName(skills []Skill, name string) []Skill {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	matches := make([]Skill, 0, 1)
	for _, skill := range skills {
		if skill.Name == name {
			matches = append(matches, skill)
		}
	}
	if len(matches) > 0 {
		return matches
	}

	lower := strings.ToLower(name)
	for _, skill := range skills {
		if strings.ToLower(skill.Name) == lower {
			matches = append(matches, skill)
		}
	}
	return matches
}

// ResolveByPath finds skills by normalized path.
func ResolveByPath(skills []Skill, path string) []Skill {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	clean := filepath.Clean(path)
	if ap, err := filepath.Abs(clean); err == nil {
		clean = ap
	}

	matches := make([]Skill, 0, 1)
	for _, skill := range skills {
		if samePath(skill.Path, clean) {
			matches = append(matches, skill)
		}
	}
	return matches
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return filepath.Clean(ra) == filepath.Clean(rb)
	}
	return false
}

// JoinAmbiguousPaths renders candidate paths for ambiguous skills.
func JoinAmbiguousPaths(skills []Skill) string {
	var b bytes.Buffer
	for i, skill := range skills {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(filepath.ToSlash(skill.Path))
	}
	return b.String()
}
