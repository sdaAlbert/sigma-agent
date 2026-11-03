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

// GitHubMCPOptions scopes a GitHub MCP session.
type GitHubMCPOptions struct {
	AllowCreatePullRequest bool
	Owner                  string
	Repository             string
	HeadBranch             string
}

// StartGitHubMCP starts the pinned official GitHub MCP server in Docker with
// only the requested toolsets enabled. Credentials come from the user's saved
// configuration, the environment, then .secrets/github-pat in the working directory.
func StartGitHubMCP(ctx context.Context, options ...GitHubMCPOptions) (*GitHubMCP, error) {
	token, err := GitHubMCPToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("no GitHub token is configured; use /login github or set GITHUB_PERSONAL_ACCESS_TOKEN")
	}
	var selected GitHubMCPOptions
	if len(options) > 0 {
		selected = options[0]
	}
	if selected.AllowCreatePullRequest && (selected.Owner == "" || selected.Repository == "" || selected.HeadBranch == "") {
		return nil, errors.New("GitHub PR creation requires an origin repository and configured push branch")
	}
	return startGitHubMCP(ctx, token, selected)
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
	writeOptions GitHubMCPOptions
}

func startGitHubMCP(ctx context.Context, token string, options GitHubMCPOptions) (*GitHubMCP, error) {
	environment := map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": token}
	dockerArgs := []string{"run", "--rm", "-i", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN"}
	if options.AllowCreatePullRequest {
		environment["GITHUB_TOOLS"] = "get_file_contents,issue_read,list_issues,search_issues,pull_request_read,list_pull_requests,search_pull_requests,search_code,search_repositories,get_commit,list_commits,list_branches,create_pull_request"
		dockerArgs = append(dockerArgs, "-e", "GITHUB_TOOLS")
	} else {
		environment["GITHUB_READ_ONLY"] = "1"
		environment["GITHUB_TOOLSETS"] = "issues,repos,pull_requests"
		dockerArgs = append(dockerArgs, "-e", "GITHUB_READ_ONLY", "-e", "GITHUB_TOOLSETS")
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
	return &GitHubMCP{Client: client, writeOptions: options}, nil
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

// callTool invokes a GitHub MCP tool. Pull request creation is refused unless
// the session explicitly enabled it and the request stays inside the
// configured repository and branch.
func (c *GitHubMCP) callTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	if name == "create_pull_request" {
		if !c.writeOptions.AllowCreatePullRequest {
			return "", errors.New("GitHub pull request creation is not enabled for this session")
		}
		var args struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
			Head  string `json:"head"`
		}
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", fmt.Errorf("decode pull request scope: %w", err)
		}
		if !strings.EqualFold(args.Owner, c.writeOptions.Owner) || !strings.EqualFold(args.Repo, c.writeOptions.Repository) {
			return "", errors.New("pull request creation is restricted to the repository configured for this session")
		}
		if args.Head != c.writeOptions.HeadBranch && args.Head != c.writeOptions.Owner+":"+c.writeOptions.HeadBranch {
			return "", errors.New("pull request head must be the feature branch configured for this session")
		}
	}
	if c.Client == nil {
		return "", errors.New("GitHub MCP server is not connected")
	}
	return c.Client.CallTool(ctx, name, arguments)
}

// Close shuts the GitHub MCP server down.
func (c *GitHubMCP) Close() error {
	if c == nil || c.Client == nil {
		return nil
	}
	return c.Client.Close()
}
