package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBeforeToolHooksCanAllowOrBlockCalls(t *testing.T) {
	host := NewHost(nil)
	var calls []string
	if err := host.OnBeforeTool(func(_ context.Context, event Event) (bool, error) {
		calls = append(calls, "audit:"+event.Tool)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.OnBeforeTool(func(_ context.Context, event Event) (bool, error) {
		calls = append(calls, "policy:"+event.Tool)
		return event.Tool != "delete_file", nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := host.BeforeTool(context.Background(), Event{Tool: "read_file"}); err != nil {
		t.Fatalf("allow read_file: %v", err)
	}
	if err := host.BeforeTool(context.Background(), Event{Tool: "delete_file"}); err == nil || !strings.Contains(err.Error(), `blocked tool "delete_file"`) {
		t.Fatalf("block delete_file error = %v", err)
	}
	if got, want := strings.Join(calls, ","), "audit:read_file,policy:read_file,audit:delete_file,policy:delete_file"; got != want {
		t.Fatalf("hook order = %q, want %q", got, want)
	}
}

func TestBeforeToolHookErrorsFailClosed(t *testing.T) {
	host := NewHost(nil)
	want := errors.New("extension unavailable")
	if err := host.OnBeforeTool(func(context.Context, Event) (bool, error) { return true, want }); err != nil {
		t.Fatal(err)
	}
	if err := host.BeforeTool(context.Background(), Event{Tool: "edit_file"}); !errors.Is(err, want) {
		t.Fatalf("BeforeTool() error = %v, want %v", err, want)
	}
}
