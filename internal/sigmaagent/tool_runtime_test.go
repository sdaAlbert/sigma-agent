package sigmaagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

type scriptedToolModel struct {
	filePath       string
	turn           int
	separateChecks bool
	repeatCheck    bool
	visibleTools   map[int]map[string]bool
	checksRun      []string
	checkTurns     []int
}

func TestRecordChangeContractSchemaRequiresExecutionPath(t *testing.T) {
	tool := (workspace{}).recordChangeContractTool()
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(tool.Definition.InputSchema, &schema); err != nil {
		t.Fatalf("decode change-contract schema: %v", err)
	}
	for _, name := range schema.Required {
		if name == "execution_path_and_short_circuits" {
			return
		}
	}
	t.Fatalf("change-contract schema does not require execution path: %#v", schema.Required)
}

func (m *scriptedToolModel) ModelID() string { return "scripted-test-model" }

func (m *scriptedToolModel) Stream(_ context.Context, _ string, _ []agent.Message, tools []agent.ToolDefinition, _ func(agent.ModelEvent)) (agent.ModelResponse, error) {
	m.turn++
	visible := make(map[string]bool, len(tools))
	for _, definition := range tools {
		visible[definition.Name] = true
	}
	if m.visibleTools == nil {
		m.visibleTools = make(map[int]map[string]bool)
	}
	m.visibleTools[m.turn] = visible

	use := func(name, input string) agent.ContentBlock {
		return agent.ContentBlock{Type: "tool_use", ID: fmt.Sprintf("call-%d-%s", m.turn, name), Name: name, Input: json.RawMessage(input)}
	}
	response := func(blocks ...agent.ContentBlock) (agent.ModelResponse, error) {
		return agent.ModelResponse{Content: blocks, StopReason: "tool_use"}, nil
	}
	switch m.turn {
	case 1:
		return response(
			use("read_file", `{"path":"main.go"}`),
			use("read_file", `{"path":"main_test.go"}`),
		)
	case 2:
		return response(use("record_change_contract", `{"behavior_and_trigger":"Change the returned value for the selected request.","scope_and_reason":"Keep the change limited to this handler.","existing_contract_and_callers":"The handler is covered by the neighboring package test.","execution_path_and_short_circuits":"Input enters the handler, the handler computes a result, and the response returns it; no earlier dispatch layer applies.","evidence_paths":["main.go","main_test.go"],"positive_acceptance":"The selected handler returns the new value.","negative_acceptance":"Other handlers retain their existing values."}`))
	case 3:
		if err := os.WriteFile(m.filePath, []byte("package main\n\nconst value = 3\n"), 0o600); err != nil {
			return agent.ModelResponse{}, err
		}
		return response(use("edit_file", `{"path":"main.go","old_text":"const value = 1","new_text":"const value = 2"}`))
	case 4:
		return response(use("read_file", `{"path":"main.go"}`))
	case 5:
		return response(use("edit_file", `{"path":"main.go","old_text":"const value = 3","new_text":"const value = 2"}`))
	case 6:
		return response(use("edit_file", `{"path":"main.go","old_text":"const value = 2","new_text":"const value = 5"}`))
	case 7:
		if m.separateChecks {
			return response(use("run_command", `{"command":"go test ./...","purpose":"test"}`))
		}
		return response(use("run_command", `{"command":"go test ./...","purpose":"test"}`), use("git_diff", `{}`))
	case 8:
		if m.separateChecks {
			return response(use("git_diff", `{}`))
		}
		if m.repeatCheck {
			return response(use("run_command", `{"command":"go test ./...","purpose":"test"}`))
		}
		return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "done"}}, StopReason: "end_turn"}, nil
	case 9:
		if m.repeatCheck {
			return response(use("run_command", `{"command":"go test ./...","purpose":"test"}`))
		}
		if m.separateChecks {
			return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "done"}}, StopReason: "end_turn"}, nil
		}
	case 10:
		if m.repeatCheck {
			return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "done"}}, StopReason: "end_turn"}, nil
		}
	default:
		return agent.ModelResponse{}, fmt.Errorf("unexpected scripted turn %d", m.turn)
	}
	return agent.ModelResponse{}, fmt.Errorf("unexpected scripted turn %d", m.turn)
}

