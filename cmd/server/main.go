package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
	"github.com/MimeLyc/agent-core-go/pkg/controller"
	"github.com/MimeLyc/agent-core-go/pkg/tools/builtin"
)

func main() {
	cfg := loadConfig()

	a, err := createAgent(cfg)
	if err != nil {
		log.Fatalf("failed to create agent: %v", err)
	}
	defer a.Close()

	chatCtrl := controller.NewChatController(a, controller.ChatConfig{
		SystemPrompt:    cfg.systemPrompt,
		SoulFile:        cfg.soulFile,
		DefaultDir:      cfg.workDir,
		EnableStreaming: cfg.streamingEnabled,
	})

	mux := http.NewServeMux()
	chatCtrl.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%d", cfg.serverPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: time.Duration(cfg.timeoutSeconds+10) * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-done
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("server stopped")
}

type serverConfig struct {
	// LLM
	providerType   agent.ProviderType
	baseURL        string
	apiKey         string
	model          string
	maxTokens      int
	timeoutSeconds int
	maxAttempts    int

	// Agent
	maxIterations    int
	maxMessages      int
	systemPrompt     string
	soulFile         string
	workDir          string
	streamingEnabled bool

	// Compaction
	compactEnabled    bool
	compactThreshold  int
	compactKeepRecent int

	// Server
	serverPort int
}

func loadConfig() serverConfig {
	return serverConfig{
		providerType:      agent.ProviderType(envOrDefault("LLM_PROVIDER_TYPE", "openai")),
		baseURL:           envOrDefault("LLM_BASE_URL", "https://api.openai.com"),
		apiKey:            os.Getenv("LLM_API_KEY"),
		model:             envOrDefault("LLM_MODEL", "gpt-4.1"),
		maxTokens:         envIntOrDefault("LLM_MAX_TOKENS", 4096),
		timeoutSeconds:    envIntOrDefault("LLM_TIMEOUT_SECONDS", 300),
		maxAttempts:       envIntOrDefault("LLM_MAX_ATTEMPTS", 5),
		maxIterations:     envIntOrDefault("AGENT_MAX_ITERATIONS", 0),
		maxMessages:       envIntOrDefault("AGENT_MAX_MESSAGES", 50),
		systemPrompt:      os.Getenv("AGENT_SYSTEM_PROMPT"),
		soulFile:          os.Getenv("AGENT_SOUL_FILE"),
		workDir:           envOrDefault("AGENT_WORK_DIR", "."),
		streamingEnabled:  envBoolOrDefault("AGENT_ENABLE_STREAMING", false),
		compactEnabled:    envBoolOrDefault("COMPACT_ENABLED", false),
		compactThreshold:  envIntOrDefault("COMPACT_THRESHOLD", 30),
		compactKeepRecent: envIntOrDefault("COMPACT_KEEP_RECENT", 10),
		serverPort:        envIntOrDefault("SERVER_PORT", 8080),
	}
}

func createAgent(cfg serverConfig) (agent.Agent, error) {
	if cfg.apiKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY is required")
	}

	var compactCfg *agent.CompactConfig
	if cfg.compactEnabled {
		compactCfg = &agent.CompactConfig{
			Enabled:    true,
			Threshold:  cfg.compactThreshold,
			KeepRecent: cfg.compactKeepRecent,
		}
	}

	return agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAPI,
		API: &agent.APIConfig{
			ProviderType:    cfg.providerType,
			BaseURL:         cfg.baseURL,
			APIKey:          cfg.apiKey,
			Model:           cfg.model,
			MaxTokens:       cfg.maxTokens,
			Timeout:         time.Duration(cfg.timeoutSeconds) * time.Second,
			MaxAttempts:     cfg.maxAttempts,
			MaxIterations:   cfg.maxIterations,
			MaxMessages:     cfg.maxMessages,
			SystemPrompt:    cfg.systemPrompt,
			CompactConfig:   compactCfg,
			EnableStreaming: cfg.streamingEnabled,
		},
		Registry: builtin.NewRegistryWithBuiltins(),
	})
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("warning: invalid integer for %s=%q, using default %d", key, v, def)
		return def
	}
	return n
}

func envBoolOrDefault(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("warning: invalid boolean for %s=%q, using default %v", key, v, def)
		return def
	}
	return b
}
