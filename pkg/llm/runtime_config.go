package llm

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIPath         = "/v1/chat/completions"
	DefaultAPIKeyHeader    = "Authorization"
	DefaultAPIKeyPrefix    = "Bearer"
	DefaultAPIMaxAttempts  = 5
	DefaultTimeout         = 30 * time.Minute
	DefaultAgentIterations = 50
	DefaultAgentMessages   = 40
	DefaultAgentMaxTokens  = 4096
	DefaultCompactThresh   = 30
	DefaultCompactKeep     = 10
	DefaultAgentType       = "api"
	DefaultProviderType    = "claude"
)

// MCPServerConfig configures an MCP server connection for agent tools.
type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// RuntimeConfig contains LLM/agent runtime configuration.
type RuntimeConfig struct {
	LLMCommand        string
	LLMArgs           []string
	LLMAPIBaseURL     string
	LLMAPIKey         string
	LLMAPIModel       string
	LLMAPIPath        string
	LLMAPIKeyHeader   string
	LLMAPIKeyPrefix   string
	LLMAPIMaxAttempts int
	LLMTimeout        time.Duration

	// Agent mode configuration
	AgentMode          bool
	AgentMaxIterations int
	AgentMaxMessages   int
	AgentMaxTokens     int
	ToolsEnabled       bool
	MCPServers         []MCPServerConfig

	// Compact (context summarization) configuration
	CompactEnabled    bool
	CompactThreshold  int
	CompactKeepRecent int

	// Unified Agent configuration
	AgentType       string
	LLMProviderType string
	CLICommand      string
	CLIArgs         []string

	// Deprecated: Use CLICommand/CLIArgs instead.
	ClaudeCodePath string
	ClaudeCodeArgs []string
}

// LoadRuntimeConfig loads LLM/agent runtime configuration from environment.
func LoadRuntimeConfig(getenv func(string) string) RuntimeConfig {
	return RuntimeConfig{
		LLMCommand:         getenv("LLM_COMMAND"),
		LLMArgs:            parseList(getenv("LLM_ARGS")),
		LLMAPIBaseURL:      getenv("LLM_API_BASE_URL"),
		LLMAPIKey:          getenv("LLM_API_KEY"),
		LLMAPIModel:        getenv("LLM_API_MODEL"),
		LLMAPIPath:         getOrDefault(getenv, "LLM_API_PATH", DefaultAPIPath),
		LLMAPIKeyHeader:    getOrDefault(getenv, "LLM_API_KEY_HEADER", DefaultAPIKeyHeader),
		LLMAPIKeyPrefix:    getOrDefault(getenv, "LLM_API_KEY_PREFIX", DefaultAPIKeyPrefix),
		LLMAPIMaxAttempts:  getIntOrDefault(getenv, "LLM_API_MAX_ATTEMPTS", DefaultAPIMaxAttempts),
		LLMTimeout:         getDurationOrDefault(getenv, "LLM_TIMEOUT", DefaultTimeout),
		AgentMode:          getBoolOrDefault(getenv, "AGENT_MODE", false),
		AgentMaxIterations: getIntOrDefault(getenv, "AGENT_MAX_ITERATIONS", DefaultAgentIterations),
		AgentMaxMessages:   getIntOrDefault(getenv, "AGENT_MAX_MESSAGES", DefaultAgentMessages),
		AgentMaxTokens:     getIntOrDefault(getenv, "AGENT_MAX_TOKENS", DefaultAgentMaxTokens),
		ToolsEnabled:       getBoolOrDefault(getenv, "TOOLS_ENABLED", true),
		MCPServers:         parseMCPServers(getenv("MCP_SERVERS")),
		CompactEnabled:     getBoolOrDefault(getenv, "COMPACT_ENABLED", true),
		CompactThreshold:   getIntOrDefault(getenv, "COMPACT_THRESHOLD", DefaultCompactThresh),
		CompactKeepRecent:  getIntOrDefault(getenv, "COMPACT_KEEP_RECENT", DefaultCompactKeep),
		AgentType:          getOrDefault(getenv, "AGENT_TYPE", DefaultAgentType),
		LLMProviderType:    getOrDefault(getenv, "LLM_PROVIDER_TYPE", DefaultProviderType),
		CLICommand:         getOrDefaultMulti(getenv, "CLI_COMMAND", "CLAUDE_CODE_PATH", ""),
		CLIArgs:            parseListMulti(getenv, "CLI_ARGS", "CLAUDE_CODE_ARGS"),
		ClaudeCodePath:     getenv("CLAUDE_CODE_PATH"),
		ClaudeCodeArgs:     parseList(getenv("CLAUDE_CODE_ARGS")),
	}
}

// UsesAPI reports whether API-based LLM configuration is in use.
func (c RuntimeConfig) UsesAPI() bool {
	return c.LLMAPIBaseURL != "" || c.LLMAPIKey != "" || c.LLMAPIModel != ""
}

func getOrDefault(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

func getOrDefaultMulti(getenv func(string) string, keys ...string) string {
	for _, key := range keys {
		if v := getenv(key); v != "" {
			return v
		}
	}
	return ""
}

func parseListMulti(getenv func(string) string, keys ...string) []string {
	for _, key := range keys {
		if v := getenv(key); v != "" {
			return parseList(v)
		}
	}
	return nil
}

func getIntOrDefault(getenv func(string) string, key string, def int) int {
	val := getenv(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return parsed
}

func getDurationOrDefault(getenv func(string) string, key string, def time.Duration) time.Duration {
	val := getenv(key)
	if val == "" {
		return def
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return def
	}
	return parsed
}

func parseList(value string) []string {
	if value == "" {
		return nil
	}
	var parts []string
	if strings.Contains(value, ",") {
		parts = strings.Split(value, ",")
	} else {
		parts = strings.Fields(value)
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func getBoolOrDefault(getenv func(string) string, key string, def bool) bool {
	val := strings.ToLower(getenv(key))
	if val == "" {
		return def
	}
	return val == "true" || val == "1" || val == "yes"
}

// parseMCPServers parses MCP server configurations from a JSON string.
// Format: [{"name":"server1","command":"cmd","args":["arg1"],"env":{"KEY":"val"}}]
func parseMCPServers(value string) []MCPServerConfig {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "[") {
		var servers []MCPServerConfig
		if err := json.Unmarshal([]byte(value), &servers); err != nil {
			return nil
		}
		return servers
	}

	var servers []MCPServerConfig
	for _, part := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(parts) == 2 {
			servers = append(servers, MCPServerConfig{
				Name:    strings.TrimSpace(parts[0]),
				Command: strings.TrimSpace(parts[1]),
			})
		}
	}
	return servers
}
