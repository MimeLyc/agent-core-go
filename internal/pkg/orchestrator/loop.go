package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/instructions"
	"github.com/MimeLyc/agent-core-go/pkg/skills"
	"github.com/MimeLyc/agent-core-go/pkg/soul"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

const (
	defaultMaxIterations = 0
	defaultMaxMessages   = 50
)

// generateToolUseID generates a unique ID for tool_use blocks that have empty IDs.
// This is needed because some LLM APIs may return tool_use blocks without IDs,
// but the API requires matching IDs between tool_use and tool_result.
func generateToolUseID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a simple counter-based ID if crypto/rand fails
		return fmt.Sprintf("tool_%d", time.Now().UnixNano())
	}
	return "tool_" + hex.EncodeToString(b)
}

// validateToolPairs checks that all tool_results have matching tool_uses in the messages.
// Returns an error if any orphaned tool_results are found.
func validateToolPairs(messages []llm.Message) error {
	// Collect all tool_use IDs and log them
	toolUseIDs := make(map[string]bool)
	toolUseLocations := make(map[string]int) // ID -> message index
	for i, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse {
				if block.ID == "" {
					log.Printf("[orchestrator] VALIDATION: tool_use at msg %d has empty ID (name=%s)", i, block.Name)
				} else {
					toolUseIDs[block.ID] = true
					toolUseLocations[block.ID] = i
				}
			}
		}
	}

	log.Printf("[orchestrator] VALIDATION: found %d tool_use IDs in %d messages", len(toolUseIDs), len(messages))

	// Check all tool_results have matching tool_uses
	var orphans []string
	for i, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolResult {
				if block.ToolUseID == "" {
					log.Printf("[orchestrator] VALIDATION: tool_result at msg %d has empty ToolUseID", i)
					orphans = append(orphans, fmt.Sprintf("msg[%d]:empty_id", i))
				} else if !toolUseIDs[block.ToolUseID] {
					log.Printf("[orchestrator] VALIDATION: tool_result at msg %d references missing tool_use %s", i, block.ToolUseID)
					orphans = append(orphans, fmt.Sprintf("msg[%d]:%s", i, block.ToolUseID))
				}
			}
		}
	}

	if len(orphans) > 0 {
		return fmt.Errorf("found %d orphaned tool_results: %v", len(orphans), orphans)
	}

	log.Printf("[orchestrator] VALIDATION: all tool pairs intact")
	return nil
}

// AgentLoop implements the Orchestrator interface.
type AgentLoop struct {
	// Provider is the LLM provider for making API calls.
	// This abstracts Claude, OpenAI, and other LLM backends.
	Provider llm.LLMProvider

	// Registry contains all available tools.
	Registry *tools.Registry
}

// NewAgentLoop creates a new agent loop orchestrator.
// The provider parameter accepts any LLMProvider implementation (ClaudeProvider, OpenAIProvider, etc.)
// or the legacy AgentRunner which implements LLMProvider for backward compatibility.
func NewAgentLoop(provider llm.LLMProvider, registry *tools.Registry) *AgentLoop {
	if registry == nil {
		registry = tools.NewRegistry()
	}
	return &AgentLoop{
		Provider: provider,
		Registry: registry,
	}
}

