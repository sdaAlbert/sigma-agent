package agent

import (
	"reflect"
	"strings"
	"testing"
)

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
