package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OpenAIChatCompletionsModel struct {
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	Client          *http.Client
	MaxRetries      int
	MaxOutputTokens int
}

func (m *OpenAIChatCompletionsModel) ModelID() string { return m.Provider + "/" + m.Model }

func (m *OpenAIChatCompletionsModel) ConfigureRuntime(maxRetries, maxOutputTokens int, timeout time.Duration) {
	m.MaxRetries = maxRetries
	m.MaxOutputTokens = maxOutputTokens
	m.Client = &http.Client{Timeout: timeout}
}

func (m *OpenAIChatCompletionsModel) Stream(ctx context.Context, system string, messages []Message, tools []ToolDefinition, onEvent func(ModelEvent)) (ModelResponse, error) {
	if m.Client == nil {
		m.Client = defaultModelHTTPClient()
	}
	endpoint, err := chatCompletionsEndpoint(m.BaseURL)
	if err != nil {
		return ModelResponse{}, err
	}
	requestMessages, err := openAIRequestMessages(system, messages)
	if err != nil {
		return ModelResponse{}, err
	}
	requestTools, err := openAIRequestTools(tools)
	if err != nil {
		return ModelResponse{}, err
	}
	requestBody := map[string]any{
		"model": m.Model, "messages": requestMessages, "stream": true,
		"max_tokens": outputTokenLimit(m.MaxOutputTokens),
	}
	// DeepSeek enables its thinking mode by default. Sigma's Chat Completions
	// adapter does not preserve DeepSeek's reasoning_content across tool turns,
	// and coding-agent tool loops benefit from the faster non-thinking mode.
	if strings.EqualFold(m.Provider, "deepseek") {
		requestBody["thinking"] = map[string]string{"type": "disabled"}
	}
	if len(requestTools) > 0 {
		requestBody["tools"] = requestTools
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("encode %s request: %w", m.Provider, err)
	}
	maxRetries := m.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return ModelResponse{}, fmt.Errorf("create %s request: %w", m.Provider, err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		request.Header.Set("Authorization", "Bearer "+m.APIKey)
		response, err := m.Client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ModelResponse{}, ctx.Err()
			}
			if attempt < maxRetries {
				if waitErr := waitRetry(ctx, retryDelay(attempt, "")); waitErr != nil {
					return ModelResponse{}, waitErr
				}
				continue
			}
			return ModelResponse{}, fmt.Errorf("send %s request: %w", m.Provider, err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			retryAfter := response.Header.Get("Retry-After")
			response.Body.Close()
			if readErr != nil {
				return ModelResponse{}, fmt.Errorf("read %s error response: %w", m.Provider, readErr)
			}
			message := strings.TrimSpace(string(responseBody))
			if len(message) > 1000 {
				message = message[:1000] + "…"
			}
			if retryableStatus(response.StatusCode) && attempt < maxRetries {
				if waitErr := waitRetry(ctx, retryDelay(attempt, retryAfter)); waitErr != nil {
					return ModelResponse{}, waitErr
				}
				continue
			}
			return ModelResponse{}, fmt.Errorf("%s API returned HTTP %d: %s", m.Provider, response.StatusCode, message)
		}
		result, decodeErr := decodeOpenAIChatStream(response.Body, onEvent)
		response.Body.Close()
		return result, decodeErr
	}
}

