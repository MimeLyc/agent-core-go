package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/instructions"
	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/skills"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

const (
	defaultMaxIterations = 50
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
	systemPrompt := buildSystemPrompt(req.SystemPrompt, repoInstructions)
	log.Printf("[orchestrator] system prompt length: %d chars", len(systemPrompt))

	// Set max iterations
	maxIterations := req.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}

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
	for state.Iterations < maxIterations {
		select {
		case <-ctx.Done():
			log.Printf("[orchestrator] context cancelled at iteration %d", state.Iterations)
			return state.ToResult(), ctx.Err()
		default:
		}

		state.IncrementIteration()
		log.Printf("[orchestrator] === iteration %d/%d ===", state.Iterations, maxIterations)

		// Handle message history management
		messages := state.Messages

		// Try compaction if enabled and messages exceed threshold
		if compactor != nil && compactor.ShouldCompact(messages) {
			log.Printf("[orchestrator] triggering compaction: %d messages exceed threshold %d",
				len(messages), req.CompactConfig.Threshold)
			compactedMessages, err := compactor.Compact(ctx, messages)
			if err != nil {
				log.Printf("[orchestrator] WARNING: compaction failed: %v, falling back to truncation", err)
			} else {
				messages = compactedMessages
				// Update state with compacted messages
				state.Messages = messages
				log.Printf("[orchestrator] compaction succeeded: reduced to %d messages", len(messages))
			}
		}

		// Fall back to truncation if still too long
		if len(messages) > maxMessages {
			messages = truncateMessages(messages, maxMessages)
		}

		// Validate messages before sending - check for orphaned tool_results
		if err := validateToolPairs(messages); err != nil {
			log.Printf("[orchestrator] ERROR: message validation failed: %v", err)
			// Try to recover by not truncating
			messages = state.Messages
			log.Printf("[orchestrator] falling back to full message history: %d messages", len(messages))
		}

		// Build request
		agentReq := llm.AgentRequest{
			System:   systemPrompt,
			Messages: messages,
			Tools:    toolDefs,
		}
		log.Printf("[orchestrator] sending request: messages=%d tools=%d", len(messages), len(toolDefs))

		// Call the agent
		resp, err := l.Provider.Call(ctx, agentReq)
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

		// Check if we should stop
		if resp.StopReason == llm.StopReasonEndTurn {
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

			toolResults, err := l.executeTools(ctx, toolCtx, toolUses, req)
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
		} else {
			log.Printf("[orchestrator] WARNING: unexpected stop_reason=%s, no tool_use", resp.StopReason)
		}
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
) ([]toolExecResult, error) {
	results := make([]toolExecResult, 0, len(uses))

	for _, use := range uses {
		log.Printf("[orchestrator] calling tool: %s id=%s input=%v", use.Name, use.ID, use.Input)

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
	}

	return results, nil
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

// buildSystemPrompt combines the base system prompt with repo instructions.
func buildSystemPrompt(base, repoInstructions string) string {
	parts := []string{}

	base = strings.TrimSpace(base)
	if base != "" {
		parts = append(parts, base)
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
	return strings.Join(parts, "\n\n")
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
	if err != nil || len(discovered) == 0 {
		return "", 0, false
	}
	block := skills.BuildPromptBlock(discovered, skills.DefaultPromptBlockMaxBytes)
	return block.Content, block.SkillCount, block.Truncated
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
