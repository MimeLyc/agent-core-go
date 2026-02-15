package agent

import (
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/MimeLyc/agent-core-go/internal/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools"
)

// ProviderType identifies supported LLM provider backends for API agents.
type ProviderType string

const (
	ProviderTypeClaude ProviderType = "claude"
	ProviderTypeOpenAI ProviderType = "openai"
)

// AgentType identifies the type of agent to create.
type AgentType string

const (
	// AgentTypeAPI uses the local orchestrator with LLM API (Claude, OpenAI, etc.).
	AgentTypeAPI AgentType = "api"

	// AgentTypeCLI uses an external CLI tool (like Claude Code, aider, etc.).
	AgentTypeCLI AgentType = "cli"

	// AgentTypeClaudeCode is an alias for AgentTypeCLI (backward compatibility).
	// Deprecated: Use AgentTypeCLI instead.
	AgentTypeClaudeCode AgentType = "claude-code"

	// AgentTypeAuto automatically selects the best available agent.
	AgentTypeAuto AgentType = "auto"
)

// AgentConfig contains configuration for creating an agent.
type AgentConfig struct {
	// Type specifies which agent type to create.
	Type AgentType

	// API contains configuration for APIAgent.
	API *APIConfig

	// CLI contains configuration for CLI-based agents.
	CLI *CLIAgentConfig

	// Registry is the tool registry (used by APIAgent).
	Registry *tools.Registry
}

// APIConfig contains configuration for the API-based agent.
type APIConfig struct {
	// ProviderType specifies which LLM provider to use ("claude", "openai").
	// Must be set explicitly by the caller.
	ProviderType ProviderType

	// BaseURL is the LLM API base URL.
	BaseURL string

	// APIKey is the LLM API key.
	APIKey string

	// Model is the model to use.
	Model string

	// MaxTokens limits response token count.
	MaxTokens int

	// Timeout is the API request timeout.
	Timeout time.Duration

	// MaxAttempts is the maximum API retry count.
	MaxAttempts int

	// MaxIterations limits agent loop iterations.
	MaxIterations int

	// MaxMessages limits conversation history size.
	MaxMessages int

	// SystemPrompt is the default system prompt.
	SystemPrompt string

	// CompactConfig configures context compaction.
	CompactConfig *CompactConfig

	// EnableStreaming turns on stream-capable execution paths.
	EnableStreaming bool
}

// NewAgent creates a new agent based on the configuration.
func NewAgent(cfg AgentConfig) (Agent, error) {
	switch cfg.Type {
	case AgentTypeAPI:
		return newAPIAgentFromConfig(cfg)
	case AgentTypeCLI, AgentTypeClaudeCode:
		return newCLIAgentFromConfig(cfg)
	case AgentTypeAuto:
		return autoDetectAgent(cfg)
	default:
		return nil, fmt.Errorf("unknown agent type: %s", cfg.Type)
	}
}

// newAPIAgentFromConfig creates an APIAgent from configuration.
func newAPIAgentFromConfig(cfg AgentConfig) (*APIAgent, error) {
	if cfg.API == nil {
		return nil, fmt.Errorf("API configuration is required for api agent type")
	}

	apiCfg := cfg.API
	if apiCfg.BaseURL == "" {
		return nil, fmt.Errorf("API base URL is required")
	}
	if apiCfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if apiCfg.Model == "" {
		return nil, fmt.Errorf("API model is required")
	}

	// Create LLM provider based on configured type
	providerCfg := llm.LLMProviderConfig{
		Type:           llm.LLMProviderType(apiCfg.ProviderType),
		BaseURL:        apiCfg.BaseURL,
		APIKey:         apiCfg.APIKey,
		Model:          apiCfg.Model,
		MaxTokens:      apiCfg.MaxTokens,
		TimeoutSeconds: int(apiCfg.Timeout.Seconds()),
		MaxAttempts:    apiCfg.MaxAttempts,
	}

	provider, err := llm.NewLLMProvider(providerCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM provider: %w", err)
	}

	registry := cfg.Registry
	if registry == nil {
		registry = tools.NewRegistry()
	}

	opts := APIAgentOptions{
		MaxIterations:   apiCfg.MaxIterations,
		MaxMessages:     apiCfg.MaxMessages,
		MaxTokens:       apiCfg.MaxTokens,
		SystemPrompt:    apiCfg.SystemPrompt,
		CompactConfig:   apiCfg.CompactConfig,
		EnableStreaming: apiCfg.EnableStreaming,
	}

	return NewAPIAgent(provider, registry, opts), nil
}

// newCLIAgentFromConfig creates a CLIAgent from configuration.
func newCLIAgentFromConfig(cfg AgentConfig) (*CLIAgent, error) {
	if cfg.CLI == nil {
		return nil, fmt.Errorf("CLI configuration is required for cli agent type")
	}

	cliCfg := cfg.CLI
	if cliCfg.Command == "" {
		return nil, fmt.Errorf("CLI command is required")
	}
	if cliCfg.Timeout <= 0 {
		cliCfg.Timeout = 30 * time.Minute
	}

	// Verify CLI command exists
	if _, err := exec.LookPath(cliCfg.Command); err != nil {
		return nil, fmt.Errorf("CLI command not found: %s", cliCfg.Command)
	}

	client := NewClaudeCodeClient(*cliCfg)
	return NewCLIAgent(client, *cliCfg), nil
}

// autoDetectAgent automatically selects the best available agent.
func autoDetectAgent(cfg AgentConfig) (Agent, error) {
	log.Printf("[agent-factory] auto-detecting agent type")

	// First, try API agent if configured
	if cfg.API != nil && cfg.API.BaseURL != "" && cfg.API.APIKey != "" {
		log.Printf("[agent-factory] API configuration found, using api agent")
		return newAPIAgentFromConfig(cfg)
	}

	// Second, try CLI agent if configured and available
	if cfg.CLI != nil && cfg.CLI.Command != "" {
		if _, err := exec.LookPath(cfg.CLI.Command); err == nil {
			log.Printf("[agent-factory] CLI command found (%s), using cli agent", cfg.CLI.Command)
			return newCLIAgentFromConfig(cfg)
		}
	}

	return nil, fmt.Errorf("no agent available: configure API credentials or provide a CLI agent command")
}

// MustNewAgent creates a new agent or panics on error.
func MustNewAgent(cfg AgentConfig) Agent {
	agent, err := NewAgent(cfg)
	if err != nil {
		panic(err)
	}
	return agent
}
