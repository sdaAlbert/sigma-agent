package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyToolAuthorizationRiskClasses(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		network       bool
		wantClass     string
		wantTarget    string
		wantReasonHas string
	}{
		{name: "read file", input: `{"path":"src/main.go"}`, wantClass: RiskReadOnly, wantTarget: "src/main.go"},
		{name: "file write", input: `{"path":"src/main.go"}`, wantClass: RiskWorkspaceWrite, wantTarget: "src/main.go"},
		{name: "git status", input: `{"action":"status"}`, wantClass: RiskReadOnly, wantTarget: "status"},
		{name: "git commit", input: `{"action":"commit","paths":["README.md"]}`, wantClass: RiskCommitPublish, wantTarget: `commit ["README.md"]`},
		{name: "prepare docker image", input: `{"base_image":"debian:trixie-slim"}`, wantClass: RiskExternalService, wantTarget: "debian:trixie-slim"},
		{name: "download command", input: `{"command":"curl https://example.invalid/file"}`, wantClass: RiskNetwork, wantTarget: "curl https://example.invalid/file"},
		{name: "network-enabled commands", input: `{"command":"sh tests/run.sh"}`, network: true, wantClass: RiskNetwork, wantTarget: "sh tests/run.sh"},
		{name: "push command", input: `{"command":"git push origin main"}`, network: true, wantClass: RiskCommitPublish, wantTarget: "git push origin main"},
		{name: "third-party tool", input: `{"query":"x"}`, wantClass: RiskExternalService, wantTarget: "mcp__server__tool"},
	}
	toolNames := map[string]string{
		"read file": "read_file", "file write": "edit_file", "git status": "git", "git commit": "git",
		"prepare docker image": "prepare_environment", "download command": "run_command",
		"network-enabled commands": "run_command", "push command": "run_command", "third-party tool": "mcp__server__tool",
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := ClassifyToolAuthorization(toolNames[test.name], json.RawMessage(test.input), test.network)
			if request.RiskClass != test.wantClass {
				t.Fatalf("risk class = %q, want %q", request.RiskClass, test.wantClass)
			}
			if request.Target != test.wantTarget {
				t.Fatalf("target = %q, want %q", request.Target, test.wantTarget)
			}
			if test.wantReasonHas != "" && !strings.Contains(request.Reason, test.wantReasonHas) {
				t.Fatalf("reason = %q, want it to contain %q", request.Reason, test.wantReasonHas)
			}
			if !json.Valid(request.Args) {
				t.Fatalf("authorization args are not valid JSON: %s", request.Args)
			}
		})
	}
}

func TestClassifyToolAuthorizationBoundsLargeArguments(t *testing.T) {
	request := ClassifyToolAuthorization("run_command", json.RawMessage(`{"command":"`+strings.Repeat("x", 5000)+`"}`), false)
	if request.RiskClass != RiskWorkspaceWrite {
		t.Fatalf("risk class = %q, want workspace_write", request.RiskClass)
	}
	if string(request.Args) != `{"truncated":true}` {
		t.Fatalf("large authorization args = %s, want bounded truncation marker", request.Args)
	}
	if len([]rune(request.Target)) > 241 {
		t.Fatalf("large target length = %d, want at most 241 runes", len([]rune(request.Target)))
	}
}
