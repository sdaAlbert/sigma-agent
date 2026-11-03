package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MiniMaxMessagesModel struct {
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	Client          *http.Client
	MaxRetries      int
	MaxOutputTokens int
}

func NewMiniMaxMessagesModelFromEnv() (*MiniMaxMessagesModel, error) {
	model := strings.TrimSpace(os.Getenv("SIGMA_AGENT_MODEL"))
	if model == "" {
		model = "MiniMax-M3"
	}
	baseURL := strings.TrimSpace(os.Getenv("SIGMA_AGENT_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://api.minimaxi.com/anthropic"
	}
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("MINIMAX_API_KEY"))
	}
	if apiKey == "" {
		data, err := os.ReadFile(filepath.Join(".secrets", "minimax-api-key"))
		if err == nil {
			apiKey = strings.TrimSpace(string(data))
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read local MiniMax API key file: %w", err)
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("set ANTHROPIC_AUTH_TOKEN, ANTHROPIC_API_KEY, or MINIMAX_API_KEY, or save the key in .secrets/minimax-api-key")
	}
	return &MiniMaxMessagesModel{
		Provider:        "minimax",
		Model:           model,
		BaseURL:         baseURL,
		APIKey:          apiKey,
		Client:          &http.Client{Timeout: 2 * time.Minute},
		MaxRetries:      2,
		MaxOutputTokens: 8192,
	}, nil
}

func (m *MiniMaxMessagesModel) ModelID() string {
	if m.Provider != "" {
		return m.Provider + "/" + m.Model
	}
	return m.Model
}

func (m *MiniMaxMessagesModel) ConfigureRuntime(maxRetries, maxOutputTokens int, timeout time.Duration) {
	m.MaxRetries = maxRetries
	m.MaxOutputTokens = maxOutputTokens
	m.Client = &http.Client{Timeout: timeout}
}