// Run executes the agent loop until completion or max iterations.
func (l *AgentLoop) Run(ctx context.Context, req OrchestratorRequest) (OrchestratorResult, error) {
	// Initialize state
	state := NewState(req.InitialMessages)

	// Set up tool context
	toolCtx := req.ToolContext
	if toolCtx == nil {
		toolCtx = tools.NewToolContext(req.WorkDir)
	}

	// Read repository instruction files from repo root if repo instructions not provided
	repoInstructions := req.RepoInstructions
	if repoInstructions == "" && req.WorkDir != "" {
		repoInstructions = readRepoInstructions(req.WorkDir, req.InstructionFiles)
	}

	// Load SOUL file
	soulContent := readSoulContent(req.WorkDir, req.SoulFile)

	// Handle explicit slash-skill invocation from the initial user message.
	// This mirrors Claude Code's user-triggered "/skill args" behavior.
	if applied, err := applySlashSkillInvocation(state, toolCtx, req.WorkDir); err != nil {
		log.Printf("[orchestrator] WARNING: slash skill invocation failed: %v", err)
	} else if applied {
		log.Printf("[orchestrator] applied explicit slash skill invocation")
	}

	// Build tool definitions from registry
	allTools := l.Registry.List()
	toolDefs := make([]llm.ToolDefinition, len(allTools))
	toolNames := make([]string, len(allTools))
	for i, t := range allTools {
		toolDefs[i] = llm.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		}
		toolNames[i] = t.Name()
	}
	log.Printf("[orchestrator] starting agent loop: workdir=%s tools=%v max_iterations=%d",
		req.WorkDir, toolNames, req.MaxIterations)

	// Build system prompt
	systemPrompt := buildSystemPrompt(req.SystemPrompt, soulContent, repoInstructions)
	log.Printf("[orchestrator] system prompt length: %d chars", len(systemPrompt))

	// Set max iterations.
	maxIterations := req.MaxIterations
	hasIterationLimit := !req.DisableIterationLimit && maxIterations > 0

	// Set max messages for history truncation
	maxMessages := req.MaxMessages
	if maxMessages <= 0 {
		maxMessages = defaultMaxMessages
	}

	// Initialize compactor if enabled
	var compactor *Compactor
	if req.CompactConfig.Enabled {
		compactor = NewCompactor(l.Provider, req.CompactConfig)
		log.Printf("[orchestrator] compaction enabled: threshold=%d keep_recent=%d",
			req.CompactConfig.Threshold, req.CompactConfig.KeepRecent)
	}

	// Track all tool_use IDs to detect and fix duplicates from the LLM
	seenToolUseIDs := make(map[string]bool)

	// Agent loop
	for !hasIterationLimit || state.Iterations < maxIterations {
		select {
		case <-ctx.Done():
			log.Printf("[orchestrator] context cancelled at iteration %d", state.Iterations)
			return state.ToResult(), ctx.Err()
		default:
		}

		state.IncrementIteration()
		if hasIterationLimit {
			log.Printf("[orchestrator] === iteration %d/%d ===", state.Iterations, maxIterations)
		} else {
			log.Printf("[orchestrator] === iteration %d/unbounded ===", state.Iterations)
		}

		transformPlugins := buildTransformPlugins(req, state, compactor, maxMessages)
		contextMessages, err := runTransformPlugins(ctx, state.Messages, transformPlugins)
		if err != nil {
			return state.ToResult(), fmt.Errorf("transform context failed: %w", err)
		}

		// Convert agent-context messages into provider-ready LLM messages.
		llmMessages := defaultConvertToLlm(contextMessages)
		if req.ConvertToLlm != nil {
			converted, err := req.ConvertToLlm(ctx, contextMessages, l.Provider.Name())
			if err != nil {
				return state.ToResult(), fmt.Errorf("convert to llm failed: %w", err)
			}
			llmMessages = converted
		}

		// Build request
		agentReq := llm.AgentRequest{
			System:   systemPrompt,
			Messages: llmMessages,
			Tools:    toolDefs,
		}
		log.Printf("[orchestrator] sending request: messages=%d tools=%d", len(llmMessages), len(toolDefs))

		// Call the agent
		resp, err := l.callProvider(ctx, agentReq, req.EnableStreaming, req.OnStreamDelta)
		if err != nil {
			log.Printf("[orchestrator] ERROR: agent call failed: %v", err)
			return state.ToResult(), fmt.Errorf("agent call failed: %w", err)
		}

		log.Printf("[orchestrator] response: stop_reason=%s content_blocks=%d usage={in:%d out:%d}",
			resp.StopReason, len(resp.Content), resp.Usage.InputTokens, resp.Usage.OutputTokens)

		// Update usage stats
		state.UpdateUsage(resp.Usage)
		state.LastResponse = resp

		// Ensure all tool_use IDs are unique across the entire conversation.
		// Some LLM APIs (e.g., Kimi K2.5) may return empty IDs or reuse IDs
		// across different calls, which breaks tool_use/tool_result pairing
		// when message truncation removes one occurrence but keeps another.
		for i := range resp.Content {
			if resp.Content[i].Type == llm.ContentTypeToolUse {
				origID := resp.Content[i].ID
				if origID == "" || seenToolUseIDs[origID] {
					newID := generateToolUseID()
					if origID == "" {
						log.Printf("[orchestrator] generated ID %s for tool %s (API returned empty ID)",
							newID, resp.Content[i].Name)
					} else {
						log.Printf("[orchestrator] replaced duplicate ID %s -> %s for tool %s",
							origID, newID, resp.Content[i].Name)
					}
					resp.Content[i].ID = newID
				}
				seenToolUseIDs[resp.Content[i].ID] = true
			}
		}

		// Add assistant message to history (now with fixed IDs)
		assistantMsg := resp.ToMessage()
		state.AddMessage(assistantMsg)

		// Log response content
		text := resp.GetText()
		if len(text) > 500 {
			log.Printf("[orchestrator] response text (truncated): %s...", text[:500])
		} else if text != "" {
			log.Printf("[orchestrator] response text: %s", text)
		}

		// Notify callback
		if req.OnMessage != nil {
			req.OnMessage(assistantMsg)
		}

		if resp.StopReason == llm.StopReasonEndTurn {
			// TS-like runtime loop input injection point.
			steering, followUp := l.fetchLoopInputs(ctx, state, req)
			if len(steering) > 0 || len(followUp) > 0 {
				l.applyLoopInputs(state, req, steering, followUp)
				continue
			}
			log.Printf("[orchestrator] agent completed (end_turn) after %d iterations", state.Iterations)
			return state.ToResult(), nil
		}

		if resp.StopReason == llm.StopReasonMaxTokens {
			log.Printf("[orchestrator] ERROR: max tokens reached at iteration %d", state.Iterations)
			return state.ToResult(), errors.New("max tokens reached")
		}

		// Handle tool calls
		if resp.StopReason == llm.StopReasonToolUse || resp.HasToolUse() {
			toolUses := resp.GetToolUses()
			log.Printf("[orchestrator] executing %d tool(s)", len(toolUses))

			toolResults, steering, followUp, interrupted, err := l.executeTools(ctx, toolCtx, toolUses, req, state)
			if err != nil {
				log.Printf("[orchestrator] ERROR: tool execution failed: %v", err)
				return state.ToResult(), fmt.Errorf("tool execution failed: %w", err)
			}

			// Add tool results to state
			for _, tr := range toolResults {
				state.AddToolCall(tr.Name, tr.Input, tr.Result)
				resultPreview := tr.Result.Content
				if len(resultPreview) > 200 {
					resultPreview = resultPreview[:200] + "..."
				}
				log.Printf("[orchestrator] tool result: %s -> is_error=%v content=%s",
					tr.Name, tr.Result.IsError, resultPreview)
			}

			// Build tool result message
			resultMsg := buildToolResultMessage(toolResults)
			state.AddMessage(resultMsg)
			if interrupted {
				l.applyLoopInputs(state, req, steering, followUp)
				continue
			}
		} else {
			log.Printf("[orchestrator] WARNING: unexpected stop_reason=%s, no tool_use", resp.StopReason)
		}
	}

	if !hasIterationLimit {
		// Defensive fallback: the loop should only terminate via returns above.
		return state.ToResult(), nil
	}

	log.Printf("[orchestrator] ERROR: max iterations (%d) reached", maxIterations)
	return state.ToResult(), fmt.Errorf("max iterations (%d) reached", maxIterations)
}

