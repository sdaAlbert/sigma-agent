package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestFinalizationToolDefinitionsExposeOnlyMissingChecks(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_file"},
		{Name: "record_change_contract"},
		{Name: "run_command"},
		{Name: "git_diff"},
		{Name: "create_file"},
		{Name: "edit_file"},
		{Name: "apply_patch"},
	}
	tests := []struct {
		name                 string
		reviewOnly           bool
		testsNeeded          bool
		diffNeeded           bool
		verificationComplete bool
		want                 []string
	}{
		{name: "both missing", reviewOnly: true, testsNeeded: true, diffNeeded: true, want: []string{"read_file", "record_change_contract", "run_command", "git_diff", "create_file", "edit_file", "apply_patch"}},
		{name: "only tests missing", reviewOnly: true, testsNeeded: true, want: []string{"read_file", "record_change_contract", "run_command", "create_file", "edit_file", "apply_patch"}},
		{name: "only diff missing", reviewOnly: true, diffNeeded: true, want: []string{"read_file", "record_change_contract", "git_diff", "create_file", "edit_file", "apply_patch"}},
		{name: "review complete", reviewOnly: true, want: []string{"read_file", "record_change_contract", "create_file", "edit_file", "apply_patch"}},
		{name: "verified source allows correction but no repeated checks", verificationComplete: true, want: []string{"read_file", "record_change_contract", "create_file", "edit_file", "apply_patch"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotDefinitions := finalizationToolDefinitions(definitions, test.reviewOnly, test.testsNeeded, test.diffNeeded, test.verificationComplete)
			got := make([]string, 0, len(gotDefinitions))
			for _, definition := range gotDefinitions {
				got = append(got, definition.Name)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tools = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMissingModelStopReasonIsRecoverable(t *testing.T) {
	tests := []struct {
		name       string
		stopReason string
		want       bool
	}{
		{name: "missing finish reason", stopReason: "", want: true},
		{name: "normal final response", stopReason: "end_turn"},
		{name: "tool response", stopReason: "tool_use"},
		{name: "output limit", stopReason: "max_tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRecoverMissingStopReason(test.stopReason); got != test.want {
				t.Fatalf("shouldRecoverMissingStopReason(%q) = %t, want %t", test.stopReason, got, test.want)
			}
		})
	}
}

func TestReviewToolNeededOnlyAllowsOutstandingCheck(t *testing.T) {
	tests := []struct {
		name       string
		tool       string
		input      string
		testsNeed  bool
		diffNeeded bool
		want       bool
	}{
		{name: "only diff remains", tool: "git_diff", diffNeeded: true, want: true},
		{name: "native test command", tool: "run_command", input: `{"purpose":"test"}`, testsNeed: true, want: true},
		{name: "inspect command is blocked while tests remain", tool: "run_command", input: `{"purpose":"inspect"}`, testsNeed: true},
		{name: "missing purpose is blocked while tests remain", tool: "run_command", input: `{}`, testsNeed: true},
		{name: "edit is a correction tool, not a verification check", tool: "edit_file", testsNeed: true, diffNeeded: true},
		{name: "no review tools remain", tool: "git_diff"},
	}
	for _, name := range []string{"read_file", "record_change_contract", "create_file", "edit_file", "apply_patch"} {
		if !isReviewCorrectionTool(name) {
			t.Errorf("%q should remain available for targeted repair while checks are pending", name)
		}
	}
	for _, name := range []string{"search_code", "search_memory", "list_files"} {
		if isReviewCorrectionTool(name) {
			t.Errorf("%q should not bypass targeted verification", name)
		}
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := reviewToolNeeded(test.tool, json.RawMessage(test.input), test.testsNeed, test.diffNeeded)
			if got != test.want {
				t.Fatalf("reviewToolNeeded(%q, %s, %t, %t) = %t, want %t", test.tool, test.input, test.testsNeed, test.diffNeeded, got, test.want)
			}
		})
	}
}

func TestToolDefinitionFilteringPreservesDefinitions(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "run_command", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "git_diff", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	got := finalizationToolDefinitions(definitions, true, true, true, false)
	if !reflect.DeepEqual(got, definitions) {
		t.Fatalf("definitions = %#v, want %#v", got, definitions)
	}
}

func TestRequiredSkillBootstrapExposesOnlyReadSkill(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_skill"},
		{Name: "search_memory"},
		{Name: "list_files"},
		{Name: "search_code"},
		{Name: "run_command"},
	}
	got := requiredSkillBootstrapTools(definitions, []string{"python-issue-workflow"})
	if !reflect.DeepEqual(toolNames(got), []string{"read_skill"}) {
		t.Fatalf("bootstrap tools = %v, want only read_skill", toolNames(got))
	}
	got = requiredSkillBootstrapTools(definitions, nil)
	if !reflect.DeepEqual(toolNames(got), []string{"read_skill", "search_memory", "list_files", "search_code", "run_command"}) {
		t.Fatalf("tools without a pending Skill = %v, want all tools", toolNames(got))
	}
}

func TestRequiredSkillGateBlocksWorkUntilMatchingSkillIsRead(t *testing.T) {
	pending := []string{"python-issue-workflow"}
	if !skillBootstrapCallAllowed("read_skill", json.RawMessage(`{"name":"python-issue-workflow"}`), pending) {
		t.Fatal("matching read_skill call should be allowed")
	}
	if skillBootstrapCallAllowed("read_skill", json.RawMessage(`{"name":"other"}`), pending) {
		t.Fatal("unmatched read_skill should be blocked")
	}
	for _, name := range []string{"read_file", "search_code", "run_command", "search_memory"} {
		if skillBootstrapCallAllowed(name, json.RawMessage(`{}`), pending) {
			t.Errorf("%s should be blocked until the required Skill is loaded", name)
		}
	}
	if !skillBootstrapCallAllowed("search_memory", json.RawMessage(`{}`), nil) {
		t.Fatal("tools should be unrestricted after all required Skills are loaded")
	}
}

