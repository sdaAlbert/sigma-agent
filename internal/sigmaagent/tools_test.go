package sigmaagent

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestTestPurposeCommandsPreserveExitStatus(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{name: "plain test command", command: "python3 -m pytest tests/utils_tests/test_http.py"},
		{name: "setup chained with and", command: "cd /workspace && python3 -m unittest tests.test_http"},
		{name: "quoted pipe is data", command: `python3 -c "print('|')"`},
		{name: "pipeline can mask failure", command: "python3 -m pytest tests | tail -20", wantErr: true},
		{name: "fallback can mask failure", command: "python3 -m pytest tests || echo passed", wantErr: true},
		{name: "semicolon can mask failure", command: "python3 -m pytest tests; echo passed", wantErr: true},
		{name: "stderr redirection is safe", command: "python3 -m pytest tests 2>&1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTestCommandExitStatus(test.command)
			if test.wantErr && err == nil {
				t.Fatalf("validateTestCommandExitStatus(%q) = nil, want error", test.command)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateTestCommandExitStatus(%q) error = %v", test.command, err)
			}
		})
	}
}

// The command network can be kept internal when the operator disables egress.
func TestIsolatedTestNetworkIsInternalUnlessNetworkIsEnabled(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available in this environment")
	}
	off, err := createIsolatedTestNetwork(context.Background(), false)
	if err != nil {
		t.Fatalf("createIsolatedTestNetwork(false) error = %v", err)
	}
	defer removeIsolatedTestNetwork(off)
	inspect, err := exec.Command("docker", "network", "inspect", off).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect %s: %v: %s", off, err, strings.TrimSpace(string(inspect)))
	}
	if !strings.Contains(string(inspect), `"Internal": true`) {
		t.Fatalf("network %s is not internal, want an internal network with no external egress", off)
	}
}

func TestIsolatedTestNetworkUsesBridgeWhenNetworkIsEnabled(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available in this environment")
	}
	on, err := createIsolatedTestNetwork(context.Background(), true)
	if err != nil {
		t.Fatalf("createIsolatedTestNetwork(true) error = %v", err)
	}
	defer removeIsolatedTestNetwork(on)
	inspect, err := exec.Command("docker", "network", "inspect", on).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect %s: %v: %s", on, err, strings.TrimSpace(string(inspect)))
	}
	if !strings.Contains(string(inspect), `"Internal": false`) {
		t.Fatalf("network %s is internal, want a bridge network with egress when the session opted in", on)
	}
}

// run_command must advertise the container network mode so the model knows
// whether it can download dependencies.
func TestRunCommandDescribesTheConfiguredNetworkMode(t *testing.T) {
	off := workspace{root: t.TempDir(), allowNetwork: false}.runCommandTool("debian:trixie-slim")
	if !strings.Contains(off.Definition.Description, "no external network") {
		t.Fatalf("run_command description does not report the disabled network: %q", off.Definition.Description)
	}
	on := workspace{root: t.TempDir(), allowNetwork: true}.runCommandTool("golang:1.26")
	if !strings.Contains(on.Definition.Description, "internet access is enabled") {
		t.Fatalf("run_command description does not report the enabled network: %q", on.Definition.Description)
	}
}
