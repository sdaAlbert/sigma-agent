package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFinalizationToolDefinitionsKeepToolsAvailable(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_file"},
		{Name: "record_change_contract"},
		{Name: "run_command"},
		{Name: "git_diff"},
		{Name: "search_code"},
		{Name: "list_files"},
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
	}{
		{name: "checks outstanding", reviewOnly: true, testsNeeded: true, diffNeeded: true},
		{name: "review complete", reviewOnly: true},
		{name: "verified source", verificationComplete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotDefinitions := finalizationToolDefinitions(definitions, test.reviewOnly, test.testsNeeded, test.diffNeeded, test.verificationComplete)
			got := make([]string, 0, len(gotDefinitions))
			for _, definition := range gotDefinitions {
				got = append(got, definition.Name)
			}
			if !reflect.DeepEqual(got, toolNames(definitions)) {
				t.Fatalf("tools = %v, want all tools %v", got, toolNames(definitions))
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

func TestRequiredSkillBootstrapKeepsToolSchemaStable(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_skill"},
		{Name: "search_memory"},
		{Name: "list_files"},
		{Name: "search_code"},
		{Name: "run_command"},
	}
	got := requiredSkillBootstrapTools(definitions, []string{"python-issue-workflow"})
	if !reflect.DeepEqual(toolNames(got), []string{"read_skill", "search_memory", "list_files", "search_code", "run_command"}) {
		t.Fatalf("bootstrap tools = %v, want the stable full tool schema", toolNames(got))
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

func toolNames(definitions []ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}
