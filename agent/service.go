package provider

import "fmt"

func NewProvider(cfg ProviderConfig) (LLMProvider, error) {
	switch cfg.Type {
	case "openai":
		return NewOpenAIClient(cfg), nil

	case "openrouter":
		cfg.BaseURL = "https://openrouter.ai/api/v1"
		return NewOpenAIClient(cfg), nil

	case "ollama":
		return nil, fmt.Errorf("ollama not implemented yet")

	default:
		return nil, fmt.Errorf("unknown provider type: %s", cfg.Type)
	}
}
