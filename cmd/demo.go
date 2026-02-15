package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
	"github.com/MimeLyc/agent-core-go/pkg/tools/builtin"
)

// Demo entrypoint for running the SDK in API mode.
func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("OPENAI_API_KEY is empty; set it to run cmd/demo.go")
		return
	}

	a, err := agent.NewAgent(agent.AgentConfig{
		Type: agent.AgentTypeAPI,
		API: &agent.APIConfig{
			ProviderType:  agent.ProviderTypeOpenAI,
			BaseURL:       "https://api.openai.com",
			APIKey:        apiKey,
			Model:         "gpt-4.1",
			MaxTokens:     1024,
			Timeout:       120 * time.Second,
			MaxAttempts:   3,
			MaxIterations: 8,
			MaxMessages:   30,
			SystemPrompt:  "You are a helpful assistant. Analyze the repository and respond with a brief summary.",
		},
		Registry: builtin.NewRegistryWithBuiltins(),
	})
	if err != nil {
		panic(err)
	}
	defer a.Close()

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "List the files in the current directory and return a short JSON response with a summary of what you found.",
		WorkDir: ".",
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Summary: %s\n", result.Summary)
}
