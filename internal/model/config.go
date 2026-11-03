// Package model owns provider discovery, model selection, credentials resolution,
// and construction of Sigma's streaming model adapters.
package model

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/credentials"
)

type Selection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ProviderInfo struct {
	ID          string
	Name        string
	Environment string
	Protocol    string
}

var providers = []ProviderInfo{
	{ID: "minimax", Name: "MiniMax", Environment: "SIGMA_AGENT_API_KEY / ANTHROPIC_AUTH_TOKEN / MINIMAX_API_KEY", Protocol: "anthropic-messages"},
	{ID: "anthropic", Name: "Anthropic", Environment: "ANTHROPIC_AUTH_TOKEN / ANTHROPIC_API_KEY", Protocol: "anthropic-messages"},
	{ID: "openai", Name: "OpenAI", Environment: "OPENAI_API_KEY", Protocol: "openai-chat-completions"},
	{ID: "deepseek", Name: "DeepSeek", Environment: "DEEPSEEK_API_KEY", Protocol: "openai-chat-completions"},
	{ID: "openrouter", Name: "OpenRouter", Environment: "OPENROUTER_API_KEY", Protocol: "openai-chat-completions"},
}

func Providers() []ProviderInfo { return append([]ProviderInfo(nil), providers...) }

func providerInfo(id string) (ProviderInfo, bool) {
	for _, provider := range providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return ProviderInfo{}, false
}

func NormalizeProvider(provider string) (string, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "minimax", "mini-max", "mini_max":
		return "minimax", true
	case "anthropic", "claude":
		return "anthropic", true
	case "openai", "gpt":
		return "openai", true
	case "deepseek", "deep-seek":
		return "deepseek", true
	case "openrouter", "open-router":
		return "openrouter", true
	default:
		return "", false
	}
}

func DefaultModel(provider string) string {
	provider, _ = NormalizeProvider(provider)
	if provider == "minimax" {
		return "MiniMax-M3.1-Flash"
	}
	return ""
}

func LoadDefaultSelection() (Selection, error) {
	file, err := credentials.Load()
	if err != nil {
		return Selection{}, err
	}
	selection := Selection{Provider: file.DefaultModel.Provider, Model: file.DefaultModel.Model}
	if provider, ok := NormalizeProvider(selection.Provider); ok {
		selection.Provider = provider
	} else {
		selection.Provider = ""
	}
	return selection, nil
}

func SaveDefaultSelection(selection Selection) error {
	provider, ok := NormalizeProvider(selection.Provider)
	if !ok || strings.TrimSpace(selection.Model) == "" {
		return errors.New("model selection needs a supported provider and model id")
	}
	selection.Provider = provider
	selection.Model = strings.TrimSpace(selection.Model)
	file, err := credentials.Load()
	if err != nil {
		return err
	}
	file.DefaultModel.Provider = selection.Provider
	file.DefaultModel.Model = selection.Model
	return credentials.Save(file)
}

func SaveAPIKey(provider, key string) error {
	provider, ok := NormalizeProvider(provider)
	key = strings.TrimSpace(key)
	if !ok || key == "" || len(key) > 16*1024 || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("provider API key is empty or invalid")
	}
	file, err := credentials.Load()
	if err != nil {
		return err
	}
	file.APIKeys[provider] = key
	return credentials.Save(file)
}

func RemoveAPIKey(provider string) error {
	provider, ok := NormalizeProvider(provider)
	if !ok {
		return fmt.Errorf("unsupported model provider %q", provider)
	}
	file, err := credentials.Load()
	if err != nil {
		return err
	}
	delete(file.APIKeys, provider)
	return credentials.Save(file)
}

func ProviderHasAPIKey(provider string) (bool, error) {
	provider, ok := NormalizeProvider(provider)
	if !ok {
		return false, fmt.Errorf("unsupported model provider %q", provider)
	}
	if strings.TrimSpace(providerAPIKeyFromEnv(provider)) != "" {
		return true, nil
	}
	file, err := credentials.Load()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(file.APIKeys[provider]) != "" {
		return true, nil
	}
	if provider == "minimax" {
		data, err := os.ReadFile(".secrets/minimax-api-key")
		return strings.TrimSpace(string(data)) != "", notExistIsNil(err)
	}
	return false, nil
}

// NewConfigured creates the adapter selected by provider metadata. Model IDs
// are intentionally user-supplied because providers change their catalogs often.
func NewConfigured(selection Selection) (agent.Model, error) {
	provider, ok := NormalizeProvider(selection.Provider)
	if !ok {
		return nil, fmt.Errorf("unsupported model provider %q; use /model to choose a supported provider", selection.Provider)
	}
	modelID := strings.TrimSpace(selection.Model)
	if modelID == "" {
		modelID = DefaultModel(provider)
	}
	if modelID == "" || len(modelID) > 256 || strings.ContainsAny(modelID, "\r\n\x00") {
		return nil, errors.New("model id is required; use /model <provider>/<model-id>")
	}
	apiKey, err := providerAPIKey(provider)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured for %s; use /login %s", provider, provider)
	}
	info, ok := providerInfo(provider)
	if !ok {
		return nil, fmt.Errorf("unsupported model provider %q", provider)
	}
	baseURL := providerBaseURL(provider)
	switch info.Protocol {
	case "openai-chat-completions":
		return &agent.OpenAIChatCompletionsModel{Provider: provider, Model: modelID, BaseURL: baseURL, APIKey: apiKey}, nil
	case "anthropic-messages":
		return &agent.AnthropicMessagesModel{Provider: provider, Model: modelID, BaseURL: baseURL, APIKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("provider %q uses unsupported protocol %q", provider, info.Protocol)
	}
}

func providerAPIKey(provider string) (string, error) {
	file, err := credentials.Load()
	if err != nil {
		return "", err
	}
	if key := strings.TrimSpace(file.APIKeys[provider]); key != "" {
		return key, nil
	}
	if key := strings.TrimSpace(providerAPIKeyFromEnv(provider)); key != "" {
		return key, nil
	}
	if provider == "minimax" {
		data, err := os.ReadFile(".secrets/minimax-api-key")
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read local MiniMax API key file: %w", err)
		}
	}
	return "", nil
}

func providerAPIKeyFromEnv(provider string) string {
	switch provider {
	case "minimax":
		for _, name := range []string{"SIGMA_AGENT_API_KEY", "ANTHROPIC_AUTH_TOKEN", "MINIMAX_API_KEY"} {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				return value
			}
		}
	case "anthropic":
		for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				return value
			}
		}
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "deepseek":
		return os.Getenv("DEEPSEEK_API_KEY")
	case "openrouter":
		return os.Getenv("OPENROUTER_API_KEY")
	}
	return ""
}

func providerBaseURL(provider string) string {
	switch provider {
	case "minimax":
		for _, name := range []string{"SIGMA_AGENT_BASE_URL", "ANTHROPIC_BASE_URL"} {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				return value
			}
		}
		return "https://api.minimaxi.com/anthropic"
	case "anthropic":
		if value := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); value != "" {
			return value
		}
		return "https://api.anthropic.com"
	case "openai":
		if value := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); value != "" {
			return value
		}
		return "https://api.openai.com/v1"
	case "deepseek":
		if value := strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL")); value != "" {
			return value
		}
		return "https://api.deepseek.com"
	case "openrouter":
		if value := strings.TrimSpace(os.Getenv("OPENROUTER_BASE_URL")); value != "" {
			return value
		}
		return "https://openrouter.ai/api/v1"
	default:
		return ""
	}
}

func notExistIsNil(err error) error {
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
