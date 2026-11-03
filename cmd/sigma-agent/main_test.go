package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
)

type interactiveSteerModel struct {
	call     int
	gotSteer chan bool
}

func (m *interactiveSteerModel) ModelID() string { return "interactive-test-model" }

func (m *interactiveSteerModel) Stream(_ context.Context, _ string, messages []agent.Message, _ []agent.ToolDefinition, _ func(agent.ModelEvent)) (agent.ModelResponse, error) {
	m.call++
	if m.call == 1 {
		return agent.ModelResponse{
			Content:    []agent.ContentBlock{{Type: "tool_use", ID: "wait-call", Name: "wait", Input: json.RawMessage(`{}`)}},
			StopReason: "tool_use",
		}, nil
	}
	gotSteer := false
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, block := range message.Content {
			if block.Text == "focus on the smallest safe fix" {
				gotSteer = true
			}
		}
	}
	m.gotSteer <- gotSteer
	return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "done"}}, StopReason: "end_turn"}, nil
}

func TestInteractiveSessionSteersActiveRunAtSafeTurn(t *testing.T) {
	oldStdin, oldStderr := os.Stdin, os.Stderr
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stderr = inputR, stderrW
	defer func() {
		os.Stdin, os.Stderr = oldStdin, oldStderr
		_ = inputR.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		_ = inputW.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	toolStarted := make(chan struct{})
	releaseTool := make(chan struct{})
	tools, err := agent.NewToolRegistry([]agent.Tool{{
		Definition:    agent.ToolDefinition{Name: "wait", InputSchema: json.RawMessage(`{"type":"object"}`)},
		ExecutionMode: "sequential",
		Handle: func(ctx context.Context, _ json.RawMessage) (string, error) {
			close(toolStarted)
			select {
			case <-releaseTool:
				return "released", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	model := &interactiveSteerModel{gotSteer: make(chan bool, 1)}
	session := &agent.Session{ID: "interactive-test", Model: model, Tools: tools}
	workspace := t.TempDir()
	finished := make(chan error, 1)
	go func() {
		_, runErr := interactiveSession(ctx, session, workspace, "start the task", false, "", config.Defaults())
		finished <- runErr
	}()
	steeringQueued := make(chan struct{}, 2)
	go func() {
		scanner := bufio.NewScanner(stderrR)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "Steering queued") {
				steeringQueued <- struct{}{}
			}
		}
	}()
	if _, err := io.WriteString(inputW, "focus on the broad issue\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-steeringQueued:
	case <-time.After(5 * time.Second):
		t.Fatal("interactive CLI did not acknowledge the first steering message")
	}
	select {
	case <-toolStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not start its tool call")
	}
	if _, err := io.WriteString(inputW, "/steer focus on the smallest safe fix\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-steeringQueued:
	case <-time.After(5 * time.Second):
		t.Fatal("interactive CLI did not acknowledge the active-run steering message")
	}
	close(releaseTool)
	select {
	case gotSteer := <-model.gotSteer:
		if !gotSteer {
			t.Fatal("queued steering message was missing from the next model request")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not issue the next model request")
	}
	if _, err := io.WriteString(inputW, "/exit\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interactive CLI did not exit")
	}
}
