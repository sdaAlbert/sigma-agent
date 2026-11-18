package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	"github.com/sdaAlbert/sigma-agent/internal/workflow"
	workspaceagent "github.com/sdaAlbert/sigma-agent/internal/workspace"
)

func TestResolveWorkspacePathUsesExplicitRepoAndRejectsMissingFolders(t *testing.T) {
	want := t.TempDir()
	got, err := resolveWorkspacePath(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("resolveWorkspacePath() = %q, want %q", got, filepath.Clean(want))
	}
	if _, err := resolveWorkspacePath(filepath.Join(want, "missing")); err == nil {
		t.Fatal("resolveWorkspacePath() accepted a missing folder")
	}
}

func TestSessionCommandsStayOutOfDefaultInteractiveMode(t *testing.T) {
	if shouldStartInteractive(false, false, "", "", true) {
		t.Fatal("session command without a task unexpectedly starts the TUI")
	}
	if !shouldStartInteractive(false, false, "", "", false) {
		t.Fatal("plain invocation should still default to interactive mode")
	}
	if shouldStartInteractive(false, false, "review changes", "", false) {
		t.Fatal("one-shot task unexpectedly starts the TUI")
	}
	if shouldStartInteractive(false, false, "", "existing-session", false) {
		t.Fatal("session resume unexpectedly starts the TUI")
	}
}

func TestConfiguredToolPolicyAppliesOverridePrecedence(t *testing.T) {
	tests := []struct {
		name         string
		config       config.Runtime
		approve      func(context.Context, agent.AuthorizationRequest) (bool, error)
		interactive  bool
		wantErr      bool
		wantDecision string
	}{
		{name: "default allow", config: config.Defaults(), wantDecision: "allow"},
		{name: "risk class overrides default", config: config.Runtime{DefaultToolPermission: "deny", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "allow"}}, wantDecision: "allow"},
		{name: "tool override wins", config: config.Runtime{DefaultToolPermission: "allow", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "ask"}, ToolPermissions: map[string]string{"edit_file": "deny"}}, wantErr: true, wantDecision: "deny"},
		{name: "tool override asks once", config: config.Runtime{DefaultToolPermission: "deny", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "allow"}, ToolPermissions: map[string]string{"edit_file": "ask"}}, approve: func(context.Context, agent.AuthorizationRequest) (bool, error) { return true, nil }, interactive: true, wantDecision: "allow_once"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var auditEvents []agent.Event
			policy := configuredToolPolicy(test.config, test.approve, func(event agent.Event) error {
				auditEvents = append(auditEvents, event)
				return nil
			}, test.interactive)
			err := policy(context.Background(), agent.Tool{Definition: agent.ToolDefinition{Name: "edit_file"}}, json.RawMessage(`{"path":"README.md"}`))
			if (err != nil) != test.wantErr {
				t.Fatalf("policy error = %v, wantErr %t", err, test.wantErr)
			}
			if len(auditEvents) == 0 {
				t.Fatal("tool policy did not write an authorization audit event")
			}
			last := auditEvents[len(auditEvents)-1]
			if last.Decision != test.wantDecision {
				t.Fatalf("last decision = %q, want %q; events: %#v", last.Decision, test.wantDecision, auditEvents)
			}
			if last.RiskClass != agent.RiskWorkspaceWrite || last.Target != "README.md" {
				t.Fatalf("audit event risk/target = %q/%q, want workspace_write/README.md", last.RiskClass, last.Target)
			}
		})
	}
}

func TestConfiguredToolPolicyAskFailsClosedAndRecordsOutcome(t *testing.T) {
	tests := []struct {
		name         string
		interactive  bool
		approve      func(context.Context, agent.AuthorizationRequest) (bool, error)
		wantStatuses []string
		wantDecision string
	}{
		{name: "noninteractive", wantStatuses: []string{"requested", "denied"}, wantDecision: "deny"},
		{name: "user denied", interactive: true, approve: func(context.Context, agent.AuthorizationRequest) (bool, error) { return false, nil }, wantStatuses: []string{"requested", "denied"}, wantDecision: "deny"},
		{name: "user approved once", interactive: true, approve: func(context.Context, agent.AuthorizationRequest) (bool, error) { return true, nil }, wantStatuses: []string{"requested", "allowed"}, wantDecision: "allow_once"},
		{name: "approval cancelled", interactive: true, approve: func(context.Context, agent.AuthorizationRequest) (bool, error) { return false, context.Canceled }, wantStatuses: []string{"requested", "cancelled"}, wantDecision: "deny"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Runtime{DefaultToolPermission: "allow", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "ask"}}
			var events []agent.Event
			policy := configuredToolPolicy(cfg, test.approve, func(event agent.Event) error { events = append(events, event); return nil }, test.interactive)
			err := policy(context.Background(), agent.Tool{Definition: agent.ToolDefinition{Name: "edit_file"}}, json.RawMessage(`{"path":"src/main.go"}`))
			wantErr := test.wantDecision == "deny"
			if (err != nil) != wantErr {
				t.Fatalf("policy error = %v, wantErr %t", err, wantErr)
			}
			if len(events) != len(test.wantStatuses) {
				t.Fatalf("audit event count = %d, want %d: %#v", len(events), len(test.wantStatuses), events)
			}
			for index, wantStatus := range test.wantStatuses {
				if events[index].Status != wantStatus {
					t.Errorf("audit event %d status = %q, want %q", index, events[index].Status, wantStatus)
				}
			}
			if events[len(events)-1].Decision != test.wantDecision {
				t.Errorf("final decision = %q, want %q", events[len(events)-1].Decision, test.wantDecision)
			}
		})
	}
}