func TestEditAndReviewWorkflowUsesReadSnapshotsAndKeepsChecksAvailable(t *testing.T) {
	for _, separateChecks := range []bool{false, true} {
		name := "both checks in one turn"
		if separateChecks {
			name = "only missing check in later turn"
		}
		t.Run(name, func(t *testing.T) {
			runEditAndReviewWorkflowTest(t, separateChecks, false)
		})
	}
}

func TestVerifiedRunAllowsRepeatedChecks(t *testing.T) {
	runEditAndReviewWorkflowTest(t, false, true)
}

func runEditAndReviewWorkflowTest(t *testing.T, separateChecks, repeatCheck bool) {
	t.Helper()
	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nconst value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ws := workspace{root: root, changeGate: newChangeContractGate()}
	checks := []string{}
	checkTurns := []int{}
	checkTool := func(name string) agent.Tool {
		return agent.Tool{
			Definition:    agent.ToolDefinition{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
			ExecutionMode: "sequential",
			Handle: func(ctx context.Context, _ json.RawMessage) (string, error) {
				checks = append(checks, name)
				turn, _ := agent.ToolTurn(ctx)
				checkTurns = append(checkTurns, turn)
				if name == "run_command" {
					return "SIGMA_RUN_COMMAND run_command purpose=test status=passed exit_code=0 workspace_changed=false action=run", nil
				}
				return "ok", nil
			},
		}
	}
	tools, err := agent.NewToolRegistry([]agent.Tool{
		ws.readFileTool(),
		ws.recordChangeContractTool(),
		ws.editFileTool(),
		checkTool("run_command"),
		checkTool("git_diff"),
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedToolModel{filePath: filePath, separateChecks: separateChecks, repeatCheck: repeatCheck}
	var toolErrors []string
	session := &agent.Session{
		ID:     "change-contract-test",
		Model:  model,
		Tools:  tools,
		Limits: agent.Limits{MaxTurns: 10, MaxToolCalls: 32, MaxToolResultBytes: 16 * 1024, ToolTimeout: time.Second},
		OnEvent: func(event agent.Event) {
			if event.Type == "tool_execution_end" && event.Error != "" {
				toolErrors = append(toolErrors, fmt.Sprintf("turn %d: %s", event.Turn, event.Error))
			}
		},
	}
	result, err := session.Prompt(context.Background(), "Make a narrowly scoped code change and verify it.")
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if result.VerificationStatus != "verified" {
		t.Fatalf("verification status = %q, want verified", result.VerificationStatus)
	}
	updated, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "const value = 2") {
		t.Fatalf("successful edit was not applied: %s", updated)
	}
	wantChecks := []string{"run_command", "git_diff"}
	if repeatCheck {
		wantChecks = append(wantChecks, "run_command", "run_command")
	}
	if strings.Join(checks, ",") != strings.Join(wantChecks, ",") {
		t.Fatalf("checks after final edit = %#v, want %#v", checks, wantChecks)
	}
	if !model.visibleTools[6]["run_command"] || !model.visibleTools[6]["git_diff"] || !model.visibleTools[6]["read_file"] || !model.visibleTools[6]["edit_file"] {
		t.Fatalf("missing checks or targeted correction tools were hidden during review: %#v", model.visibleTools[6])
	}
	if separateChecks {
		if !model.visibleTools[8]["run_command"] || !model.visibleTools[8]["git_diff"] || !model.visibleTools[8]["edit_file"] || !model.visibleTools[8]["read_file"] {
			t.Fatalf("review tools were hidden after tests passed: %#v", model.visibleTools[8])
		}
		if checkTurns[0] == checkTurns[1] {
			t.Fatalf("separate checks unexpectedly ran in the same turn: %#v", checkTurns)
		}
	} else if checkTurns[0] != checkTurns[1] {
		t.Fatalf("checks requested in one model response ran in different turns: %#v", checkTurns)
	}
	if repeatCheck {
		for _, turn := range []int{8, 9} {
			if !model.visibleTools[turn]["run_command"] || !model.visibleTools[turn]["git_diff"] {
				t.Fatalf("verification tools were hidden on turn %d: %#v", turn, model.visibleTools[turn])
			}
		}
	}
	joinedErrors := strings.Join(toolErrors, "\n")
	if !strings.Contains(joinedErrors, "changed since it was read") {
		t.Fatalf("stale read was not rejected using the server-side snapshot: %s", joinedErrors)
	}
}
