package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/mcp"
)

const githubMCPImage = "ghcr.io/github/github-mcp-server@sha256:250bc6ce70788207608ef74cef4397230ff2e8481218da74d6093e4bd9101132"

// StartGitHubMCP starts the pinned official GitHub MCP server in Docker with
// read-only repository and issue tools. Credentials come from the user's saved
// configuration, the environment, then .secrets/github-pat in the working directory.
func StartGitHubMCP(ctx context.Context) (*GitHubMCP, error) {
	token, err := GitHubMCPToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("no GitHub token is configured; use /login github or set GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	return startGitHubMCP(ctx, token, nil)
}

type GitHubPullRequestScope struct {
	Owner      string
	Repository string
	HeadBranch string
	BaseBranch string
}

// StartGitHubMCPForDraftPR exposes only PR creation for one repository and
// feature branch. Callers must first receive explicit user authorization.
func StartGitHubMCPForDraftPR(ctx context.Context, scope GitHubPullRequestScope) (*GitHubMCP, error) {
	if scope.Owner == "" || scope.Repository == "" || scope.HeadBranch == "" || scope.BaseBranch == "" {
		return nil, errors.New("draft PR creation requires an owner, repository, head branch, and base branch")
	}
	token, err := GitHubMCPToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("no GitHub token is configured; use /login github or set GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	return startGitHubMCP(ctx, token, &scope)
}

// GitHubMCPConfigured reports whether a non-empty GitHub token is available
// from saved user configuration, the environment, or a project-local secret file.
func GitHubMCPConfigured() bool {
	token, err := GitHubMCPToken()
	return err == nil && token != ""
}

// GitHubMCP owns the Docker-backed stdio connection to GitHub's read-only MCP server.
type GitHubMCP struct {
	*mcp.Client
	pullRequestScope *GitHubPullRequestScope
}

func startGitHubMCP(ctx context.Context, token string, scope *GitHubPullRequestScope) (*GitHubMCP, error) {
	environment := map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": token}
	dockerArgs := []string{"run", "--rm", "-i", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN"}
	if scope == nil {
		environment["GITHUB_READ_ONLY"] = "1"
		environment["GITHUB_TOOLSETS"] = "issues,repos,pull_requests"
		dockerArgs = append(dockerArgs, "-e", "GITHUB_READ_ONLY", "-e", "GITHUB_TOOLSETS")
	} else {
		environment["GITHUB_TOOLS"] = "create_pull_request"
		dockerArgs = append(dockerArgs, "-e", "GITHUB_TOOLS")
	}
	dockerArgs = append(dockerArgs, githubMCPImage)
	client, err := mcp.Start(ctx, mcp.Config{
		Name:    "github",
		Command: "docker",
		Args:    dockerArgs,
		Env:     environment,
	})
	if err != nil {
		return nil, err
	}
	return &GitHubMCP{Client: client, pullRequestScope: scope}, nil
}

// replaceEnv backs the GITHUB_READ_ONLY / GITHUB_TOOLSETS scoping that keeps
// read-only sessions away from write tools.
func replaceEnv(environment []string, values map[string]string) []string {
	return mcp.ReplaceEnv(environment, values)
}

// Tools lists the GitHub MCP tools as namespaced Agent tools.
func (c *GitHubMCP) Tools(ctx context.Context) ([]Tool, error) {
	if c.Client == nil {
		return nil, errors.New("GitHub MCP server is not connected")
	}
	return MCPTools(ctx, c.Client, MCPToolOptions{
		Server:   "github",
		CallTool: c.callTool,
		Describe: func(toolName, description string) string {
			return fmt.Sprintf("GitHub MCP tool %q: %s", toolName, description)
		},
	})
}

// callTool invokes one of the read-only GitHub MCP tools.
func (c *GitHubMCP) callTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	if c.Client == nil {
		return "", errors.New("GitHub MCP server is not connected")
	}
	if c.pullRequestScope == nil {
		if name == "create_pull_request" {
			return "", errors.New("pull request creation is disabled for this GitHub connection")
		}
		return c.Client.CallTool(ctx, name, arguments)
	}
	if name != "create_pull_request" {
		return "", errors.New("this GitHub connection only permits scoped draft pull request creation")
	}
	if err := validateGitHubDraftPRArguments(*c.pullRequestScope, arguments); err != nil {
		return "", err
	}
	return c.Client.CallTool(ctx, name, arguments)
}

func validateGitHubDraftPRArguments(scope GitHubPullRequestScope, arguments json.RawMessage) error {
	var target struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}
	if err := json.Unmarshal(arguments, &target); err != nil {
		return fmt.Errorf("decode pull request scope: %w", err)
	}
	if !strings.EqualFold(target.Owner, scope.Owner) || !strings.EqualFold(target.Repo, scope.Repository) {
		return errors.New("pull request creation is restricted to the configured repository")
	}
	if target.Head != scope.HeadBranch && target.Head != scope.Owner+":"+scope.HeadBranch {
		return errors.New("pull request head must match the configured feature branch")
	}
	if target.Base != scope.BaseBranch || target.Base == target.Head {
		return errors.New("pull request base must match the configured base branch and differ from its head")
	}
	if !target.Draft {
		return errors.New("this GitHub connection only permits draft pull requests")
	}
	return nil
}

// Close shuts the GitHub MCP server down.
func (c *GitHubMCP) Close() error {
	if c == nil || c.Client == nil {
		return nil
	}
	return c.Client.Close()
}
