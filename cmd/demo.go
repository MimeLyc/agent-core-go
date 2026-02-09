package main

import (
	"context"
	"fmt"
	"os"

	"github.com/MimeLyc/agent-core-go/pkg/agent"
	"github.com/MimeLyc/agent-core-go/pkg/llm"
	"github.com/MimeLyc/agent-core-go/pkg/tools/builtin"
)

// Demo entrypoint for running the SDK in API mode.
func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("OPENAI_API_KEY is empty; set it to run cmd/demo.go")
		return
	}

	provider, err := llm.NewLLMProvider(llm.LLMProviderConfig{
		Type:           llm.ProviderOpenAI,
		BaseURL:        "https://api.openai.com",
		APIKey:         apiKey,
		Model:          "gpt-4.1",
		MaxTokens:      1024,
		TimeoutSeconds: 120,
		MaxAttempts:    3,
	})
	if err != nil {
		panic(err)
	}

	registry := builtin.NewRegistryWithBuiltins()
	a := agent.NewAPIAgent(provider, registry, agent.APIAgentOptions{
		MaxIterations: 8,
		MaxMessages:   30,
		MaxTokens:     1024,
		SystemPrompt:  "You are a helpful assistant. Analyze the repository and respond with a brief summary.",
	})

	result, err := a.Execute(context.Background(), agent.AgentRequest{
		Task:    "List the files in the current directory and return a short JSON response with a summary of what you found.",
		WorkDir: ".",
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Decision: %s\n", result.Decision)
	fmt.Printf("Summary: %s\n", result.Summary)
}
