# agent-core-go

A reusable Go SDK for agent-loop orchestration, LLM provider abstraction, tool execution, instruction loading, and Claude-Code-style skills.

This repository provides a **generic, provider-agnostic** agent framework. It includes a standard skill loop (`list_skills` / `read_skill` / `use_skill`), slash-skill invocation support, and strict tool policy enforcement driven by skill metadata.

## What Is Included

- `pkg/agent`: unified agent interface, API-backed agent, CLI-backed agent, runner adapter.
- `pkg/orchestrator`: iterative agent loop, tool dispatch, context compaction, instruction composition.
- `pkg/llm`: provider abstraction and implementations (Claude API and OpenAI-compatible APIs).
- `pkg/tools`: tool contracts, registry, execution context, built-in tools.
- `pkg/instructions`: layered loading for `AGENT.md` / `AGENTS.md`.
- `pkg/skills`: skill discovery, precedence resolution, invocation rendering, and allow-policy matching.
- `pkg/mcp`: MCP client/server protocol helpers.

## Installation

```bash
go get github.com/MimeLyc/agent-core-go
```

## Quick Start

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
		Type:           llm.ProviderOpenAI,           // explicit provider type required
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

	registry := builtin.NewRegistryWithBuiltins()
	a := agent.NewAPIAgent(provider, registry, agent.APIAgentOptions{
		MaxIterations:    30,
		MaxMessages:      60,
		MaxTokens:        4096,
		MaxContextTokens: 128000,                      // provider context window size
		SystemPrompt:     "You are a helpful assistant.", // caller must provide
	})

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "List the files in the current directory and summarize the project structure.",
		WorkDir: "/path/to/your/repo",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Decision) // "proceed", "needs_info", or "stop"
	fmt.Println(result.Summary)
	fmt.Println(result.Message)
}
```

Run the included demo (`cmd/demo.go`):

```bash
export OPENAI_API_KEY=...
go run ./cmd
```

The demo uses API mode with built-in tools and prints decision/summary from the agent result.

## Agent Modes

The factory supports four modes through `agent.AgentConfig.Type`:

| Type | Behavior |
|------|----------|
| `api` | Runs local orchestrator + configured LLM provider (`claude` or `openai`) |
| `cli` | Runs an external CLI agent process |
| `claude-code` | Alias of `cli` (backward compatibility) |
| `auto` | Prefers API when API config is valid; otherwise falls back to CLI if command exists |

E2E coverage for all modes is in `tests/e2e/agent_modes_e2e_test.go`.

## Configurable Parameters

The SDK does not inject any opinions. All behavior is explicitly configured by the caller.

### LLM Provider (`llm.LLMProviderConfig`)

| Field | Description | Default |
|-------|-------------|---------|
| `Type` | Provider type (`"claude"`, `"openai"`) | `"claude"` when empty |
| `BaseURL` | API base URL | **required** |
| `APIKey` | API key | **required** |
| `Model` | Model identifier | **required** |
| `MaxTokens` | Max response tokens | 4096 |
| `TimeoutSeconds` | Request timeout | 300 (5min) |
| `MaxAttempts` | Retry count | 5 |

### API Agent (`agent.APIAgentOptions`)

| Field | Description | Default |
|-------|-------------|---------|
| `MaxIterations` | Max agent loop iterations (`<=0` means unbounded) | 0 |
| `MaxMessages` | Max conversation history size | 50 |
| `MaxTokens` | Max response token count | 4096 |
| `MaxContextTokens` | Context window size (reported in capabilities) | 0 (unknown) |
| `SystemPrompt` | System prompt for the agent | `""` (empty) |
| `CompactConfig` | Context compaction settings | nil (disabled) |
| `EnableStreaming` | Enable stream-capable execution paths | `false` |

### CLI Agent (`agent.CLIAgentConfig`)

| Field | Description | Default |
|-------|-------------|---------|
| `Command` | CLI binary path | **required** (no default) |
| `Args` | Additional CLI arguments | nil |
| `Timeout` | Execution timeout | 30min |
| `AllowedTools` | Tool allowlist | nil (all allowed) |

### Agent Factory (`agent.AgentConfig`)

| Field | Description |
|-------|-------------|
| `Type` | `"api"`, `"cli"`, `"claude-code"`, or `"auto"` |
| `API` | `*APIConfig` for API-based agents |
| `CLI` | `*CLIAgentConfig` for CLI-based agents |
| `Registry` | Tool registry |

### Orchestrator Request (`orchestrator.OrchestratorRequest`)

| Field | Description | Default |
|-------|-------------|---------|
| `SystemPrompt` | System message | `""` (empty) |
| `RepoInstructions` | Pre-loaded instruction content | `""` (auto-loaded if WorkDir set) |
| `InstructionFiles` | Override instruction file names | `["AGENT.md", "AGENTS.md"]` |
| `MaxIterations` | Loop iteration limit (`<=0` means unbounded) | 0 |
| `DisableIterationLimit` | Force unbounded loop regardless of MaxIterations | `false` |
| `MaxMessages` | History size limit | 50 |
| `CompactConfig` | Compaction settings | disabled |
| `EnableStreaming` | Use provider streaming when supported | `false` |

### Agent Request (`agent.AgentRequest`)

| Field | Description |
|-------|-------------|
| `Task` | The full user prompt (required) |
| `SystemPrompt` | System message override |
| `RepoInstructions` | Repository instruction content |
| `WorkDir` | Working directory for tools |
| `Context` | Structured context (`AgentContext`) |
| `Options` | Execution options (`AgentOptions`) |
| `Callbacks` | Monitoring hooks (`AgentCallbacks`) |

`AgentOptions` supports runtime loop input injection and streaming controls:

- `DisableIterationLimit`: request-level override to cancel iteration cap
- `EnableStreaming`: request-level stream switch
- `GetSteeringMessages`: high-priority runtime input fetcher (polled at loop checkpoints)
- `GetFollowUpMessages`: follow-up runtime input fetcher (after steering)

### Agent Context (`agent.AgentContext`)

Generic task metadata. The SDK does **not** build prompts from these fields — callers must compose the full prompt in `Task`.

| Field | Description |
|-------|-------------|
| `RepoFullName` | Repository identifier (e.g., `"owner/repo"`) |
| `RepoPath` | Local repository path |
| `TaskID` | External task identifier |
| `TaskTitle` | Task title |
| `TaskBody` | Task description |
| `TaskLabels` | Task labels/tags |
| `TaskComments` | Task comments (`[]TaskComment`) |
| `Metadata` | Arbitrary key-value pairs |
| `CommentBody` | Trigger comment content |
| `SlashCommand` | Trigger command |
| `Requirements` | Additional requirements |

### Agent Result (`agent.AgentResult`)

| Field | Description |
|-------|-------------|
| `Success` | Whether execution completed without error |
| `Decision` | `"proceed"`, `"needs_info"`, or `"stop"` |
| `Summary` | Brief description (raw final text from LLM) |
| `Message` | Detailed response (raw final text from LLM) |
| `FileChanges` | File modifications (`[]FileChange`) |
| `ToolCalls` | Tool invocation records (`[]ToolCallRecord`) |
| `Usage` | Token usage statistics (`ExecutionUsage`) |
| `RawOutput` | Complete conversation (`[]llm.Message`) |

## Instruction Loading

If `RepoInstructions` is empty and `WorkDir` is set, the orchestrator auto-loads layered instructions from repo root to working directory. Default candidate files:

1. `AGENT.md`
2. `AGENTS.md`

Override with `OrchestratorRequest.InstructionFiles`:

```go
orchReq := orchestrator.OrchestratorRequest{
    InstructionFiles: []string{"AGENT.md", "AGENTS.md", "CUSTOM.md"},
    // ...
}
```

More specific directory instructions override broader root-level guidance.

Skill metadata is appended to the same repository instruction block automatically (progressive disclosure format).

## Skills (Claude Code Equivalent)

Built-in skill tools are registered by default in `builtin.NewRegistryWithBuiltins()`:

- `list_skills`: discover metadata only (name/description/path)
- `read_skill`: read full `SKILL.md` by name or path
- `use_skill`: resolve + render skill for execution (`$ARGUMENTS`, `${CLAUDE_SESSION_ID}`)

Slash-style user invocation is supported on the first user message:

- Input format: `/<skill-name> <arguments>`
- The orchestrator resolves the skill, renders `SKILL.md`, and rewrites the first user message with rendered instructions
- Unknown slash commands are ignored (treated as normal text)

Skill front matter fields:

- `name`
- `description`
- `invocation`
- `user-invocable` (default `true`)
- `disable-model-invocation` (default `false`)
- `allowed-tools` (comma list or YAML list)

Skill precedence for duplicate names:

- Scope precedence: `project > personal > system > unknown`
- Within same scope, later-discovered directories win

Default skill search directories:

- Project layers from repo root to workdir: `.agents/skills`, `.codex/skills`
- Personal: `~/.agents/skills`, `~/.codex/skills`, `~/.codex/superpowers/skills`, `~/.claude/skills`
- System: `/etc/codex/skills`

Skill-related environment variables:

- `SKILL_DIRS`: overrides default discovery roots (path-list format)
- `SYSTEM_SKILL_DIRS`: appends extra system-level skill roots
- `ACTIVE_SKILL_NAME`: active skill name in tool context
- `ACTIVE_SKILL_PATH`: active skill file path in tool context
- `ACTIVE_SKILL_ALLOWED_TOOLS`: active allowed-tools policy in tool context
- `CLAUDE_SESSION_ID`: optional template variable for skill rendering

When an active skill has `allowed-tools`, the orchestrator blocks tool calls not matched by policy. `use_skill` remains callable to allow skill switching.

## OpenAI-Compatible Tool-Call Handling

Some OpenAI-compatible gateways return:

- `choices[0].finish_reason = "stop"`
- while still including `choices[0].message.tool_calls`

`OpenAIProvider` treats this as tool-use (`stop_reason=tool_use`) whenever `tool_calls` are present, so tool execution is not skipped.

## Optional GitHub/Webhook Extensions

The SDK contains no business logic by default:
- No prompt building — callers provide the full prompt via `Task`.
- No response parsing — `Summary` and `Message` contain raw LLM output.
- GitHub API tools are **not** registered by default.

If your integration needs GitHub issue/PR APIs, register them explicitly:

```go
registry := builtin.NewRegistryWithBuiltinsAndGitHub()
```

Then pass credentials via `tools.ToolContext.WithGitHub(token, owner, repo)` where needed.

## Legacy Runner Compatibility

The `llm.Request`/`llm.Response` types retain Issue/PR fields for backward compatibility with webhook-driven pipelines. Use `agent.RunnerAdapter` or `orchestrator.OrchestratorRunner` to bridge the new Agent interface to the legacy `llm.Runner` interface.
