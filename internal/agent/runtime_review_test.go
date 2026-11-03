package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFinalizationToolDefinitionsExposeOnlyMissingChecks(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "read_file"},
		{Name: "run_tests"},
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
		{name: "both missing", reviewOnly: true, testsNeeded: true, diffNeeded: true, want: []string{"run_tests", "git_diff"}},
		{name: "only tests missing", reviewOnly: true, testsNeeded: true, want: []string{"run_tests"}},
		{name: "only diff missing", reviewOnly: true, diffNeeded: true, want: []string{"git_diff"}},
		{name: "review complete", reviewOnly: true, want: []string{}},
		{name: "verified source allows correction but no repeated checks", verificationComplete: true, want: []string{"read_file", "create_file", "edit_file", "apply_patch"}},
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

func TestReviewToolNeededOnlyAllowsOutstandingCheck(t *testing.T) {
	if !reviewToolNeeded("git_diff", false, true) {
		t.Fatal("git_diff should be allowed when it is the only outstanding check")
	}
	if reviewToolNeeded("run_tests", false, true) {
		t.Fatal("run_tests should be blocked after it has completed")
	}
	if reviewToolNeeded("edit_file", true, true) {
		t.Fatal("source writes should be blocked during review")
	}
	if reviewToolNeeded("run_tests", false, false) || reviewToolNeeded("git_diff", false, false) {
		t.Fatal("no review tool should be allowed after all checks complete")
	}
}

func TestToolDefinitionFilteringPreservesDefinitions(t *testing.T) {
	definitions := []ToolDefinition{
		{Name: "run_tests", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "git_diff", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	got := finalizationToolDefinitions(definitions, true, true, true, false)
	if !reflect.DeepEqual(got, definitions) {
		t.Fatalf("definitions = %#v, want %#v", got, definitions)
	}
}

func TestIsVerificationCheckRecognizesOnlyTestCommands(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "run_tests", want: true},
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
		{name: "real test failure", result: ContentBlock{IsError: true, Content: "run_tests status=failed exit_code=1"}},
		{name: "successful test", result: ContentBlock{Content: "run_tests status=passed exit_code=0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isVerificationGateRejection(test.result); got != test.want {
				t.Fatalf("isVerificationGateRejection() = %t, want %t", got, test.want)
			}
		})
	}
}
