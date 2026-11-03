package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// The shipped GitHub MCP client no longer exposes a separate read-tool
// allowlist helper: read-only sessions are enforced by the server-side
// GITHUB_READ_ONLY/GITHUB_TOOLSETS environment, while write access is gated
// by GitHubMCPOptions and enforced in (*GitHubMCP).callTool.
//
// This test keeps the original safety property - read-only sessions must never
// be able to create a pull request - but asserts it against the shipped API.
func TestGitHubMCPCallToolRejectsPullRequestWhenWriteIsDisabled(t *testing.T) {
	client := &GitHubMCP{writeOptions: GitHubMCPOptions{Owner: "sdaAlbert", Repository: "sigma-agent", HeadBranch: "feature"}}
	arguments := json.RawMessage(`{"owner":"sdaAlbert","repo":"sigma-agent","head":"feature"}`)
	if _, err := client.callTool(context.Background(), "create_pull_request", arguments); err == nil {
		t.Fatal("callTool(create_pull_request) succeeded with pull request creation disabled, want error")
	}
}

func TestGitHubMCPCallToolRejectsPullRequestOutsideTheConfiguredScope(t *testing.T) {
	client := &GitHubMCP{writeOptions: GitHubMCPOptions{
		AllowCreatePullRequest: true, Owner: "sdaAlbert", Repository: "sigma-agent", HeadBranch: "feature",
	}}
	tests := []struct {
		name      string
		arguments string
	}{
		{name: "different owner", arguments: `{"owner":"attacker","repo":"sigma-agent","head":"feature"}`},
		{name: "different repository", arguments: `{"owner":"sdaAlbert","repo":"other-repo","head":"feature"}`},
		{name: "different head branch", arguments: `{"owner":"sdaAlbert","repo":"sigma-agent","head":"main"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.callTool(context.Background(), "create_pull_request", json.RawMessage(test.arguments)); err == nil {
				t.Fatalf("callTool(create_pull_request, %s) succeeded, want a scope error", test.arguments)
			}
		})
	}
}

func TestGitHubMCPWriteOptionsRequireRepositoryAndBranch(t *testing.T) {
	if _, err := StartGitHubMCP(context.Background(), GitHubMCPOptions{AllowCreatePullRequest: true}); err == nil {
		t.Fatal("StartGitHubMCP succeeded without an origin repository and branch, want error")
	}
}

func TestRepositoryFromRemoteURL(t *testing.T) {
	for input, want := range map[string]string{
		"https://github.com/sdaAlbert/sigma-agent.git": "sdaAlbert/sigma-agent",
		"git@github.com:sdaAlbert/sigma-agent.git":     "sdaAlbert/sigma-agent",
	} {
		got, err := RepositoryFromRemoteURL(input)
		if err != nil || got != want {
			t.Errorf("RepositoryFromRemoteURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

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