// executeTools runs all tool use blocks and returns results.
func (l *AgentLoop) executeTools(
	ctx context.Context,
	toolCtx *tools.ToolContext,
	uses []llm.ContentBlock,
	req OrchestratorRequest,
	state *State,
) ([]toolExecResult, []llm.Message, []llm.Message, bool, error) {
	results := make([]toolExecResult, 0, len(uses))
	var pendingSteering []llm.Message
	var pendingFollowUp []llm.Message

	for _, use := range uses {
		log.Printf("[orchestrator] calling tool: %s id=%s input=%v", use.Name, use.ID, use.Input)

		if err := ensureToolAllowedByActiveSkill(toolCtx, use.Name); err != nil {
			log.Printf("[orchestrator] skill-allowlist blocked tool %s: %v", use.Name, err)
			result := tools.NewErrorResult(err)
			results = append(results, toolExecResult{
				ID:     use.ID,
				Name:   use.Name,
				Input:  use.Input,
				Result: result,
			})
			if req.OnToolResult != nil {
				req.OnToolResult(use.Name, result)
			}
			steering, followUp := l.fetchLoopInputs(ctx, state, req)
			if len(steering) > 0 || len(followUp) > 0 {
				pendingSteering = steering
				pendingFollowUp = followUp
				return results, pendingSteering, pendingFollowUp, true, nil
			}
			continue
		}

		// Notify callback
		if req.OnToolCall != nil {
			req.OnToolCall(use.Name, use.Input)
		}

		// Find and execute the tool
		tool := l.Registry.Get(use.Name)
		var result tools.ToolResult
		if tool == nil {
			log.Printf("[orchestrator] ERROR: tool not found: %s", use.Name)
			result = tools.NewErrorResultf("tool not found: %s", use.Name)
		} else {
			var err error
			result, err = tool.Execute(ctx, toolCtx, use.Input)
			if err != nil {
				log.Printf("[orchestrator] ERROR: tool %s execution error: %v", use.Name, err)
				result = tools.NewErrorResult(err)
			}
		}

		// Notify callback
		if req.OnToolResult != nil {
			req.OnToolResult(use.Name, result)
		}

		results = append(results, toolExecResult{
			ID:     use.ID,
			Name:   use.Name,
			Input:  use.Input,
			Result: result,
		})

		steering, followUp := l.fetchLoopInputs(ctx, state, req)
		if len(steering) > 0 || len(followUp) > 0 {
			pendingSteering = steering
			pendingFollowUp = followUp
			return results, pendingSteering, pendingFollowUp, true, nil
		}
	}

	return results, pendingSteering, pendingFollowUp, false, nil
}

