package agent

import (
	"reflect"
	"testing"
)

// replaceEnv backs the GITHUB_READ_ONLY / GITHUB_TOOLSETS scoping that keeps
// read-only sessions away from write tools.
func TestReplaceEnvOverridesGitHubToolsetVariables(t *testing.T) {
	environment := []string{"PATH=/usr/bin", "GITHUB_TOOLSETS=issues", "OTHER=1"}
	got := replaceEnv(environment, map[string]string{
		"GITHUB_TOOLSETS":  "issues,repos,pull_requests",
		"GITHUB_READ_ONLY": "1",
	})
	want := []string{"PATH=/usr/bin", "OTHER=1", "GITHUB_READ_ONLY=1", "GITHUB_TOOLSETS=issues,repos,pull_requests"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replaceEnv() = %#v, want %#v", got, want)
	}
}
