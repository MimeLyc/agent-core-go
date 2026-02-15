package orchestrator

import (
	"context"
	"fmt"
	"log"
)

type contextTransformPlugin struct {
	name string
	run  func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error)
}

func buildTransformPlugins(
	req OrchestratorRequest,
	state *State,
	compactor *Compactor,
	maxMessages int,
) []contextTransformPlugin {
	plugins := make([]contextTransformPlugin, 0, 4)

	if req.TransformContext != nil {
		plugins = append(plugins, contextTransformPlugin{
			name: "user_transform_context",
			run: func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error) {
				return req.TransformContext(ctx, messages)
			},
		})
	}

	if req.DisableDefaultContextRules {
		return plugins
	}

	if compactor != nil {
		plugins = append(plugins, contextTransformPlugin{
			name: "compact_context",
			run: func(ctx context.Context, messages []AgentMessage) ([]AgentMessage, error) {
				if !compactor.ShouldCompact(messages) {
					return messages, nil
				}

				log.Printf("[orchestrator] triggering compaction: %d messages exceed threshold %d",
					len(messages), req.CompactConfig.Threshold)
				compactedMessages, err := compactor.Compact(ctx, messages)
				if err != nil {
					log.Printf("[orchestrator] WARNING: compaction failed: %v, falling back to truncation", err)
					return messages, nil
				}
				// Compaction must persist to state for subsequent turns.
				state.Messages = compactedMessages
				log.Printf("[orchestrator] compaction succeeded: reduced to %d messages", len(compactedMessages))
				return compactedMessages, nil
			},
		})
	}

	plugins = append(plugins, contextTransformPlugin{
		name: "truncate_context",
		run: func(_ context.Context, messages []AgentMessage) ([]AgentMessage, error) {
			if len(messages) <= maxMessages {
				return messages, nil
			}
			return truncateMessages(messages, maxMessages), nil
		},
	})

	plugins = append(plugins, contextTransformPlugin{
		name: "validate_tool_pairs",
		run: func(_ context.Context, messages []AgentMessage) ([]AgentMessage, error) {
			if err := validateToolPairs(messages); err != nil {
				log.Printf("[orchestrator] ERROR: message validation failed: %v", err)
				// Preserve historical behavior: fall back to full history.
				fallback := append([]AgentMessage(nil), state.Messages...)
				log.Printf("[orchestrator] falling back to full message history: %d messages", len(fallback))
				return fallback, nil
			}
			return messages, nil
		},
	})

	return plugins
}

func runTransformPlugins(
	ctx context.Context,
	messages []AgentMessage,
	plugins []contextTransformPlugin,
) ([]AgentMessage, error) {
	current := append([]AgentMessage(nil), messages...)
	for _, plugin := range plugins {
		next, err := plugin.run(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", plugin.name, err)
		}
		current = next
	}
	return current, nil
}

func defaultConvertToLlm(messages []AgentMessage) []LLMMessage {
	if len(messages) == 0 {
		return nil
	}
	return append([]LLMMessage(nil), messages...)
}
