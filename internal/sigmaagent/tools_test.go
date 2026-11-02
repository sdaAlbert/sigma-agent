package sigmaagent

import (
	"reflect"
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
		{name: "rejects parent path", profile: "go-test-package", pkg: "../outside", wantErr: true},
		{name: "rejects shell-like input", profile: "go-test-package", pkg: "./core;whoami", wantErr: true},
		{name: "requires package", profile: "go-test-package", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goTestArgs(tt.profile, tt.pkg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("goTestArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("goTestArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNetworkArgsAreOffByDefault(t *testing.T) {
	got := networkArgs(false)
	want := []string{"--network", "none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("networkArgs(false) = %#v, want %#v", got, want)
	}
}

func TestNetworkArgsEnableOutboundNetworkWhenOptedIn(t *testing.T) {
	got := networkArgs(true)
	want := []string{"--network", "bridge"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("networkArgs(true) = %#v, want %#v", got, want)
	}
}

func TestContainerCommandDownloadsModulesOnlyWhenNetworkIsEnabled(t *testing.T) {
	goArgs := []string{"go", "test", "./core/mapping"}
	off := containerCommand(false, goArgs)
	if !reflect.DeepEqual(off, goArgs) {
		t.Fatalf("containerCommand(false) = %#v, want %#v", off, goArgs)
	}
	on := containerCommand(true, goArgs)
	want := []string{"sh", "-c", `go mod download && exec "$@"`, "sigma-test-command", "go", "test", "./core/mapping"}
	if !reflect.DeepEqual(on, want) {
		t.Fatalf("containerCommand(true) = %#v, want %#v", on, want)
	}
}

func TestNetworkModeLabelReflectsTheConfiguredMode(t *testing.T) {
	if label := networkModeLabel(false); label != "networking disabled" {
		t.Fatalf("networkModeLabel(false) = %q, want %q", label, "networking disabled")
	}
	if label := networkModeLabel(true); label != "outbound network enabled" {
		t.Fatalf("networkModeLabel(true) = %q, want %q", label, "outbound network enabled")
	}
}
