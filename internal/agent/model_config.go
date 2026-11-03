package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/fsx"
)

const modelConfigVersion = 1

type ModelSelection struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ModelProviderInfo struct {
	ID          string
	Name        string
	Environment string
	Protocol    string
}

type modelConfigFile struct {
	Version      int               `json:"version"`
	APIKeys      map[string]string `json:"api_keys,omitempty"`
	DefaultModel ModelSelection    `json:"default_model,omitempty"`
	GitHubToken  string            `json:"github_token,omitempty"`
}

var supportedModelProviders = []ModelProviderInfo{
	{ID: "minimax", Name: "MiniMax", Environment: "MINIMAX_API_KEY", Protocol: "anthropic-messages"},
	{ID: "anthropic", Name: "Anthropic", Environment: "ANTHROPIC_API_KEY", Protocol: "anthropic-messages"},
	{ID: "openai", Name: "OpenAI", Environment: "OPENAI_API_KEY", Protocol: "openai-chat-completions"},
	{ID: "deepseek", Name: "DeepSeek", Environment: "DEEPSEEK_API_KEY", Protocol: "openai-chat-completions"},
	{ID: "openrouter", Name: "OpenRouter", Environment: "OPENROUTER_API_KEY", Protocol: "openai-chat-completions"},
}

func SupportedModelProviders() []ModelProviderInfo {
	return append([]ModelProviderInfo(nil), supportedModelProviders...)
}

func NormalizeModelProvider(provider string) (string, bool) {
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

func DefaultModelForProvider(provider string) string {
	provider, _ = NormalizeModelProvider(provider)
	if provider == "minimax" {
		return "MiniMax-M3"
	}
	return ""
}

func LoadDefaultModelSelection() (ModelSelection, error) {
	file, err := loadModelConfig()
	if err != nil {
		return ModelSelection{}, err
	}
	selection := file.DefaultModel
	if provider, ok := NormalizeModelProvider(selection.Provider); ok {
		selection.Provider = provider
	} else {
		selection.Provider = ""
	}
	return selection, nil
}

func SaveDefaultModelSelection(selection ModelSelection) error {
	provider, ok := NormalizeModelProvider(selection.Provider)
	if !ok || strings.TrimSpace(selection.Model) == "" {
		return errors.New("model selection needs a supported provider and model id")
	}
	selection.Provider = provider
	selection.Model = strings.TrimSpace(selection.Model)
	file, err := loadModelConfig()
	if err != nil {
		return err
	}
	file.DefaultModel = selection
	return saveModelConfig(file)
}

func SaveProviderAPIKey(provider, key string) error {
	provider, ok := NormalizeModelProvider(provider)
	key = strings.TrimSpace(key)
	if !ok || key == "" || len(key) > 16*1024 || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("provider API key is empty or invalid")
	}
	file, err := loadModelConfig()
	if err != nil {
		return err
	}
	if file.APIKeys == nil {
		file.APIKeys = make(map[string]string)
	}
	file.APIKeys[provider] = key
	return saveModelConfig(file)
}

func RemoveProviderAPIKey(provider string) error {
	provider, ok := NormalizeModelProvider(provider)
	if !ok {
		return fmt.Errorf("unsupported model provider %q", provider)
	}
	file, err := loadModelConfig()
	if err != nil {
		return err
	}
	delete(file.APIKeys, provider)
	return saveModelConfig(file)
}

func SaveGitHubToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 16*1024 || strings.ContainsAny(token, "\r\n\x00") {
		return errors.New("GitHub token is empty or invalid")
	}
	file, err := loadModelConfig()
	if err != nil {
		return err
	}
	file.GitHubToken = token
	return saveModelConfig(file)
}

func RemoveGitHubToken() error {
	file, err := loadModelConfig()
	if err != nil {
		return err
	}
	file.GitHubToken = ""
	return saveModelConfig(file)
}

func GitHubMCPToken() (string, error) {
	file, err := loadModelConfig()
	if err != nil {
		return "", err
	}
	if token := strings.TrimSpace(file.GitHubToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")); token != "" {
		return token, nil
	}
	data, err := os.ReadFile(filepath.Join(".secrets", "github-pat"))
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("read local GitHub token file: %w", err)
}

func ModelProviderHasAPIKey(provider string) (bool, error) {
	provider, ok := NormalizeModelProvider(provider)
	if !ok {
		return false, fmt.Errorf("unsupported model provider %q", provider)
	}
	if strings.TrimSpace(providerAPIKeyFromEnv(provider)) != "" {
		return true, nil
	}
	file, err := loadModelConfig()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(file.APIKeys[provider]) != "" {
		return true, nil
	}
	if provider == "minimax" {
		data, err := os.ReadFile(filepath.Join(".secrets", "minimax-api-key"))
		return strings.TrimSpace(string(data)) != "", errOrNil(err)
	}
	return false, nil
}

func NewConfiguredModel(selection ModelSelection) (Model, error) {
	provider, ok := NormalizeModelProvider(selection.Provider)
	if !ok {
		return nil, fmt.Errorf("unsupported model provider %q; use /model to choose a supported provider", selection.Provider)
	}
	modelID := strings.TrimSpace(selection.Model)
	if modelID == "" {
		modelID = DefaultModelForProvider(provider)
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
	baseURL := providerBaseURL(provider)
	if provider == "openai" || provider == "deepseek" || provider == "openrouter" {
		return &OpenAIChatCompletionsModel{Provider: provider, Model: modelID, BaseURL: baseURL, APIKey: apiKey, Client: defaultModelHTTPClient()}, nil
	}
	return &MiniMaxMessagesModel{Provider: provider, Model: modelID, BaseURL: baseURL, APIKey: apiKey, Client: defaultModelHTTPClient()}, nil
}

func providerAPIKey(provider string) (string, error) {
	file, err := loadModelConfig()
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
		data, err := os.ReadFile(filepath.Join(".secrets", "minimax-api-key"))
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
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

func modelConfigPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, "sigma-agent", "model-auth.json"), nil
}

func loadModelConfig() (modelConfigFile, error) {
	path, err := modelConfigPath()
	if err != nil {
		return modelConfigFile{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return modelConfigFile{Version: modelConfigVersion, APIKeys: make(map[string]string)}, nil
	}
	if err != nil {
		return modelConfigFile{}, fmt.Errorf("inspect model credential file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return modelConfigFile{}, errors.New("model credential file must be a small regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return modelConfigFile{}, fmt.Errorf("read model credential file: %w", err)
	}
	var file modelConfigFile
	if err := json.Unmarshal(data, &file); err != nil {
		return modelConfigFile{}, fmt.Errorf("decode model credential file: %w", err)
	}
	if file.Version != modelConfigVersion {
		return modelConfigFile{}, fmt.Errorf("unsupported model credential file version %d", file.Version)
	}
	if file.APIKeys == nil {
		file.APIKeys = make(map[string]string)
	}
	return file, nil
}

func saveModelConfig(file modelConfigFile) error {
	path, err := modelConfigPath()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create model credential directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil && !isWindows() {
		return fmt.Errorf("restrict model credential directory permissions: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return errors.New("model credential file path is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect model credential file path: %w", err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".model-auth-*.tmp")
	if err != nil {
		return fmt.Errorf("create model credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := fsx.ReplaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("save model credential file: %w", err)
	}
	return nil
}

func errOrNil(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func isWindows() bool { return os.PathSeparator == '\\' }

func defaultModelHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Minute}
}