func TestFinalizationInspectionBudgetKeepsOnlyClosureTools(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_file"}, {Name: "search_code"}, {Name: "list_files"},
		{Name: "search_memory"}, {Name: "run_command"}, {Name: "edit_file"},
		{Name: "apply_patch"}, {Name: "git_diff"},
	}
	got := finalizationBudgetToolDefinitions(definitions, true)
	want := []string{"read_file", "run_command", "edit_file", "apply_patch", "git_diff"}
	if !reflect.DeepEqual(toolNames(got), want) {
		t.Fatalf("closure tools = %v, want %v", toolNames(got), want)
	}
	if finalizationInspectionCallBlocked("read_file", nil, true) {
		t.Fatal("targeted file reads must remain available after the finalization exploration budget is exhausted")
	}
	if !finalizationInspectionCallBlocked("run_command", json.RawMessage(`{"purpose":"inspect"}`), true) {
		t.Fatal("inspection commands should be blocked after the budget is exhausted")
	}
	if finalizationInspectionCallBlocked("run_command", json.RawMessage(`{"purpose":"test"}`), true) {
		t.Fatal("test commands must remain available for verification")
	}
	if finalizationInspectionCallBlocked("edit_file", nil, true) || finalizationInspectionCallBlocked("git_diff", nil, true) {
		t.Fatal("targeted edits and diff review must remain available")
	}
	if finalizationInspectionBudgetExhausted(true, maxFinalizationInspectionCalls-1) {
		t.Fatal("inspection budget exhausted one call early")
	}
	if !finalizationInspectionBudgetExhausted(true, maxFinalizationInspectionCalls) {
		t.Fatal("inspection budget should be exhausted at its configured limit")
	}
	if finalizationInspectionBudgetExhausted(false, maxFinalizationInspectionCalls) {
		t.Fatal("inspection budget should apply only during finalization")
	}
}

func TestFinalizationInspectionBudgetBlocksAtExecutionBoundary(t *testing.T) {
	readCalled := false
	editCalled := false
	testCalled := false
	registry, err := NewToolRegistry([]Tool{
		{Definition: ToolDefinition{Name: "read_file"}, Handle: func(context.Context, json.RawMessage) (string, error) {
			readCalled = true
			return "read", nil
		}},
		{Definition: ToolDefinition{Name: "edit_file"}, Handle: func(context.Context, json.RawMessage) (string, error) {
			editCalled = true
			return "edit", nil
		}},
		{Definition: ToolDefinition{Name: "run_command"}, Handle: func(context.Context, json.RawMessage) (string, error) {
			testCalled = true
			return "test", nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{Tools: registry, finalizationInspectionBudgetExhausted: true}
	readResult := session.executeTool(context.Background(), 12, 10, true, false, true, false,
		ContentBlock{Type: "tool_use", ID: "read", Name: "read_file", Input: json.RawMessage(`{"path":"x.py"}`)}, time.Second, 1024)
	if readResult.IsError || !readCalled {
		t.Fatalf("read_file result = %#v, handler called = %t; want a targeted read allowed during review", readResult, readCalled)
	}
	editResult := session.executeTool(context.Background(), 12, 10, true, false, true, false,
		ContentBlock{Type: "tool_use", ID: "edit", Name: "edit_file", Input: json.RawMessage(`{"path":"x.py"}`)}, time.Second, 1024)
	if editResult.IsError || !editCalled {
		t.Fatalf("edit_file result = %#v, handler called = %t; want a targeted repair allowed during review", editResult, editCalled)
	}
	testResult := session.executeTool(context.Background(), 12, 10, true, true, false, false,
		ContentBlock{Type: "tool_use", ID: "test", Name: "run_command", Input: json.RawMessage(`{"purpose":"test"}`)}, time.Second, 1024)
	if testResult.IsError || !testCalled {
		t.Fatalf("test command result = %#v, handler called = %t; want allowed", testResult, testCalled)
	}
}

func toolNames(definitions []ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func TestIsVerificationCheckRecognizesOnlyTestCommands(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "git_diff", want: true},
		{name: "run_command", input: `{"purpose":"test"}`, want: true},
		{name: "build command", input: `{"purpose":"build"}`},
		{name: "source edit"},
	}
	for _, test := range tests {
		var input json.RawMessage
		if test.input != "" {
			input = json.RawMessage(test.input)
		}
		got := isVerificationCheck(test.name, input)
		if got != test.want {
			t.Errorf("isVerificationCheck(%q, %s) = %t, want %t", test.name, test.input, got, test.want)
		}
	}
}

func TestIsVerificationGateRejectionRecognizesBlockedCalls(t *testing.T) {
	tests := []struct {
		name   string
		result ContentBlock
		want   bool
	}{
		{name: "outstanding check only", result: ContentBlock{IsError: true, Content: "verification phase only allows checks still outstanding after the latest edit"}, want: true},
		{name: "already complete", result: ContentBlock{IsError: true, Content: "verification is complete for the unchanged source"}, want: true},
		{name: "real test failure", result: ContentBlock{IsError: true, Content: "SIGMA_RUN_COMMAND run_command purpose=test status=failed exit_code=1"}},
		{name: "successful test", result: ContentBlock{Content: "SIGMA_RUN_COMMAND run_command purpose=test status=passed exit_code=0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isVerificationGateRejection(test.result); got != test.want {
				t.Fatalf("isVerificationGateRejection() = %t, want %t", got, test.want)
			}
		})
	}
}
