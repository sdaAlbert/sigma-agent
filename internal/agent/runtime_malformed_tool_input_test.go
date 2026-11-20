package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type malformedToolInputModel struct{ calls int }

func (m *malformedToolInputModel) ModelID() string { return "malformed-input-test-model" }

func (m *malformedToolInputModel) Stream(context.Context, string, []Message, []ToolDefinition, func(ModelEvent)) (ModelResponse, error) {
	m.calls++
	if m.calls == 1 {
		return ModelResponse{
			StopReason: "tool_use",
			Content: []ContentBlock{{
				Type: "tool_use", ID: "bad-json-call", Name: "record_change_contract",
				Input: json.RawMessage(`{"positive_acceptance": MigrationWriter.serialize(value)}`),
			}},
		}, nil
	}
	return ModelResponse{
		StopReason: "end_turn",
		Content:    []ContentBlock{{Type: "text", Text: "Recovered after malformed tool arguments."}},
	}, nil
}

func TestSessionRejectsMalformedToolJSONAndContinues(t *testing.T) {
	handlerCalls := 0
	registry, err := NewToolRegistry([]Tool{{
		Definition: ToolDefinition{Name: "record_change_contract", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Handle: func(context.Context, json.RawMessage) (string, error) {
			handlerCalls++
			return "should not execute", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{
		ID: "malformed-tool-input-test", Model: &malformedToolInputModel{}, Tools: registry,
		Store: store, Limits: DefaultLimits(),
	}
	result, err := session.Prompt(context.Background(), "Fix the migration serializer.")
	if err != nil {
		t.Fatalf("Prompt() error = %v; malformed tool arguments should be rejected as a tool result", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("tool handler was called %d times for malformed arguments", handlerCalls)
	}
	if result.Text != "Recovered after malformed tool arguments." {
		t.Fatalf("result text = %q", result.Text)
	}
	snapshot, err := store.Load(session.ID)
	if err != nil {
		t.Fatalf("load saved session after malformed tool call: %v", err)
	}
	var foundSanitizedCall, foundErrorResult bool
	for _, message := range snapshot.Messages {
		for _, block := range message.Content {
			if block.Type == "tool_use" && block.ID == "bad-json-call" {
				foundSanitizedCall = string(block.Input) == `{}`
			}
			if block.Type == "tool_result" && block.ToolUseID == "bad-json-call" && block.IsError && strings.Contains(block.Content, "valid JSON") {
				foundErrorResult = true
			}
		}
	}
	if !foundSanitizedCall || !foundErrorResult {
		t.Fatalf("saved transcript missing sanitized call or actionable error: %#v", snapshot.Messages)
	}
}
