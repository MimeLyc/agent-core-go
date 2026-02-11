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
	SkillDirsEnv = "SKILL_DIRS"
	// SystemSkillDirsEnv appends additional system-level skill directories.
	SystemSkillDirsEnv = "SYSTEM_SKILL_DIRS"

	// EnvActiveSkillName tracks the currently active skill name in tool context.
	EnvActiveSkillName = "ACTIVE_SKILL_NAME"
	// EnvActiveSkillPath tracks the currently active skill path in tool context.
	EnvActiveSkillPath = "ACTIVE_SKILL_PATH"
	// EnvActiveSkillAllowedTools stores allowed tool patterns for active skill.
	EnvActiveSkillAllowedTools = "ACTIVE_SKILL_ALLOWED_TOOLS"
	// EnvClaudeSessionID is available for template substitution in skill bodies.
	EnvClaudeSessionID = "CLAUDE_SESSION_ID"

	// DefaultPromptBlockMaxBytes limits skill metadata injected into prompts.
	DefaultPromptBlockMaxBytes = 8 * 1024
	// DefaultSkillReadMaxBytes limits content returned by read_skill.
	DefaultSkillReadMaxBytes = 64 * 1024
)

// SkillScope identifies where a skill comes from for precedence resolution.
type SkillScope string

const (
	ScopeUnknown  SkillScope = "unknown"
	ScopeProject  SkillScope = "project"
	ScopePersonal SkillScope = "personal"
	ScopeSystem   SkillScope = "system"
)

// Skill describes one discoverable skill.
type Skill struct {
	Name        string
	Description string
	Path        string
	Scope       SkillScope

	Invocation             string
	UserInvocable          bool
	DisableModelInvocation bool
	AllowedTools           []string

	sourceOrder int
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

	for idx, root := range dirs {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		scope := classifyScope(root)

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

			skill, err := parseSkill(path, root, idx, scope)
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
		dirs := normalizePaths(parsePaths(raw))
		dirs = append(dirs, normalizePaths(parsePaths(os.Getenv(SystemSkillDirsEnv)))...)
		return normalizePaths(dirs)
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

	dirs = append(dirs, normalizePaths(parsePaths(os.Getenv(SystemSkillDirsEnv)))...)
	dirs = append(dirs, "/etc/codex/skills")
	return normalizePaths(dirs)
}

// BuildPromptBlock renders skill metadata for prompt injection.
func BuildPromptBlock(skills []Skill, maxBytes int) PromptBlock {
	visible := canonicalSkills(skills, true)
	if len(visible) == 0 {
		return PromptBlock{}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultPromptBlockMaxBytes
	}

	header := strings.Join([]string{
		"## Available Skills",
		"",
		"Skills use progressive disclosure: surface metadata first, load full content only on invocation.",
		"When a relevant skill applies, invoke it with `use_skill` before taking action.",
		"Use `list_skills` to discover available skills; use `read_skill` for direct inspection when needed.",
		"Skills marked with disable-model-invocation are intentionally excluded from this list.",
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
	for _, skill := range visible {
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "No description."
		}
		if len(desc) > 180 {
			desc = desc[:180] + "..."
		}
		scope := string(skill.Scope)
		if scope == "" {
			scope = string(ScopeUnknown)
		}
		line := fmt.Sprintf("- `%s` [%s]: %s (path: `%s`)\n", skill.Name, scope, desc, filepath.ToSlash(skill.Path))
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

func parseSkill(path, root string, sourceOrder int, scope SkillScope) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}

	meta, body := parseFrontMatter(data)
	name := meta.Name
	desc := meta.Description
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
		Name:                   strings.TrimSpace(name),
		Description:            strings.TrimSpace(desc),
		Path:                   filepath.Clean(abs),
		Scope:                  scope,
		Invocation:             meta.Invocation,
		UserInvocable:          meta.UserInvocable,
		DisableModelInvocation: meta.DisableModelInvocation,
		AllowedTools:           meta.AllowedTools,
		sourceOrder:            sourceOrder,
	}, nil
}

type frontMatter struct {
	Name                   string
	Description            string
	Invocation             string
	UserInvocable          bool
	DisableModelInvocation bool
	AllowedTools           []string
}

func parseFrontMatter(data []byte) (meta frontMatter, body string) {
	meta.UserInvocable = true

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	body = text

	if !strings.HasPrefix(text, "---\n") {
		return meta, body
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return meta, body
	}

	front := rest[:end]
	body = rest[end+len("\n---\n"):]
	lines := strings.Split(front, "\n")
	currentListKey := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if currentListKey != "" {
			if strings.HasPrefix(line, "- ") {
				item := strings.TrimSpace(strings.TrimPrefix(line, "- "))
				setFrontMatterValue(&meta, currentListKey, item, true)
				continue
			}
			currentListKey = ""
		}

		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		if val == "" {
			currentListKey = key
			continue
		}
		setFrontMatterValue(&meta, key, val, false)
	}
	return meta, body
}