func (l *AgentLoop) callProvider(
	ctx context.Context,
	req llm.AgentRequest,
	enableStreaming bool,
	onDelta func(llm.ContentBlockDelta),
) (llm.AgentResponse, error) {
	if enableStreaming {
		if provider, ok := l.Provider.(llm.StreamingProvider); ok {
			return provider.Stream(ctx, req, onDelta)
		}
	}
	return l.Provider.Call(ctx, req)
}

func (l *AgentLoop) fetchLoopInputs(ctx context.Context, state *State, req OrchestratorRequest) ([]llm.Message, []llm.Message) {
	snapshot := LoopInputSnapshot{
		Iteration:      state.Iterations,
		MessageCount:   len(state.Messages),
		ToolCallCount:  len(state.ToolCalls),
		LastStopReason: state.LastResponse.StopReason,
	}

	var steering []llm.Message
	var followUp []llm.Message
	if req.GetSteeringMessages != nil {
		messages, err := req.GetSteeringMessages(ctx, snapshot)
		if err != nil {
			log.Printf("[orchestrator] WARNING: steering provider failed: %v", err)
		} else {
			steering = normalizeLoopInputMessages(messages)
		}
	}

	if req.GetFollowUpMessages != nil {
		messages, err := req.GetFollowUpMessages(ctx, snapshot)
		if err != nil {
			log.Printf("[orchestrator] WARNING: follow-up provider failed: %v", err)
		} else {
			followUp = normalizeLoopInputMessages(messages)
		}
	}

	return steering, followUp
}

func normalizeLoopInputMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	normalized := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Content) == 0 {
			continue
		}
		if msg.Role == "" {
			msg.Role = llm.RoleUser
		}
		normalized = append(normalized, msg)
	}
	return normalized
}

func (l *AgentLoop) applyLoopInputs(
	state *State,
	req OrchestratorRequest,
	steering []llm.Message,
	followUp []llm.Message,
) {
	if len(steering) > 0 {
		for _, msg := range steering {
			state.AddMessage(msg)
		}
		if req.OnSteeringApplied != nil {
			req.OnSteeringApplied(steering)
		}
		log.Printf("[orchestrator] applied %d steering message(s)", len(steering))
	}

	if len(followUp) > 0 {
		for _, msg := range followUp {
			state.AddMessage(msg)
		}
		if req.OnFollowUpApplied != nil {
			req.OnFollowUpApplied(followUp)
		}
		log.Printf("[orchestrator] applied %d follow-up message(s)", len(followUp))
	}
}

type toolExecResult struct {
	ID     string
	Name   string
	Input  map[string]any
	Result tools.ToolResult
}

// buildToolResultMessage creates a message with all tool results.
func buildToolResultMessage(results []toolExecResult) llm.Message {
	content := make([]llm.ContentBlock, len(results))
	for i, r := range results {
		if r.ID == "" {
			log.Printf("[orchestrator] WARNING: tool %s has empty ID, this may cause API errors", r.Name)
		}
		content[i] = llm.ContentBlock{
			Type:      llm.ContentTypeToolResult,
			ToolUseID: r.ID,
			Content:   r.Result.Content,
			IsError:   r.Result.IsError,
		}
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: content,
	}
}