// WebSearch runs MiniMax's hosted web_search server tool and returns the
// final answer with source metadata for the agent to inspect.
func (m *MiniMaxMessagesModel) WebSearch(ctx context.Context, query string) (string, error) {
	if m.Client == nil {
		m.Client = &http.Client{Timeout: 2 * time.Minute}
	}
	endpoint, err := messagesEndpoint(m.BaseURL)
	if err != nil {
		return "", err
	}
	serverTool := struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}{Type: "web_search_20250305", Name: "web_search"}
	body, err := json.Marshal(struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []Message `json:"messages"`
		Tools     []any     `json:"tools"`
	}{
		Model: m.Model, MaxTokens: outputTokenLimit(m.MaxOutputTokens),
		Messages: []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: query}}}},
		Tools:    []any{serverTool},
	})
	if err != nil {
		return "", fmt.Errorf("encode MiniMax web search request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create MiniMax web search request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	setMessagesAuthForProvider(request, m.Provider, m.BaseURL, m.APIKey)
	response, err := m.Client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("send MiniMax web search request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read MiniMax web search response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 1000 {
			message = message[:1000] + "…"
		}
		return "", fmt.Errorf("MiniMax web search returned HTTP %d: %s", response.StatusCode, message)
	}
	var result struct {
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Input struct {
				Query string `json:"query"`
			} `json:"input"`
			Content []struct {
				Type    string `json:"type"`
				Title   string `json:"title"`
				URL     string `json:"url"`
				PageAge string `json:"page_age"`
				Content string `json:"content"`
			} `json:"content"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		BaseResp   struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("decode MiniMax web search response: %w", err)
	}
	if result.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("MiniMax web search failed (%d): %s", result.BaseResp.StatusCode, result.BaseResp.StatusMsg)
	}
	queryUsed := query
	var answer strings.Builder
	var sources []map[string]string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				if answer.Len() > 0 {
					answer.WriteString("\n")
				}
				answer.WriteString(block.Text)
			}
		case "server_tool_use":
			if block.Input.Query != "" {
				queryUsed = block.Input.Query
			}
		case "web_search_tool_result":
			for _, item := range block.Content {
				if item.Type == "web_search_result" {
					sources = append(sources, map[string]string{"title": item.Title, "url": item.URL, "page_age": item.PageAge, "snippet": item.Content})
				}
			}
		}
	}
	if result.StopReason == "pause_turn" {
		return "", errors.New("MiniMax web search reached its server-side search limit before producing a final answer")
	}
	if answer.Len() == 0 && len(sources) == 0 {
		return "", errors.New("MiniMax web search returned no answer or sources")
	}
	output, err := json.Marshal(map[string]any{"query": queryUsed, "answer": answer.String(), "sources": sources})
	if err != nil {
		return "", fmt.Errorf("encode MiniMax web search result: %w", err)
	}
	return string(output), nil
}

func (m *MiniMaxMessagesModel) Stream(ctx context.Context, system string, messages []Message, tools []ToolDefinition, onEvent func(ModelEvent)) (ModelResponse, error) {
	if m.Client == nil {
		m.Client = &http.Client{Timeout: 2 * time.Minute}
	}
	endpoint, err := messagesEndpoint(m.BaseURL)
	if err != nil {
		return ModelResponse{}, err
	}
	maxRetries := m.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	requestSystem := system
	requestMessages := cloneMessages(messages)
	transportRetries := 0
	promptCompactions := 0
	const maxPromptCompactions = 3
	for {
		body, err := json.Marshal(struct {
			Model     string           `json:"model"`
			MaxTokens int              `json:"max_tokens"`
			System    string           `json:"system"`
			Messages  []Message        `json:"messages"`
			Tools     []ToolDefinition `json:"tools,omitempty"`
			Stream    bool             `json:"stream"`
		}{Model: m.Model, MaxTokens: outputTokenLimit(m.MaxOutputTokens), System: requestSystem, Messages: requestMessages, Tools: tools, Stream: true})
		if err != nil {
			return ModelResponse{}, fmt.Errorf("encode model request: %w", err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return ModelResponse{}, fmt.Errorf("create model request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		request.Header.Set("anthropic-version", "2023-06-01")
		setMessagesAuthForProvider(request, m.Provider, m.BaseURL, m.APIKey)
		response, err := m.Client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ModelResponse{}, ctx.Err()
			}
			if transportRetries < maxRetries {
				if err := waitRetry(ctx, retryDelay(transportRetries, "")); err != nil {
					return ModelResponse{}, err
				}
				transportRetries++
				continue
			}
			return ModelResponse{}, fmt.Errorf("send model request: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			retryAfter := response.Header.Get("Retry-After")
			response.Body.Close()
			if readErr != nil {
				return ModelResponse{}, fmt.Errorf("read %s error response: %w", m.providerName(), readErr)
			}
			message := strings.TrimSpace(string(responseBody))
			quotaError := response.StatusCode == http.StatusPaymentRequired || isQuotaExhaustion(message)
			if isPromptTooLong(response.StatusCode, message) && promptCompactions < maxPromptCompactions {
				compacted, compactErr := compactRejectedPrompt(requestSystem, requestMessages)
				if compactErr == nil && compacted.EstimatedAfter < estimateTextTokens(requestSystem)+estimateMessageTokens(requestMessages) {
					requestSystem, requestMessages = compacted.System, compacted.Messages
					promptCompactions++
					continue
				}
			}
			if retryableStatus(response.StatusCode) && !quotaError && transportRetries < maxRetries {
				if err := waitRetry(ctx, retryDelay(transportRetries, retryAfter)); err != nil {
					return ModelResponse{}, err
				}
				transportRetries++
				continue
			}
			if len(message) > 1000 {
				message = message[:1000] + "…"
			}
			if quotaError {
				return ModelResponse{}, fmt.Errorf("%s account quota or balance is exhausted (HTTP %d); check provider usage/billing: %s", m.providerName(), response.StatusCode, message)
			}
			return ModelResponse{}, fmt.Errorf("%s API returned HTTP %d: %s", m.providerName(), response.StatusCode, message)
		}
		defer response.Body.Close()
		return decodeMessagesStream(response.Body, onEvent)
	}
}

func (m *MiniMaxMessagesModel) providerName() string {
	if strings.TrimSpace(m.Provider) == "" {
		return "MiniMax"
	}
	for _, provider := range supportedModelProviders {
		if provider.ID == m.Provider {
			return provider.Name
		}
	}
	return m.Provider
}

func isPromptTooLong(status int, message string) bool {
	if status != http.StatusBadRequest && status != http.StatusRequestEntityTooLarge && status != http.StatusUnprocessableEntity {
		return false
	}
	message = strings.ToLower(message)
	markers := []string{
		"prompt is too long", "prompt too long", "input is too long", "input too long",
		"context length exceeded", "maximum context length", "context window exceeded",
		"too many input tokens", "maximum input tokens", "request is too large",
		"提示词过长", "输入过长", "上下文长度超出", "超过最大上下文",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func compactRejectedPrompt(system string, messages []Message) (PreparedContext, error) {
	currentTokens := estimateTextTokens(system) + estimateMessageTokens(messages)
	if currentTokens <= 1 {
		return PreparedContext{}, errors.New("request has no compactable conversation context")
	}
	targetTokens := currentTokens * 3 / 4
	if targetTokens < 512 {
		targetTokens = 512
	}
	summaryTokens := targetTokens / 12
	if summaryTokens > 800 {
		summaryTokens = 800
	}
	return (WindowContextManager{MaxTokens: targetTokens, SummaryTokens: summaryTokens}).Prepare(system, messages)
}

func isQuotaExhaustion(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range []string{"insufficient balance", "insufficient credits", "balance is insufficient", "quota exhausted", "quota exceeded", "余额不足", "额度不足", "配额已用尽", "账户欠费"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func outputTokenLimit(configured int) int {
	if configured <= 0 {
		return 8192
	}
	return configured
}

type sseEnvelope struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Content []ContentBlock `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock ContentBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeMessagesStream(body io.Reader, onEvent func(ModelEvent)) (ModelResponse, error) {
	response := ModelResponse{}
	indices := make(map[int]int)
	partialInputs := make(map[int]*strings.Builder)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 32*1024), 2*1024*1024)
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if data == "[DONE]" {
			return nil
		}
		var event sseEnvelope
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("decode MiniMax stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			response.InputTokens = event.Message.Usage.InputTokens
			response.OutputTokens = event.Message.Usage.OutputTokens
		case "content_block_start":
			indices[event.Index] = len(response.Content)
			block := event.ContentBlock
			if block.Type == "tool_use" {
				block.Input = nil
				partialInputs[event.Index] = &strings.Builder{}
				if onEvent != nil {
					onEvent(ModelEvent{Type: "tool_call_start", Index: event.Index, Name: block.Name})
				}
			}
			response.Content = append(response.Content, block)
		case "content_block_delta":
			position, ok := indices[event.Index]
			if !ok {
				return fmt.Errorf("stream delta refers to unknown content block %d", event.Index)
			}
			block := &response.Content[position]
			switch event.Delta.Type {
			case "text_delta":
				block.Text += event.Delta.Text
				if onEvent != nil && event.Delta.Text != "" {
					onEvent(ModelEvent{Type: "text_delta", Text: event.Delta.Text, Index: event.Index})
				}
			case "input_json_delta":
				builder := partialInputs[event.Index]
				if builder == nil {
					return fmt.Errorf("tool argument delta refers to non-tool block %d", event.Index)
				}
				builder.WriteString(event.Delta.PartialJSON)
				if onEvent != nil && event.Delta.PartialJSON != "" {
					onEvent(ModelEvent{Type: "tool_input_delta", Text: event.Delta.PartialJSON, Index: event.Index})
				}
			case "thinking_delta":
				if onEvent != nil {
					onEvent(ModelEvent{Type: "thinking_delta", Text: event.Delta.Text, Index: event.Index})
				}
			}
		case "content_block_stop":
			if builder := partialInputs[event.Index]; builder != nil {
				position := indices[event.Index]
				raw := strings.TrimSpace(builder.String())
				if raw == "" {
					raw = "{}"
				}
				if !json.Valid([]byte(raw)) {
					response.Content[position].Input = json.RawMessage(raw)
				} else {
					response.Content[position].Input = json.RawMessage(raw)
				}
				delete(partialInputs, event.Index)
			}
		case "message_delta":
			response.StopReason = event.Delta.StopReason
			if event.Usage.InputTokens != 0 {
				response.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens != 0 {
				response.OutputTokens = event.Usage.OutputTokens
			}
		case "error":
			message := event.Error.Message
			if message == "" {
				message = event.Error.Type
			}
			return fmt.Errorf("model stream error: %s", message)
		}
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return ModelResponse{}, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return ModelResponse{}, fmt.Errorf("read MiniMax event stream: %w", err)
	}
	if err := flush(); err != nil {
		return ModelResponse{}, err
	}
	if len(partialInputs) > 0 {
		return ModelResponse{}, errors.New("MiniMax stream ended during tool argument generation")
	}
	return response, nil
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status == http.StatusInternalServerError
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > 30*time.Second {
			return 30 * time.Second
		}
		return delay
	}
	if retryAfter != "" {
		if when, err := http.ParseTime(retryAfter); err == nil {
			delay := time.Until(when)
			if delay < 0 {
				return 0
			}
			if delay > 30*time.Second {
				return 30 * time.Second
			}
			return delay
		}
	}
	delay := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func messagesEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid provider base URL")
	}
	trimmed := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		trimmed += "/messages"
	} else if !strings.HasSuffix(trimmed, "/v1/messages") {
		trimmed += "/v1/messages"
	}
	parsed.Path = trimmed
	return parsed.String(), nil
}

func setMessagesAuth(request *http.Request, baseURL, apiKey string) {
	setMessagesAuthForProvider(request, "minimax", baseURL, apiKey)
}

func setMessagesAuthForProvider(request *http.Request, provider, baseURL, apiKey string) {
	if provider == "anthropic" {
		if apiKey == strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")) && apiKey != "" {
			request.Header.Set("Authorization", "Bearer "+apiKey)
			return
		}
		request.Header.Set("x-api-key", apiKey)
		return
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "dashscope.aliyuncs.com" || strings.HasSuffix(host, ".maas.aliyuncs.com") {
			request.Header.Set("x-api-key", apiKey)
			return
		}
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
}