func setFrontMatterValue(meta *frontMatter, key, raw string, isListItem bool) {
	clean := strings.Trim(strings.TrimSpace(raw), `"'`)
	switch key {
	case "name":
		meta.Name = clean
	case "description":
		meta.Description = clean
	case "invocation":
		meta.Invocation = clean
	case "user-invocable":
		if b, ok := parseBool(clean); ok {
			meta.UserInvocable = b
		}
	case "disable-model-invocation":
		if b, ok := parseBool(clean); ok {
			meta.DisableModelInvocation = b
		}
	case "allowed-tools":
		values := []string{clean}
		if !isListItem {
			values = parseAllowedToolsValue(raw)
		}
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			meta.AllowedTools = append(meta.AllowedTools, v)
		}
	}
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1":
		return true, true
	case "false", "no", "0":
		return false, true
	default:
		return false, false
	}
}

func parseAllowedToolsValue(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.Trim(strings.TrimSpace(part), `"'`)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
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

func classifyScope(root string) SkillScope {
	clean := filepath.Clean(root)
	if clean == filepath.Clean("/etc/codex/skills") || isExtraSystemSkillDir(clean) {
		return ScopeSystem
	}

	home, _ := os.UserHomeDir()
	home = strings.TrimSpace(home)
	if home != "" {
		for _, prefix := range []string{
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".codex", "skills"),
			filepath.Join(home, ".codex", "superpowers", "skills"),
			filepath.Join(home, ".claude", "skills"),
		} {
			if strings.HasPrefix(clean, filepath.Clean(prefix)) {
				return ScopePersonal
			}
		}
	}

	if strings.Contains(clean, string(filepath.Separator)+".agents"+string(filepath.Separator)+"skills") ||
		strings.Contains(clean, string(filepath.Separator)+".codex"+string(filepath.Separator)+"skills") {
		return ScopeProject
	}

	return ScopeUnknown
}

func isExtraSystemSkillDir(path string) bool {
	for _, dir := range normalizePaths(parsePaths(os.Getenv(SystemSkillDirsEnv))) {
		if filepath.Clean(dir) == filepath.Clean(path) {
			return true
		}
	}
	return false
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

// ResolveForInvocation chooses one best-matching skill for invocation.
func ResolveForInvocation(skills []Skill, name string) (Skill, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Skill{}, fmt.Errorf("skill name is required")
	}

	var matches []Skill
	for _, skill := range skills {
		if skill.Name == name {
			matches = append(matches, skill)
		}
	}
	if len(matches) == 0 {
		lower := strings.ToLower(name)
		for _, skill := range skills {
			if strings.ToLower(skill.Name) == lower {
				matches = append(matches, skill)
			}
		}
	}
	if len(matches) == 0 {
		return Skill{}, fmt.Errorf("skill not found: %s", name)
	}

	best := matches[0]
	for _, candidate := range matches[1:] {
		if betterSkill(candidate, best) {
			best = candidate
		}
	}
	return best, nil
}

// RenderForInvocation loads and renders a skill body with variable substitution.
func RenderForInvocation(skill Skill, arguments, sessionID string, maxBytes int) (content string, truncated bool, err error) {
	raw, truncated, err := ReadFile(skill.Path, maxBytes)
	if err != nil {
		return "", false, err
	}
	_, body := parseFrontMatter([]byte(raw))
	rendered := strings.TrimSpace(body)

	argText := strings.TrimSpace(arguments)
	hasArgPlaceholder := strings.Contains(rendered, "$ARGUMENTS") || strings.Contains(rendered, "${ARGUMENTS}")
	rendered = strings.ReplaceAll(rendered, "${ARGUMENTS}", argText)
	rendered = strings.ReplaceAll(rendered, "$ARGUMENTS", argText)
	rendered = strings.ReplaceAll(rendered, "${CLAUDE_SESSION_ID}", strings.TrimSpace(sessionID))

	if argText != "" && !hasArgPlaceholder {
		if strings.TrimSpace(rendered) != "" {
			rendered += "\n\n"
		}
		rendered += "ARGUMENTS:\n" + argText
	}

	return rendered, truncated, nil
}

