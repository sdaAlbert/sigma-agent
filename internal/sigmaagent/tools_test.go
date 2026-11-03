package sigmaagent

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestGoTestArgs(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		pkg     string
		want    []string
		wantErr bool
	}{
		{name: "whole module tests are bounded", profile: "go-test", want: []string{"go", "test", "-p", "2", "./..."}},
		{name: "focused package tests are bounded", profile: "go-test-package", pkg: "./core/mapping", want: []string{"go", "test", "-p", "2", "./core/mapping"}},
		{name: "vet is bounded", profile: "go-vet", want: []string{"go", "vet", "-p", "2", "./..."}},
		{name: "rejects parent path", profile: "go-test-package", pkg: "..", wantErr: true},
		{name: "rejects unknown profile", profile: "go-build", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := goTestArgs(test.profile, test.pkg)
			if test.wantErr {
				if err == nil {
					t.Fatalf("goTestArgs(%q, %q) = %#v, want error", test.profile, test.pkg, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("goTestArgs(%q, %q) error = %v", test.profile, test.pkg, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("goTestArgs(%q, %q) = %#v, want %#v", test.profile, test.pkg, got, test.want)
			}
		})
	}
}

// The shipped code no longer exposes a networkArgs helper: the Docker test
// network is created through createIsolatedTestNetwork, which is internal-only
// by default and bridge-connected only when the session opted into egress.
//
// This test keeps the original safety property - networking must be off unless
// the operator explicitly opted in - by asserting on the docker network create
// arguments that the helper builds.
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

// The Go test container must only download modules when the session enabled
// container network; otherwise the offline image must be used as-is.
func TestContainerCommandDownloadsModulesOnlyWhenNetworkIsEnabled(t *testing.T) {
	goArgs := []string{"go", "test", "./core/mapping"}
	off := goTestContainerArgs(goArgs, false)
	if !reflect.DeepEqual(off, goArgs) {
		t.Fatalf("goTestContainerArgs(off) = %#v, want %#v", off, goArgs)
	}
	on := goTestContainerArgs(goArgs, true)
	if len(on) != 3 || on[0] != "/bin/sh" || on[1] != "-lc" {
		t.Fatalf("goTestContainerArgs(on) = %#v, want a /bin/sh -lc invocation", on)
	}
	if !strings.Contains(on[2], "go mod download") {
		t.Fatalf("goTestContainerArgs(on) = %#v, want the command to download modules first", on)
	}
	if !strings.HasSuffix(on[2], strings.Join(goArgs, " ")) {
		t.Fatalf("goTestContainerArgs(on) = %#v, want the command to end with %#v", on, goArgs)
	}
}

func TestContainerRunArgsAllowTemporaryBuildBinaries(t *testing.T) {
	args := containerRunArgs("sigma-test", "sigma-network")
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--tmpfs" && args[i+1] == "/tmp:rw,exec,nosuid,size=256m" {
			return
		}
	}
	t.Fatalf("containerRunArgs() = %#v, want an executable /tmp tmpfs for Go and native test binaries", args)
}

// run_command must advertise the container network mode so the model does not
// assume it can download dependencies when the session kept networking off.
func TestRunCommandDescribesTheConfiguredNetworkMode(t *testing.T) {
	off := workspace{root: t.TempDir()}.runCommandTool("golang:1.26")
	if !strings.Contains(off.Definition.Description, "no external network") {
		t.Fatalf("run_command description does not report the disabled network: %q", off.Definition.Description)
	}
	on := workspace{root: t.TempDir(), allowNetwork: true}.runCommandTool("golang:1.26")
	if !strings.Contains(on.Definition.Description, "internet access is enabled") {
		t.Fatalf("run_command description does not report the enabled network: %q", on.Definition.Description)
	}
}