// buildSystemPrompt combines the base system prompt with SOUL and repo instructions.
func buildSystemPrompt(base, soulContent, repoInstructions string) string {
	parts := []string{}

	base = strings.TrimSpace(base)
	if base != "" {
		parts = append(parts, base)
	}

	soulContent = strings.TrimSpace(soulContent)
	if soulContent != "" {
		parts = append(parts, strings.Join([]string{
			"## Soul",
			"",
			"The following defines your character, personality, and behavioral directives.",
			"Follow these directives throughout the conversation.",
			"",
			soulContent,
		}, "\n"))
	}

	repoInstructions = strings.TrimSpace(repoInstructions)
	if repoInstructions != "" {
		parts = append(parts, strings.Join([]string{
			"## Repository Instructions",
			"",
			"The sections below are ordered from repository root to current directory.",
			"More specific instructions should override broader ones.",
			"",
			repoInstructions,
		}, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// readSoulContent loads the SOUL file content.
func readSoulContent(workDir, soulFile string) string {
	opts := soul.LoadOptions{
		File: soulFile,
	}
	result := soul.Load(workDir, opts)

	if result.Content != "" {
		log.Printf("[orchestrator] loaded SOUL from %s (%d bytes)%s",
			result.Source, len(result.Content), truncatedSuffix(result.Truncated))
	}

	return result.Content
}

// readRepoInstructions loads repository instructions from repo root to workDir.
// If instructionFiles is non-empty, those file names are used as candidates;
// otherwise the default candidate list from the instructions package is used.
func readRepoInstructions(workDir string, instructionFiles []string) string {
	opts := instructions.LoadOptions{
		MaxBytes: instructions.DefaultMaxBytes,
	}
	if len(instructionFiles) > 0 {
		opts.CandidateFiles = instructionFiles
	}
	result := instructions.Load(workDir, opts)

	combined := strings.TrimSpace(result.Content)
	if combined != "" {
		log.Printf("[orchestrator] loaded repo instructions from %d file(s): %s (%d bytes)%s",
			len(result.Sources),
			strings.Join(result.Sources, ", "),
			len(result.Content),
			truncatedSuffix(result.Truncated))
	} else {
		log.Printf("[orchestrator] no repository instructions found in %s", workDir)
	}

	skillBlock, skillCount, skillTruncated := buildSkillMetadata(workDir)
	if strings.TrimSpace(skillBlock) != "" {
		if combined != "" {
			combined += "\n\n" + skillBlock
		} else {
			combined = skillBlock
		}
		log.Printf("[orchestrator] loaded %d skill metadata entries%s",
			skillCount,
			truncatedSuffix(skillTruncated))
	} else {
		log.Printf("[orchestrator] no discoverable skills found for workdir=%s", workDir)
	}

	return combined
}

func truncatedSuffix(truncated bool) string {
	if truncated {
		return " [truncated]"
	}
	return ""
}

func buildSkillMetadata(workDir string) (content string, count int, truncated bool) {
	searchDirs := skills.DefaultSearchDirs(workDir)
	discovered, err := skills.Discover(searchDirs)
	if err != nil {
		log.Printf("[orchestrator] failed to discover skills for workdir=%s: %v", workDir, err)
		return "", 0, false
	}
	logSkillDiscoveryByDir(searchDirs, discovered)
	if len(discovered) == 0 {
		return "", 0, false
	}
	block := skills.BuildPromptBlock(discovered, skills.DefaultPromptBlockMaxBytes)
	return block.Content, block.SkillCount, block.Truncated
}

func applySlashSkillInvocation(state *State, toolCtx *tools.ToolContext, workDir string) (bool, error) {
	if state == nil || len(state.Messages) == 0 {
		return false, nil
	}
	initial := state.Messages[0]
	if initial.Role != llm.RoleUser {
		return false, nil
	}
	name, arguments, ok := skills.ParseSlashSkillCommand(initial.GetText())
	if !ok {
		return false, nil
	}

	discovered, err := skills.Discover(skills.DefaultSearchDirs(workDir))
	if err != nil {
		return false, err
	}
	if len(discovered) == 0 {
		return false, nil
	}
	selected, err := skills.ResolveForInvocation(discovered, name)
	if err != nil {
		// Unknown slash command is not an error; leave message unchanged.
		return false, nil
	}
	if !selected.UserInvocable {
		return false, fmt.Errorf("skill %q has user-invocable=false", selected.Name)
	}
	log.Printf(
		"[orchestrator] slash-skill invocation resolved: skill=%s scope=%s path=%s args=%q",
		selected.Name,
		selected.Scope,
		filepath.ToSlash(selected.Path),
		strings.TrimSpace(arguments),
	)

	sessionID := ""
	if toolCtx != nil && toolCtx.Env != nil {
		sessionID = strings.TrimSpace(toolCtx.Env[skills.EnvClaudeSessionID])
	}
	rendered, truncated, err := skills.RenderForInvocation(selected, arguments, sessionID, skills.DefaultSkillReadMaxBytes)
	if err != nil {
		return false, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "User invoked /%s\n", name)
	if strings.TrimSpace(arguments) != "" {
		fmt.Fprintf(&b, "Arguments: %s\n", strings.TrimSpace(arguments))
	}
	b.WriteString("\n")
	b.WriteString(rendered)
	if truncated {
		fmt.Fprintf(&b, "\n\n[truncated to %d bytes]", skills.DefaultSkillReadMaxBytes)
	}
	state.Messages[0] = llm.NewTextMessage(llm.RoleUser, strings.TrimSpace(b.String()))

	if toolCtx != nil {
		toolCtx.WithEnv(skills.EnvActiveSkillName, selected.Name)
		toolCtx.WithEnv(skills.EnvActiveSkillPath, selected.Path)
		if len(selected.AllowedTools) > 0 {
			toolCtx.WithEnv(skills.EnvActiveSkillAllowedTools, skills.JoinAllowedToolsEnv(selected.AllowedTools))
		} else if toolCtx.Env != nil {
			delete(toolCtx.Env, skills.EnvActiveSkillAllowedTools)
		}
	}

	return true, nil
}

const unmatchedSkillDirLabel = "<unmatched>"

type skillDiscoveryLogEntry struct {
	Dir    string
	Skills []skills.Skill
}

func logSkillDiscoveryByDir(searchDirs []string, discovered []skills.Skill) {
	entries := summarizeSkillDiscoveryByDir(searchDirs, discovered)
	if len(entries) == 0 {
		log.Printf("[orchestrator] skill discovery paths: none")
		return
	}

	for _, entry := range entries {
		log.Printf(
			"[orchestrator] skills loaded from %s: count=%d list=%s",
			filepath.ToSlash(entry.Dir),
			len(entry.Skills),
			formatSkillListForLog(entry.Skills),
		)
	}
}

func summarizeSkillDiscoveryByDir(searchDirs []string, discovered []skills.Skill) []skillDiscoveryLogEntry {
	dirs := uniqueNonEmptyPaths(searchDirs)
	if len(dirs) == 0 && len(discovered) == 0 {
		return nil
	}

	grouped := make(map[string][]skills.Skill, len(dirs)+1)
	for _, dir := range dirs {
		grouped[dir] = nil
	}

	for _, skill := range discovered {
		matched := false
		for _, dir := range dirs {
			if isPathWithinDir(skill.Path, dir) {
				grouped[dir] = append(grouped[dir], skill)
				matched = true
				break
			}
		}
		if !matched {
			grouped[unmatchedSkillDirLabel] = append(grouped[unmatchedSkillDirLabel], skill)
		}
	}

	entries := make([]skillDiscoveryLogEntry, 0, len(dirs)+1)
	for _, dir := range dirs {
		entries = append(entries, skillDiscoveryLogEntry{
			Dir:    dir,
			Skills: sortSkillsForLog(grouped[dir]),
		})
	}
	if unmatched := sortSkillsForLog(grouped[unmatchedSkillDirLabel]); len(unmatched) > 0 {
		entries = append(entries, skillDiscoveryLogEntry{
			Dir:    unmatchedSkillDirLabel,
			Skills: unmatched,
		})
	}
	return entries
}

func uniqueNonEmptyPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func isPathWithinDir(path, dir string) bool {
	path = strings.TrimSpace(path)
	dir = strings.TrimSpace(dir)
	if path == "" || dir == "" {
		return false
	}

	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sortSkillsForLog(in []skills.Skill) []skills.Skill {
	out := append([]skills.Skill(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func formatSkillListForLog(skillList []skills.Skill) string {
	if len(skillList) == 0 {
		return "[]"
	}
	items := make([]string, 0, len(skillList))
	for _, skill := range skillList {
		items = append(items, fmt.Sprintf("%s(%s)", skill.Name, filepath.ToSlash(skill.Path)))
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func ensureToolAllowedByActiveSkill(toolCtx *tools.ToolContext, toolName string) error {
	if toolCtx == nil || toolCtx.Env == nil {
		return nil
	}
	// Allow reloading/switching skills even under a restrictive skill allowlist.
	if toolName == "use_skill" {
		return nil
	}

	allowedRaw := strings.TrimSpace(toolCtx.Env[skills.EnvActiveSkillAllowedTools])
	if allowedRaw == "" {
		return nil
	}
	allowed := skills.ParseAllowedToolsEnv(allowedRaw)
	if skills.IsToolAllowed(toolName, allowed) {
		return nil
	}

	skillName := strings.TrimSpace(toolCtx.Env[skills.EnvActiveSkillName])
	if skillName == "" {
		skillName = "active skill"
	}
	return fmt.Errorf(
		"tool %q is blocked by skill %q allowed-tools policy (%s)",
		toolName,
		skillName,
		strings.Join(allowed, ", "),
	)
}

// truncateMessages truncates message history while preserving tool_use/tool_result pairs.
// It keeps the first message (initial prompt) and the most recent messages.
// Uses fixed-point iteration to ensure all dependencies are resolved.
func truncateMessages(messages []llm.Message, maxMessages int) []llm.Message {
	if len(messages) <= maxMessages {
		return messages
	}

	// Start from the ideal cut point
	keepFrom := len(messages) - maxMessages + 1 // +1 because we keep the first message
	if keepFrom < 1 {
		keepFrom = 1
	}

	// Helper function to collect tool_use IDs from a range of messages
	// includeFirst indicates whether to also include messages[0]
	collectToolUseIDs := func(from int, includeFirst bool) map[string]bool {
		ids := make(map[string]bool)
		// Always include tool_uses from messages[0] if requested (it's always kept)
		if includeFirst {
			for _, block := range messages[0].Content {
				if block.Type == llm.ContentTypeToolUse && block.ID != "" {
					ids[block.ID] = true
				}
			}
		}
		for i := from; i < len(messages); i++ {
			for _, block := range messages[i].Content {
				if block.Type == llm.ContentTypeToolUse && block.ID != "" {
					ids[block.ID] = true
				}
			}
		}
		return ids
	}

	// Fixed-point iteration: keep expanding keepFrom until all tool pairs are preserved
	for iteration := 0; iteration < 100; iteration++ { // Safety limit
		changed := false

		// Collect all tool_use IDs from messages we want to keep (including messages[0])
		toolUseIDs := collectToolUseIDs(keepFrom, true)

		// Check if any tool_result references a tool_use that would be truncated
		for i := keepFrom; i < len(messages); i++ {
			for _, block := range messages[i].Content {
				if block.Type == llm.ContentTypeToolResult && block.ToolUseID != "" {
					if !toolUseIDs[block.ToolUseID] {
						// Find and include the message with this tool_use
						for j := keepFrom - 1; j >= 1; j-- {
							for _, b := range messages[j].Content {
								if b.Type == llm.ContentTypeToolUse && b.ID == block.ToolUseID {
									log.Printf("[orchestrator] truncation: including msg %d for tool_use %s (needed by tool_result in msg %d)",
										j, block.ToolUseID, i)
									keepFrom = j
									changed = true
									break
								}
							}
							if changed {
								break
							}
						}
					}
				}
				if changed {
					break
				}
			}
			if changed {
				break
			}
		}

		if !changed {
			break
		}
	}

	// Final validation: ensure no orphaned tool_results
	toolUseIDs := collectToolUseIDs(keepFrom, true)

	// Check for orphaned tool_results and tool_results with empty IDs
	hasOrphans := false
	for i := keepFrom; i < len(messages); i++ {
		for _, block := range messages[i].Content {
			if block.Type == llm.ContentTypeToolResult {
				if block.ToolUseID == "" {
					log.Printf("[orchestrator] WARNING: tool_result at msg %d has empty tool_use_id", i)
					hasOrphans = true
				} else if !toolUseIDs[block.ToolUseID] {
					log.Printf("[orchestrator] WARNING: orphaned tool_result at msg %d, tool_use_id=%s not found",
						i, block.ToolUseID)
					hasOrphans = true
				}
			}
		}
	}
	// Also check messages[0] for tool_results with issues
	for _, block := range messages[0].Content {
		if block.Type == llm.ContentTypeToolResult {
			if block.ToolUseID == "" {
				log.Printf("[orchestrator] WARNING: tool_result at msg 0 has empty tool_use_id")
				hasOrphans = true
			} else if !toolUseIDs[block.ToolUseID] {
				log.Printf("[orchestrator] WARNING: orphaned tool_result at msg 0, tool_use_id=%s not found",
					block.ToolUseID)
				hasOrphans = true
			}
		}
	}

	if hasOrphans {
		log.Printf("[orchestrator] WARNING: truncation resulted in orphaned tool_results, this may cause API errors")
	}

	// Build the truncated message list
	result := make([]llm.Message, 0, len(messages)-keepFrom+1)
	result = append(result, messages[0]) // Always keep first message
	result = append(result, messages[keepFrom:]...)

	truncated := len(messages) - len(result)
	log.Printf("[orchestrator] truncating message history: %d -> %d messages (removed %d)",
		len(messages), len(result), truncated)

	return result
}