// ParseSlashSkillCommand parses "/skill-name args..." command format.
func ParseSlashSkillCommand(input string) (name, arguments string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return "", "", false
	}
	firstLine := trimmed
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		firstLine = strings.TrimSpace(trimmed[:idx])
	}
	firstLine = strings.TrimPrefix(firstLine, "/")
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return "", "", false
	}

	parts := strings.Fields(firstLine)
	if len(parts) == 0 {
		return "", "", false
	}
	name = parts[0]
	if !isValidSlashSkillName(name) {
		return "", "", false
	}

	arguments = strings.TrimSpace(strings.TrimPrefix(firstLine, name))
	return name, arguments, true
}

func isValidSlashSkillName(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', '/', '.':
			continue
		default:
			return false
		}
	}
	return name != ""
}

// ParseAllowedToolsEnv parses active skill allowlist from environment value.
func ParseAllowedToolsEnv(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "\n") {
		var out []string
		for _, part := range strings.Split(value, "\n") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
		return out
	}
	return parseAllowedToolsValue(value)
}

// JoinAllowedToolsEnv serializes allowed-tools values for environment storage.
func JoinAllowedToolsEnv(allowed []string) string {
	filtered := make([]string, 0, len(allowed))
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	return strings.Join(filtered, "\n")
}

// IsToolAllowed checks if a tool is permitted by skill allowed-tools patterns.
func IsToolAllowed(toolName string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	tool := strings.ToLower(strings.TrimSpace(toolName))
	if tool == "" {
		return false
	}

	for _, raw := range allowed {
		pattern := normalizeAllowedPattern(raw)
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if wildcardMatch(pattern, tool) {
			return true
		}

		switch pattern {
		case "bash":
			if tool == "bash" {
				return true
			}
		case "git":
			if strings.HasPrefix(tool, "git_") {
				return true
			}
		case "read", "grep", "glob", "ls":
			if tool == "read_file" || tool == "list_files" {
				return true
			}
		case "write", "edit":
			if tool == "write_file" {
				return true
			}
		case "skill", "skills":
			if tool == "use_skill" || tool == "list_skills" || tool == "read_skill" {
				return true
			}
		}
	}
	return false
}

func normalizeAllowedPattern(raw string) string {
	pattern := strings.TrimSpace(strings.ToLower(raw))
	if pattern == "" {
		return ""
	}
	if idx := strings.Index(pattern, "("); idx >= 0 {
		pattern = strings.TrimSpace(pattern[:idx])
	}
	pattern = strings.TrimSpace(strings.Trim(pattern, `"'`))
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		switch prefix {
		case "git":
			return "git_*"
		default:
			return prefix + "*"
		}
	}
	return pattern
}

func wildcardMatch(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if strings.Contains(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasSuffix(pattern, "*") {
			return strings.HasPrefix(value, prefix)
		}
	}
	return false
}

func canonicalSkills(skills []Skill, skipModelDisabled bool) []Skill {
	if len(skills) == 0 {
		return nil
	}
	byName := make(map[string]Skill)
	for _, skill := range skills {
		if skipModelDisabled && skill.DisableModelInvocation {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(skill.Name))
		if key == "" {
			continue
		}
		current, exists := byName[key]
		if !exists || betterSkill(skill, current) {
			byName[key] = skill
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, skill := range byName {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func betterSkill(a, b Skill) bool {
	scoreA := precedenceScore(a)
	scoreB := precedenceScore(b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}
	return a.Path < b.Path
}

func precedenceScore(skill Skill) int {
	scopeRank := map[SkillScope]int{
		ScopeUnknown:  0,
		ScopeSystem:   1,
		ScopePersonal: 2,
		ScopeProject:  3,
	}
	return scopeRank[skill.Scope]*1_000_000 + skill.sourceOrder
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
