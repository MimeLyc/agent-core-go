package orchestrator

import (
	"context"
	"reflect"
	"testing"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
)

func TestBuildTransformPluginsIncludesBuiltins(t *testing.T) {
	state := NewState([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "hello"),
	})
	req := OrchestratorRequest{
		TransformContext: func(_ context.Context, messages []AgentMessage) ([]AgentMessage, error) {
			return messages, nil
		},
		CompactConfig: CompactConfig{
			Enabled:    true,
			Threshold:  1,
			KeepRecent: 1,
		},
	}
	compactor := &Compactor{config: req.CompactConfig}

	plugins := buildTransformPlugins(req, state, compactor, 20)
	var names []string
	for _, plugin := range plugins {
		names = append(names, plugin.name)
	}

	want := []string{
		"user_transform_context",
		"compact_context",
		"truncate_context",
		"validate_tool_pairs",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("plugin names = %v, want %v", names, want)
	}
}

func TestBuildTransformPluginsCanDisableBuiltins(t *testing.T) {
	state := NewState([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "hello"),
	})
	req := OrchestratorRequest{
		TransformContext: func(_ context.Context, messages []AgentMessage) ([]AgentMessage, error) {
			return messages, nil
		},
		DisableDefaultContextRules: true,
	}

	plugins := buildTransformPlugins(req, state, nil, 20)
	if len(plugins) != 1 {
		t.Fatalf("plugin count = %d, want 1", len(plugins))
	}
	if plugins[0].name != "user_transform_context" {
		t.Fatalf("plugin name = %q, want %q", plugins[0].name, "user_transform_context")
	}
}
