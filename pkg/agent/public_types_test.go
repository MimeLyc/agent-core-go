package agent

import (
	"context"
	"testing"

	agenttypes "github.com/MimeLyc/agent-core-go/pkg/agent/types"
)

func TestAgentCallbacksUsePublicAgentTypes(t *testing.T) {
	req := AgentRequest{
		Callbacks: AgentCallbacks{
			OnMessage: func(msg agenttypes.Message) {},
			OnSteeringApplied: func(messages []agenttypes.Message) {
				_ = messages
			},
			OnFollowUpApplied: func(messages []agenttypes.Message) {
				_ = messages
			},
			OnStreamDelta: func(delta agenttypes.ContentBlockDelta) {
				_ = delta
			},
		},
		Options: AgentOptions{
			GetSteeringMessages: func(_ context.Context, _ LoopInputSnapshot) ([]agenttypes.Message, error) {
				return []agenttypes.Message{agenttypes.NewTextMessage(agenttypes.RoleUser, "steer")}, nil
			},
			GetFollowUpMessages: func(_ context.Context, _ LoopInputSnapshot) ([]agenttypes.Message, error) {
				return []agenttypes.Message{agenttypes.NewTextMessage(agenttypes.RoleUser, "follow")}, nil
			},
		},
	}
	if req.Callbacks.OnMessage == nil {
		t.Fatalf("on message callback should be set")
	}
}
