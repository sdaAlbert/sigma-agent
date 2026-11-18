package agent

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestValidateGitHubDraftPRArguments(t *testing.T) {
	scope := GitHubPullRequestScope{Owner: "example", Repository: "sigma", HeadBranch: "feature/task-1", BaseBranch: "main"}
	valid := json.RawMessage(`{"owner":"example","repo":"sigma","head":"feature/task-1","base":"main","draft":true}`)
	if err := validateGitHubDraftPRArguments(scope, valid); err != nil {
		t.Fatalf("valid scoped draft PR: %v", err)
	}

	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "different repo", args: `{"owner":"example","repo":"other","head":"feature/task-1","base":"main","draft":true}`, want: "configured repository"},
		{name: "different head", args: `{"owner":"example","repo":"sigma","head":"feature/other","base":"main","draft":true}`, want: "feature branch"},
		{name: "different base", args: `{"owner":"example","repo":"sigma","head":"feature/task-1","base":"develop","draft":true}`, want: "configured base branch"},
		{name: "not draft", args: `{"owner":"example","repo":"sigma","head":"feature/task-1","base":"main","draft":false}`, want: "only permits draft"},
		{name: "invalid JSON", args: `{`, want: "decode pull request scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateGitHubDraftPRArguments(scope, json.RawMessage(test.args))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want message containing %q", err, test.want)
			}
		})
	}
}
