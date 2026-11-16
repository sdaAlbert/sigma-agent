package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestToolAuthorizationFailureBlocksHandlerAndCarriesCallID(t *testing.T) {
	var handlerCalls int
	registry, err := NewToolRegistry([]Tool{{
		Definition: ToolDefinition{Name: "edit_file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Handle: func(context.Context, json.RawMessage) (string, error) {
			handlerCalls++
			return "written", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("approval denied")
	var gotCallID string
	var events []Event
	session := &Session{
		ID: "authorization-test", Tools: registry,
		BeforeTool: func(ctx context.Context, _ Tool, _ json.RawMessage) error {
			gotCallID = ToolCallID(ctx)
			return denied
		},
		OnEvent: func(event Event) { events = append(events, event) },
	}
	result := session.executeTool(context.Background(), 1, ContentBlock{
		Type: "tool_use", ID: "call-write-1", Name: "edit_file", Input: json.RawMessage(`{"path":"README.md"}`),
	}, time.Second, 1024)
	if handlerCalls != 0 {
		t.Fatalf("tool handler ran %d times after authorization denied it", handlerCalls)
	}
	if !result.IsError {
		t.Fatalf("tool result IsError = false, want denied result")
	}
	if gotCallID != "call-write-1" {
		t.Fatalf("authorization saw call ID %q, want call-write-1", gotCallID)
	}
	if len(events) < 2 || events[0].Type != "tool_execution_start" || events[len(events)-1].Status != "failed" {
		t.Fatalf("tool lifecycle events = %#v, want start followed by failed end", events)
	}
}
