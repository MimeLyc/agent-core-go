package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

// CompactConfig holds configuration for context compaction.
type CompactConfig struct {
	Enabled    bool
	Threshold  int // Trigger compact when messages exceed this
	KeepRecent int // Keep this many recent messages after compact
}

// DefaultCompactConfig returns sensible defaults for compaction.
func DefaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:    true,
		Threshold:  30,
		KeepRecent: 10,
	}
}

// compactSummaryPrompt is the system prompt for generating conversation summaries.
const compactSummaryPrompt = `You are a conversation summarizer. Your task is to create a concise but comprehensive summary of the conversation history that preserves all important context for continuing the task.

Your summary MUST include:
1. **Original Task**: What was the user's initial request/goal?
2. **Key Decisions**: Important decisions made during the conversation
3. **Files Modified**: List of files that were read, created, or modified with brief descriptions of changes
4. **Current State**: What has been accomplished so far?
5. **Pending Work**: What still needs to be done?
6. **Important Context**: Any critical information needed to continue (error messages, specific requirements, etc.)

Format your summary as a structured document. Be concise but don't omit important details.
Do NOT include tool call details or raw outputs - just summarize the key information.`

// Compactor handles conversation context compaction.
type Compactor struct {
	provider llm.LLMProvider
	config   CompactConfig
}

// NewCompactor creates a new Compactor.
// The provider parameter accepts any LLMProvider implementation.
func NewCompactor(provider llm.LLMProvider, config CompactConfig) *Compactor {
	return &Compactor{
		provider: provider,
		config:   config,
	}
}

// ShouldCompact returns true if the conversation should be compacted.
func (c *Compactor) ShouldCompact(messages []llm.Message) bool {
	if !c.config.Enabled {
		return false
	}
	return len(messages) > c.config.Threshold
}

// Compact summarizes the conversation and returns a compacted message list.
// It keeps the first message (initial prompt), generates a summary of the middle,
// and keeps the most recent messages.
func (c *Compactor) Compact(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
	if len(messages) <= c.config.KeepRecent+1 {
		// Not enough messages to compact
		return messages, nil
	}

	log.Printf("[compact] starting compaction: %d messages, threshold=%d, keep_recent=%d",
		len(messages), c.config.Threshold, c.config.KeepRecent)

	// Determine which messages to summarize
	// Keep: first message (index 0) + last KeepRecent messages
	summarizeEnd := len(messages) - c.config.KeepRecent
	if summarizeEnd <= 1 {
		// Nothing to summarize
		return messages, nil
	}

	// Build the conversation text to summarize
	messagesToSummarize := messages[1:summarizeEnd]
	conversationText := formatMessagesForSummary(messagesToSummarize)

	log.Printf("[compact] summarizing %d messages (%d chars)", len(messagesToSummarize), len(conversationText))

	// Generate summary using the LLM
	summary, err := c.generateSummary(ctx, conversationText)
	if err != nil {
		log.Printf("[compact] ERROR: failed to generate summary: %v", err)
		// Fall back to simple truncation
		return truncateMessages(messages, c.config.KeepRecent+1), nil
	}

	log.Printf("[compact] generated summary: %d chars", len(summary))

	// Build the compacted message list
	result := make([]llm.Message, 0, c.config.KeepRecent+2)

	// First message (original prompt)
	result = append(result, messages[0])

	// Summary as an assistant message
	result = append(result, llm.Message{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{
				Type: llm.ContentTypeText,
				Text: fmt.Sprintf("[Conversation Summary - %d messages compacted]\n\n%s", len(messagesToSummarize), summary),
			},
		},
	})

	// Recent messages (need to ensure tool pairs are intact)
	recentMessages := messages[summarizeEnd:]
	recentMessages = ensureToolPairsIntact(recentMessages, messages[:summarizeEnd])
	result = append(result, recentMessages...)

	log.Printf("[compact] compaction complete: %d -> %d messages", len(messages), len(result))

	return result, nil
}

// generateSummary calls the LLM to generate a conversation summary.
func (c *Compactor) generateSummary(ctx context.Context, conversationText string) (string, error) {
	req := llm.AgentRequest{
		System: compactSummaryPrompt,
		Messages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, "Please summarize the following conversation:\n\n"+conversationText),
		},
		// No tools for summary generation
		Tools: nil,
	}

	resp, err := c.provider.Call(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summary generation failed: %w", err)
	}

	summary := resp.GetText()
	if summary == "" {
		return "", fmt.Errorf("summary generation returned empty response")
	}

	return summary, nil
}

// formatMessagesForSummary converts messages to a readable text format for summarization.
func formatMessagesForSummary(messages []llm.Message) string {
	var sb strings.Builder

	for i, msg := range messages {
		role := string(msg.Role)
		if role == "assistant" {
			role = "Assistant"
		} else if role == "user" {
			role = "User"
		}

		sb.WriteString(fmt.Sprintf("--- Message %d (%s) ---\n", i+1, role))

		for _, block := range msg.Content {
			switch block.Type {
			case llm.ContentTypeText:
				if block.Text != "" {
					sb.WriteString(block.Text)
					sb.WriteString("\n")
				}
			case llm.ContentTypeToolUse:
				sb.WriteString(fmt.Sprintf("[Tool Call: %s]\n", block.Name))
				// Don't include full input to keep summary manageable
			case llm.ContentTypeToolResult:
				content := block.Content
				if len(content) > 500 {
					content = content[:500] + "... (truncated)"
				}
				if block.IsError {
					sb.WriteString(fmt.Sprintf("[Tool Error: %s]\n", content))
				} else {
					sb.WriteString(fmt.Sprintf("[Tool Result: %s]\n", content))
				}
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ensureToolPairsIntact ensures that recent messages don't have orphaned tool_results.
// If a tool_result in recent messages references a tool_use from older messages,
// we need to include context about that tool call.
func ensureToolPairsIntact(recentMessages []llm.Message, olderMessages []llm.Message) []llm.Message {
	// Collect tool_use IDs from recent messages
	recentToolUseIDs := make(map[string]bool)
	for _, msg := range recentMessages {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse && block.ID != "" {
				recentToolUseIDs[block.ID] = true
			}
		}
	}

	// Check for orphaned tool_results
	orphanedResults := make(map[string]bool)
	for _, msg := range recentMessages {
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolResult && block.ToolUseID != "" {
				if !recentToolUseIDs[block.ToolUseID] {
					orphanedResults[block.ToolUseID] = true
				}
			}
		}
	}

	if len(orphanedResults) == 0 {
		return recentMessages
	}

	// Find the tool_uses from older messages and prepend them
	var toolUseMessages []llm.Message
	for _, msg := range olderMessages {
		hasNeededToolUse := false
		for _, block := range msg.Content {
			if block.Type == llm.ContentTypeToolUse && orphanedResults[block.ID] {
				hasNeededToolUse = true
				break
			}
		}
		if hasNeededToolUse {
			toolUseMessages = append(toolUseMessages, msg)
			// Also need the following tool_result message
		}
	}

	if len(toolUseMessages) == 0 {
		return recentMessages
	}

	log.Printf("[compact] including %d older messages to preserve tool pairs", len(toolUseMessages))

	// We need to include the tool_use messages and their results
	// This is complex, so for now just prepend the needed tool_use blocks as context
	result := make([]llm.Message, 0, len(toolUseMessages)+len(recentMessages))
	result = append(result, toolUseMessages...)
	result = append(result, recentMessages...)
	return result
}