func TestConfiguredToolPolicyRequiresWorkingAuditBeforeAllow(t *testing.T) {
	cfg := config.Defaults()
	policy := configuredToolPolicy(cfg, nil, nil, false)
	err := policy(context.Background(), agent.Tool{Definition: agent.ToolDefinition{Name: "read_file"}}, json.RawMessage(`{"path":"README.md"}`))
	if err == nil || !strings.Contains(err.Error(), "audit log is unavailable") {
		t.Fatalf("policy error = %v, want missing audit log to fail closed", err)
	}

	auditFailure := errors.New("disk full")
	approvalCalled := false
	cfg.DefaultToolPermission = "ask"
	policy = configuredToolPolicy(cfg, func(context.Context, agent.AuthorizationRequest) (bool, error) {
		approvalCalled = true
		return true, nil
	}, func(agent.Event) error { return auditFailure }, true)
	err = policy(context.Background(), agent.Tool{Definition: agent.ToolDefinition{Name: "read_file"}}, json.RawMessage(`{"path":"README.md"}`))
	if !errors.Is(err, auditFailure) || approvalCalled {
		t.Fatalf("policy error = %v, approvalCalled = %t; want audit failure and no approval", err, approvalCalled)
	}
}

func TestAskPoliciesSerializeToolCalls(t *testing.T) {
	tests := []struct {
		name   string
		config config.Runtime
		want   bool
	}{
		{name: "default policy", config: config.Defaults()},
		{name: "default asks", config: config.Runtime{DefaultToolPermission: "ask"}, want: true},
		{name: "risk class asks", config: config.Runtime{DefaultToolPermission: "allow", RiskPermissions: map[string]string{agent.RiskNetwork: "ask"}}, want: true},
		{name: "tool override asks", config: config.Runtime{DefaultToolPermission: "allow", ToolPermissions: map[string]string{"run_command": "ask"}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasAskPermissions(test.config); got != test.want {
				t.Fatalf("hasAskPermissions() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestToolApprovalDoesNotPersistBeyondOneCall(t *testing.T) {
	cfg := config.Runtime{DefaultToolPermission: "allow", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "ask"}}
	approvalCount := 0
	var events []agent.Event
	policy := configuredToolPolicy(cfg, func(context.Context, agent.AuthorizationRequest) (bool, error) {
		approvalCount++
		return true, nil
	}, func(event agent.Event) error { events = append(events, event); return nil }, true)
	tool := agent.Tool{Definition: agent.ToolDefinition{Name: "edit_file"}}
	for range 2 {
		if err := policy(context.Background(), tool, json.RawMessage(`{"path":"README.md"}`)); err != nil {
			t.Fatalf("approved tool call %d failed: %v", approvalCount+1, err)
		}
	}
	if approvalCount != 2 || len(events) != 4 {
		t.Fatalf("approval calls/events = %d/%d, want one approval and two audit events per tool call", approvalCount, len(events))
	}
}

type authorizationFlowModel struct{ calls int }

func (*authorizationFlowModel) ModelID() string { return "authorization-flow-test" }

func (m *authorizationFlowModel) Stream(context.Context, string, []agent.Message, []agent.ToolDefinition, func(agent.ModelEvent)) (agent.ModelResponse, error) {
	m.calls++
	if m.calls == 1 {
		return agent.ModelResponse{Content: []agent.ContentBlock{{
			Type: "tool_use", ID: "model-edit-call", Name: "edit_file", Input: json.RawMessage(`{"path":"README.md"}`),
		}}, StopReason: "tool_use"}, nil
	}
	return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "finished"}}, StopReason: "end_turn"}, nil
}

func TestConfiguredAuthorizationFlowsThroughSessionBeforeToolExecution(t *testing.T) {
	var handlerCalls int
	registry, err := agent.NewToolRegistry([]agent.Tool{{
		Definition: agent.ToolDefinition{Name: "edit_file", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Handle: func(context.Context, json.RawMessage) (string, error) {
			handlerCalls++
			return "updated", nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Runtime{DefaultToolPermission: "allow", RiskPermissions: map[string]string{agent.RiskWorkspaceWrite: "ask"}}
	var approvals []agent.AuthorizationRequest
	var auditEvents []agent.Event
	session := &agent.Session{
		ID: "authorization-flow", Model: &authorizationFlowModel{}, Tools: registry, SerializeToolCalls: hasAskPermissions(cfg),
		BeforeTool: configuredToolPolicy(cfg, func(_ context.Context, request agent.AuthorizationRequest) (bool, error) {
			approvals = append(approvals, request)
			return true, nil
		}, func(event agent.Event) error { auditEvents = append(auditEvents, event); return nil }, true),
		Limits: agent.Limits{MaxTurns: 3, MaxToolCalls: 2, MaxToolResultBytes: 1024, ToolTimeout: time.Second},
	}
	if _, err := session.Prompt(context.Background(), "edit the README"); err != nil {
		t.Fatalf("session prompt: %v", err)
	}
	if handlerCalls != 1 || len(approvals) != 1 {
		t.Fatalf("handler/approval calls = %d/%d, want one each", handlerCalls, len(approvals))
	}
	if approvals[0].ID != "model-edit-call" || approvals[0].Target != "README.md" || approvals[0].RiskClass != agent.RiskWorkspaceWrite {
		t.Fatalf("approval request = %#v, want call ID, target, and workspace-write risk", approvals[0])
	}
	if len(auditEvents) != 2 || auditEvents[0].Status != "requested" || auditEvents[1].Status != "allowed" || auditEvents[1].Decision != "allow_once" || auditEvents[1].AuthorizationID != "model-edit-call" || auditEvents[1].ToolCallID != "model-edit-call" {
		t.Fatalf("authorization audit events = %#v, want request and allowed-once correlated to tool call", auditEvents)
	}
}

func TestAuthorizationAuditPersistsSessionAndRunEvents(t *testing.T) {
	directory := t.TempDir()
	open := func(name string) *os.File {
		t.Helper()
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		return file
	}
	sessionLog, trace := open("authorizations.jsonl"), open("trace.jsonl")
	var mu sync.Mutex
	written, err := appendAuthorizationAuditEvent(agent.Event{Type: "tool_authorization", Tool: "edit_file", Decision: "allow_once", Status: "allowed", Target: "README.md"}, "session-123", sessionLog, trace, &mu)
	if err != nil {
		t.Fatal(err)
	}
	if written.Session != "session-123" || written.At.IsZero() {
		t.Fatalf("persisted event session/time = %q/%v, want session-123 and timestamp", written.Session, written.At)
	}
	for _, file := range []string{"authorizations.jsonl", "trace.jsonl"} {
		data, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil {
			t.Fatal(err)
		}
		var event agent.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &event); err != nil {
			t.Fatalf("decode %s event: %v", file, err)
		}
		if event.Type != "tool_authorization" || event.Decision != "allow_once" || event.Session != "session-123" {
			t.Errorf("%s event = %#v, want the recorded approval", file, event)
		}
	}
}

func TestTUIToolAuthorizationCanApproveDenyAndEscape(t *testing.T) {
	makeModel := func() (*tuiModel, chan bool) {
		response := make(chan bool, 1)
		model := &tuiModel{input: textarea.New(), transcript: viewport.New(), width: 80, height: 24}
		model.Update(tuiToolApprovalMsg{request: agent.AuthorizationRequest{Tool: "edit_file", RiskClass: agent.RiskWorkspaceWrite, Target: "README.md"}, response: response})
		return model, response
	}

	model, response := makeModel()
	if model.overlayDetail == "" || !strings.Contains(model.overlayDetail, "README.md") || !strings.Contains(model.overlayDetail, agent.RiskWorkspaceWrite) {
		t.Fatalf("approval detail = %q, want risk and target context", model.overlayDetail)
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if approved := <-response; !approved {
		t.Fatal("moving to Allow and pressing Enter returned deny")
	}
	if model.overlay != "" {
		t.Fatalf("approval overlay = %q, want closed", model.overlay)
	}

	model, response = makeModel()
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if approved := <-response; approved {
		t.Fatal("default Deny selection returned allow")
	}

	model, response = makeModel()
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if approved := <-response; approved {
		t.Fatal("Esc returned allow")
	}
}

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
		_, runErr := interactiveSession(ctx, session, workspace, "start the task", false, "", config.Runtime{}, workspaceagent.ProjectGuidance{}, workflow.NewHost(consoleWorkflowHandler{}))
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
	if _, err := io.WriteString(inputW, "/quit\n"); err != nil {
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
