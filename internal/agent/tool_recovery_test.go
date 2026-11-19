package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type recoveryTestModel struct {
	inspect func([]Message)
}

func (m recoveryTestModel) ModelID() string { return "recovery-test" }

func (m recoveryTestModel) Stream(_ context.Context, _ string, messages []Message, _ []ToolDefinition, _ func(ModelEvent)) (ModelResponse, error) {
	if m.inspect != nil {
		m.inspect(messages)
	}
	return ModelResponse{StopReason: "end_turn", Content: []ContentBlock{{Type: "text", Text: "continued"}}}, nil
}

type recoveryTestStore struct{ snapshot SessionSnapshot }

func (s *recoveryTestStore) Load(string) (SessionSnapshot, error) { return s.snapshot, nil }
func (s *recoveryTestStore) Save(snapshot SessionSnapshot) error {
	s.snapshot = snapshot
	return nil
}

func interruptedToolSnapshot(replayPolicy ToolReplayPolicy) SessionSnapshot {
	call := ContentBlock{Type: "tool_use", ID: "tool-call-1", Name: "external_write", Input: json.RawMessage(`{"value":"x"}`)}
	return SessionSnapshot{
		Version:      sessionSchemaVersion,
		ID:           "durable-resume",
		System:       "test",
		Messages:     []Message{{Role: "assistant", Content: []ContentBlock{call}}},
		PendingTools: []ContentBlock{call},
		ToolOperations: []ToolOperation{{
			ToolCallID: call.ID, Turn: 1, Tool: call.Name, IdempotencyKey: "stable-key", ReplayPolicy: replayPolicy,
			Status: "running", Args: append(json.RawMessage(nil), call.Input...),
		}},
	}
}

func TestContinueReplaysInterruptedSafeToolWithStableIdempotencyKey(t *testing.T) {
	store := &recoveryTestStore{snapshot: interruptedToolSnapshot(ToolReplaySafe)}
	var calls int
	registry, err := NewToolRegistry([]Tool{{
		Definition:   ToolDefinition{Name: "external_write", InputSchema: json.RawMessage(`{"type":"object"}`)},
		ReplayPolicy: ToolReplaySafe,
		Handle: func(ctx context.Context, _ json.RawMessage) (string, error) {
			calls++
			if got := ToolIdempotencyKey(ctx); got != "stable-key" {
				t.Fatalf("idempotency key = %q, want stable-key", got)
			}
			return "external operation completed", nil
		},
		Recover: func(_ context.Context, _ json.RawMessage, key string) (ToolRecoveryResult, error) {
			if key != "stable-key" {
				t.Fatalf("recovery idempotency key = %q, want stable-key", key)
			}
			return ToolRecoveryResult{Status: ToolRecoveryNotFound}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	model := recoveryTestModel{inspect: func(messages []Message) {
		if len(messages) < 2 || messages[len(messages)-1].Role != "user" || len(messages[len(messages)-1].Content) != 1 || messages[len(messages)-1].Content[0].Content != "external operation completed" {
			t.Fatalf("model did not receive replay result: %#v", messages)
		}
	}}
	session, err := RestoreSession(store.snapshot, model, registry, store)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("RestoreSession executed a side effect before Continue")
	}
	if _, err := session.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("safe tool executions = %d, want 1", calls)
	}
}

func TestNewToolRegistryDefaultsReplayPolicyToNever(t *testing.T) {
	registry, err := NewToolRegistry([]Tool{{
		Definition: ToolDefinition{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Handle:     func(context.Context, json.RawMessage) (string, error) { return "", nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.byName["read"].ReplayPolicy; got != ToolReplayNever {
		t.Fatalf("default replay policy = %q, want %q", got, ToolReplayNever)
	}
	if _, err := NewToolRegistry([]Tool{{
		Definition:   ToolDefinition{Name: "bad", InputSchema: json.RawMessage(`{"type":"object"}`)},
		ReplayPolicy: "sometimes",
		Handle:       func(context.Context, json.RawMessage) (string, error) { return "", nil },
	}}); err == nil {
		t.Fatal("unsupported replay policy was accepted")
	}
}

func TestContinueDoesNotReplayToolWithoutExplicitSafePolicy(t *testing.T) {
	store := &recoveryTestStore{snapshot: interruptedToolSnapshot("")}
	var calls int
	registry, err := NewToolRegistry([]Tool{{
		Definition: ToolDefinition{Name: "external_write", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Handle: func(context.Context, json.RawMessage) (string, error) {
			calls++
			return "should not run", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	model := recoveryTestModel{inspect: func(messages []Message) {
		if len(messages) < 2 || len(messages[len(messages)-1].Content) != 1 || !strings.Contains(messages[len(messages)-1].Content[0].Content, "status is unknown") {
			t.Fatalf("model did not receive unknown-outcome warning: %#v", messages)
		}
	}}
	session, err := RestoreSession(store.snapshot, model, registry, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("never-replay tool executions = %d, want 0", calls)
	}
}

func TestDeniedSafeReplayKeepsPreviousOutcomeUnknown(t *testing.T) {
	store := &recoveryTestStore{snapshot: interruptedToolSnapshot(ToolReplaySafe)}
	var calls int
	registry, err := NewToolRegistry([]Tool{{
		Definition:   ToolDefinition{Name: "external_write", InputSchema: json.RawMessage(`{"type":"object"}`)},
		ReplayPolicy: ToolReplaySafe,
		Handle: func(context.Context, json.RawMessage) (string, error) {
			calls++
			return "should not run", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := RestoreSession(store.snapshot, recoveryTestModel{}, registry, store)
	if err != nil {
		t.Fatal(err)
	}
	session.BeforeTool = func(context.Context, Tool, json.RawMessage) error { return errors.New("approval denied") }
	if _, err := session.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("denied replay executions = %d, want 0", calls)
	}
	if got := store.snapshot.ToolOperations[0].Status; got != "unknown" {
		t.Fatalf("persisted operation status = %q, want unknown", got)
	}
}

func TestRestoreUsesPersistedCompletedToolResultWithoutReplay(t *testing.T) {
	snapshot := interruptedToolSnapshot(ToolReplaySafe)
	snapshot.ToolOperations[0].Status = "completed"
	snapshot.ToolOperations[0].Output = "saved result"
	store := &recoveryTestStore{snapshot: snapshot}
	var calls int
	registry, err := NewToolRegistry([]Tool{{
		Definition:   ToolDefinition{Name: "external_write", InputSchema: json.RawMessage(`{"type":"object"}`)},
		ReplayPolicy: ToolReplaySafe,
		Handle: func(context.Context, json.RawMessage) (string, error) {
			calls++
			return "unexpected replay", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	model := recoveryTestModel{inspect: func(messages []Message) {
		if len(messages) < 2 || messages[len(messages)-1].Content[0].Content != "saved result" {
			t.Fatalf("model did not receive persisted result: %#v", messages)
		}
	}}
	session, err := RestoreSession(store.snapshot, model, registry, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("completed tool executions = %d, want 0", calls)
	}
}