func openAIRequestMessages(system string, messages []Message) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(messages)+1)
	if system != "" {
		result = append(result, map[string]any{"role": "system", "content": system})
	}
	for _, message := range messages {
		if message.Role == "assistant" {
			entry := map[string]any{"role": "assistant"}
			var text strings.Builder
			toolCalls := make([]map[string]any, 0)
			for _, block := range message.Content {
				switch block.Type {
				case "text":
					text.WriteString(block.Text)
				case "tool_use":
					arguments := block.Input
					if len(arguments) == 0 || !json.Valid(arguments) {
						arguments = json.RawMessage(`{}`)
					}
					toolCalls = append(toolCalls, map[string]any{
						"id": block.ID, "type": "function",
						"function": map[string]any{"name": block.Name, "arguments": string(arguments)},
					})
				}
			}
			if text.Len() > 0 {
				entry["content"] = text.String()
			} else {
				entry["content"] = nil
			}
			if len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
			}
			result = append(result, entry)
			continue
		}
		var userText strings.Builder
		var toolResults []map[string]any
		for _, block := range message.Content {
			switch block.Type {
			case "text":
				userText.WriteString(block.Text)
			case "tool_result":
				content := block.Content
				if block.IsError {
					content = "Tool error: " + content
				}
				toolResults = append(toolResults, map[string]any{"role": "tool", "tool_call_id": block.ToolUseID, "content": content})
			}
		}
		if userText.Len() > 0 {
			result = append(result, map[string]any{"role": "user", "content": userText.String()})
		}
		result = append(result, toolResults...)
	}
	if len(result) == 0 {
		return nil, errors.New("model request has no messages")
	}
	return result, nil
}

func openAIRequestTools(tools []ToolDefinition) ([]map[string]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == "" || !json.Valid(tool.InputSchema) {
			return nil, fmt.Errorf("invalid tool definition %q", tool.Name)
		}
		var schema any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("decode schema for tool %s: %w", tool.Name, err)
		}
		result = append(result, map[string]any{"type": "function", "function": map[string]any{
			"name": tool.Name, "description": tool.Description, "parameters": schema,
		}})
	}
	return result, nil
}

func chatCompletionsEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid provider base URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/chat/completions") {
		path += "/chat/completions"
	}
	parsed.Path = path
	return parsed.String(), nil
}

type openAIStreamChunk struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func decodeOpenAIChatStream(body io.Reader, onEvent func(ModelEvent)) (ModelResponse, error) {
	response := ModelResponse{}
	toolPositions := make(map[int]int)
	toolArguments := make(map[int]*strings.Builder)
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
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode OpenAI-compatible stream event: %w", err)
		}
		if chunk.Error.Message != "" {
			return errors.New(chunk.Error.Message)
		}
		response.InputTokens = chunk.Usage.PromptTokens
		response.OutputTokens = chunk.Usage.CompletionTokens
		if len(chunk.Choices) == 0 {
			return nil
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			position := -1
			for i := range response.Content {
				if response.Content[i].Type == "text" {
					position = i
					break
				}
			}
			if position < 0 {
				position = len(response.Content)
				response.Content = append(response.Content, ContentBlock{Type: "text"})
			}
			response.Content[position].Text += choice.Delta.Content
			if onEvent != nil {
				onEvent(ModelEvent{Type: "text_delta", Text: choice.Delta.Content, Index: position})
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			position, exists := toolPositions[delta.Index]
			if !exists {
				position = len(response.Content)
				block := ContentBlock{Type: "tool_use", ID: delta.ID}
				response.Content = append(response.Content, block)
				toolPositions[delta.Index] = position
				toolArguments[delta.Index] = &strings.Builder{}
				if onEvent != nil {
					onEvent(ModelEvent{Type: "tool_call_start", Index: position, Name: delta.Function.Name})
				}
			}
			block := &response.Content[position]
			if delta.ID != "" {
				block.ID = delta.ID
			}
			if delta.Function.Name != "" {
				block.Name += delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				toolArguments[delta.Index].WriteString(delta.Function.Arguments)
				if onEvent != nil {
					onEvent(ModelEvent{Type: "tool_input_delta", Text: delta.Function.Arguments, Index: position})
				}
			}
		}
		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "stop":
				response.StopReason = "end_turn"
			case "tool_calls", "function_call":
				response.StopReason = "tool_use"
			case "length":
				response.StopReason = "max_tokens"
			default:
				response.StopReason = *choice.FinishReason
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
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
		return ModelResponse{}, fmt.Errorf("read OpenAI-compatible event stream: %w", err)
	}
	if err := flush(); err != nil {
		return ModelResponse{}, err
	}
	for index, arguments := range toolArguments {
		position := toolPositions[index]
		raw := strings.TrimSpace(arguments.String())
		if raw == "" {
			raw = "{}"
		}
		response.Content[position].Input = json.RawMessage(raw)
	}
	return response, nil
}
