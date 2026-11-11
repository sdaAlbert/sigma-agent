package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestDeepSeekThinkingRoundTripsAcrossToolTurns(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []ContentBlock{
			{Type: "thinking", Text: "inspect the field path"},
			{Type: "text", Text: "I will inspect the model field."},
			{Type: "tool_use", ID: "call-1", Name: "read_file", Input: json.RawMessage(`{"path":"django/forms/models.py"}`)},
		}},
		{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "call-1", Content: "source"}}},
	}
	requestMessages, err := openAIRequestMessages("system", messages, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if got := requestMessages[1]["reasoning_content"]; got != "inspect the field path" {
		t.Fatalf("reasoning_content = %v, want preserved DeepSeek reasoning", got)
	}
	if _, ok := requestMessages[1]["tool_calls"]; !ok {
		t.Fatal("tool calls were dropped while preserving reasoning_content")
	}

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"follow the field\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	response, err := decodeOpenAIChatStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Content) != 2 || response.Content[0].Type != "thinking" || response.Content[0].Text != "follow the field" {
		t.Fatalf("decoded thinking content = %#v", response.Content)
	}
	if response.Content[1].Type != "text" || response.Content[1].Text != "Done." {
		t.Fatalf("decoded answer content = %#v", response.Content)
	}
}

func TestDeepSeekThinkingRequestConfiguration(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model := &OpenAIChatCompletionsModel{Provider: "deepseek", Model: "deepseek-flash", BaseURL: server.URL}
	model.ConfigureDeepSeekThinking(true)
	model.ConfigureReasoningEffort("medium")
	if _, err := model.Stream(context.Background(), "system", []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "task"}}}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	thinking, ok := request["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking request = %#v, want enabled", request["thinking"])
	}
	if request["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning_effort = %v, want medium", request["reasoning_effort"])
	}
}

func TestOpenAIRequestMessagesAnswersToolCallBeforeAdjacentUserText(t *testing.T) {
	messages := []Message{
		{Role: "assistant", Content: []ContentBlock{{
			Type: "tool_use", ID: "call-1", Name: "search_code",
		}}},
		{Role: "user", Content: []ContentBlock{{
			Type: "tool_result", ToolUseID: "call-1", Content: "found source",
		}}},
		{Role: "user", Content: []ContentBlock{{
			Type: "text", Text: "Progress checkpoint",
		}}},
	}

	prepared, err := (WindowContextManager{MaxTokens: 10000}).Prepare("system", messages)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	requestMessages, err := openAIRequestMessages(prepared.System, prepared.Messages)
	if err != nil {
		t.Fatalf("openAIRequestMessages() error = %v", err)
	}

	var gotRoles []string
	for _, message := range requestMessages {
		gotRoles = append(gotRoles, message["role"].(string))
	}
	wantRoles := []string{"system", "assistant", "tool", "user"}
	if !reflect.DeepEqual(gotRoles, wantRoles) {
		t.Fatalf("message roles = %v, want %v", gotRoles, wantRoles)
	}
	if requestMessages[2]["tool_call_id"] != "call-1" {
		t.Fatalf("tool response id = %v, want call-1", requestMessages[2]["tool_call_id"])
	}
}

func TestValidateOpenAIToolCallSequenceRejectsMalformedSequences(t *testing.T) {
	tests := []struct {
		name     string
		messages []map[string]any
	}{
		{
			name: "user message before tool response",
			messages: []map[string]any{
				{"role": "assistant", "tool_calls": []map[string]any{{"id": "call-1"}}},
				{"role": "user", "content": "checkpoint"},
				{"role": "tool", "tool_call_id": "call-1"},
			},
		},
		{
			name: "missing tool response",
			messages: []map[string]any{
				{"role": "assistant", "tool_calls": []map[string]any{{"id": "call-1"}}},
			},
		},
		{
			name: "orphan tool response",
			messages: []map[string]any{
				{"role": "tool", "tool_call_id": "call-1"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateOpenAIToolCallSequence(test.messages); err == nil {
				t.Fatal("validateOpenAIToolCallSequence() error = nil, want malformed sequence error")
			}
		})
	}
}

func TestDecodeOpenAIChatStreamCacheUsage(t *testing.T) {
	tests := []struct {
		name     string
		usage    string
		wantHit  int
		wantMiss int
	}{
		{
			name:     "deepseek explicit cache counters",
			usage:    `"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_cache_hit_tokens":72,"prompt_cache_miss_tokens":28}`,
			wantHit:  72,
			wantMiss: 28,
		},
		{
			name:     "cached token details fallback",
			usage:    `"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":64}}`,
			wantHit:  64,
			wantMiss: 36,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := `data: {"choices":[{"delta":{},"finish_reason":"stop"}],` + test.usage + `}` + "\n\n" + "data: [DONE]\n\n"
			response, err := decodeOpenAIChatStream(strings.NewReader(stream), nil)
			if err != nil {
				t.Fatal(err)
			}
			if response.InputTokens != 100 || response.OutputTokens != 3 {
				t.Fatalf("token totals = input %d, output %d; want 100, 3", response.InputTokens, response.OutputTokens)
			}
			if response.CacheReadInputTokens != test.wantHit || response.CacheMissInputTokens != test.wantMiss {
				t.Fatalf("cache tokens = hit %d, miss %d; want %d, %d", response.CacheReadInputTokens, response.CacheMissInputTokens, test.wantHit, test.wantMiss)
			}
		})
	}
}
