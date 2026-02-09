# agent-core-go

A reusable Go SDK for agent-loop orchestration, LLM provider abstraction, tool execution, instruction loading, and skill discovery.

This repository extracts the generic AI core from `git_sonic` so other projects can integrate it as an SDK.

## What Is Included

- `pkg/agent`: unified agent interface, API-backed agent, CLI-backed agent, runner adapter.
- `pkg/orchestrator`: iterative agent loop, tool dispatch, context compaction, instruction composition.
- `pkg/llm`: provider abstraction and implementations (Claude API and OpenAI-compatible APIs).
- `pkg/tools`: tool contracts, registry, execution context, built-in tools.
- `pkg/instructions`: layered loading for `AGENT.md` / `AGENTS.md` / `CLAUDE.md`.
- `pkg/skills`: skill discovery and metadata rendering.
- `pkg/mcp`: MCP client/server protocol helpers.

## Installation

```bash
go get github.com/MimeLyc/agent-core-go
```

## Quick Start (Generic Task Context)

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools/builtin"
)

func main() {
	provider, err := llm.NewLLMProvider(llm.LLMProviderConfig{
		Type:           llm.ProviderOpenAI,
		BaseURL:        "https://api.openai.com",
		APIKey:         os.Getenv("OPENAI_API_KEY"),
		Model:          "gpt-4.1",
		MaxTokens:      4096,
		TimeoutSeconds: 300,
		MaxAttempts:    5,
	})
	if err != nil {
		panic(err)
	}

	registry := builtin.NewRegistryWithBuiltins() // default built-ins, no GitHub API tools
	a := agent.NewAPIAgent(provider, registry, agent.APIAgentOptions{
		MaxIterations: 30,
		MaxMessages:   60,
		MaxTokens:     4096,
	})

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		WorkDir: "/path/to/your/repo",
		Context: agent.AgentContext{
			RepoFullName: "acme/service",
			TaskID:       "TASK-123",
			TaskTitle:    "Move HTTP handlers to internal/controller",
			TaskBody:     "Refactor package layout and keep tests passing.",
			TaskLabels:   []string{"refactor", "architecture"},
			Metadata: map[string]string{
				"source": "project-tracker",
			},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Decision)
	fmt.Println(result.Summary)
}
```

## Optional GitHub/Webhook Extensions

The SDK is desensitized by default:
- Prompt building uses generic **Task Context** semantics.
- GitHub API tools are **not** registered by default.

If your integration needs GitHub issue/PR APIs, register them explicitly:

```go
registry := builtin.NewRegistryWithBuiltinsAndGitHub()
```

Or:

```go
registry := builtin.NewRegistryWithBuiltins()
builtin.RegisterAllWithGitHub(registry)
```

Then pass credentials via `tools.ToolContext.WithGitHub(token, owner, repo)` where needed.

## Backward Compatibility

Legacy issue/PR fields are still supported for existing webhook-driven pipelines (`IssueNumber`, `PRNumber`, etc.), but new integrations should prefer generic fields (`TaskID`, `TaskTitle`, `TaskBody`, `TaskLabels`, `TaskComments`, `Metadata`).

## Instruction Loading Behavior

If `RepoInstructions` is empty and `WorkDir` is set, the orchestrator can load layered repository instructions from root to current directory, preferring in-directory precedence:

1. `AGENT.md`
2. `AGENTS.md`
3. `CLAUDE.md`

More specific directory instructions override broader root-level guidance.
