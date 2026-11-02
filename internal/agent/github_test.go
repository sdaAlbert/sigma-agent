package agent

import (
	"reflect"
	"testing"
)

func TestGitHubMCPReadToolAllowlistExcludesWrites(t *testing.T) {
	for _, name := range []string{"get_file_contents", "issue_read", "search_code", "pull_request_read", "list_branches"} {
		if !isGitHubReadTool(name) {
			t.Errorf("isGitHubReadTool(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"create_pull_request", "merge_pull_request", "create_issue", "push_files", "delete_branch"} {
		if isGitHubReadTool(name) {
			t.Errorf("isGitHubReadTool(%q) = true, want false", name)
		}
	}
}

func TestCreatePullRequestRequestUsesTopLevelGitHubMCPFields(t *testing.T) {
	got := createPullRequestRequest(GitHubWriteOptions{
		Repository: "sdaAlbert/sigma-agent",
		HeadBranch: "sigma/readme-issue-to-pr-20261102",
		BaseBranch: "main",
	}, "Sigma Agent: feature", "Summary")
	want := map[string]any{
		"owner": "sdaAlbert", "repo": "sigma-agent", "head": "sigma/readme-issue-to-pr-20261102",
		"base": "main", "title": "Sigma Agent: feature", "body": "Summary",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("createPullRequestRequest() = %#v, want %#v", got, want)
	}
	if _, nested := got["pullRequest"]; nested {
		t.Fatal("request unexpectedly nests fields under pullRequest")
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
