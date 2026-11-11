package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ModelInfo is a model offered by a provider's authenticated model catalog.
type ModelInfo struct {
	ID   string
	Name string
}

// ListModels loads the provider's live model catalog using the configured key.
func ListModels(ctx context.Context, provider string) ([]ModelInfo, error) {
	provider, ok := NormalizeProvider(provider)
	if !ok {
		return nil, fmt.Errorf("unsupported model provider %q", provider)
	}
	key, err := providerAPIKey(provider)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fmt.Errorf("no API key configured for %s; use /login", provider)
	}
	base := strings.TrimRight(providerBaseURL(provider), "/")
	endpoint := base + "/models"
	if provider == "minimax" || provider == "anthropic" {
		endpoint = base + "/v1/models"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid model catalog URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if provider == "minimax" || provider == "anthropic" {
		request.Header.Set("x-api-key", key)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("could not load %s model catalog: %w", provider, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s model catalog returned HTTP %d", provider, response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("could not parse %s model catalog: %w", provider, err)
	}
	models := make([]ModelInfo, 0, len(payload.Data)+1)
	seen := make(map[string]bool)
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(entry.DisplayName)
		if name == "" {
			name = strings.TrimSpace(entry.Name)
		}
		models = append(models, ModelInfo{ID: id, Name: name})
	}
	if fallback := DefaultModel(provider); fallback != "" && !seen[fallback] {
		models = append([]ModelInfo{{ID: fallback, Name: "Default model"}}, models...)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%s returned no selectable models", provider)
	}
	return models, nil
}
