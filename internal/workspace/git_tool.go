package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

func (w workspace) gitTool() agent.Tool {
	description := "Perform local Git operations in the current workspace without Docker: status, diff, log, show, list branches, initialize a repository, stage selected paths, or commit staged changes. Git runs on the Sigma host with the workspace as its fixed working directory. This tool never contacts remotes. Do not initialize, stage, or commit unless the user asks for that Git operation."
	schema := `{"type":"object","properties":{"action":{"type":"string","enum":["status","diff","log","show","branches","init","stage","commit"]},"paths":{"type":"array","items":{"type":"string","minLength":1,"maxLength":1024},"maxItems":128},"message":{"type":"string","maxLength":4096},"author_name":{"type":"string","maxLength":256},"author_email":{"type":"string","maxLength":256}},"required":["action"],"additionalProperties":false}`
	return tool("git", description, schema, "sequential", func(ctx context.Context, raw json.RawMessage) (string, error) {
		if err := w.ensureNoActiveBackgroundTask(); err != nil {
			return "", err
		}
		var args struct {
			Action      string   `json:"action"`
			Paths       []string `json:"paths"`
			Message     string   `json:"message"`
			AuthorName  string   `json:"author_name"`
			AuthorEmail string   `json:"author_email"`
		}
		if err := decodeArgs(raw, &args); err != nil {
			return "", err
		}
		if args.Action == "status" && !IsGitRepository(w.root) {
			return "This workspace is not a Git repository; Git status was skipped. Filesystem tools can still work directly in this folder.", nil
		}
		if args.Action != "init" {
			if err := w.requireGitWorkspaceRoot(ctx); err != nil {
				return "", err
			}
		}
		switch args.Action {
		case "status":
			return w.gitStatus(ctx)
		case "diff":
			if !HasGitCommit(w.root) {
				return "This folder has no Git commit to compare against. Ask Sigma to initialize Git and create a baseline commit if you want Git-based diffs.", nil
			}
			diff, err := WorkspaceDiff(w.root, w.diffBase)
			return diff, err
		case "log":
			return w.runGit(ctx, "log", "-10", "--oneline", "--decorate")
		case "show":
			return w.runGit(ctx, "show", "--stat", "--oneline", "HEAD")
		case "branches":
			return w.runGit(ctx, "branch", "--list")
		case "init":
			if IsGitRepository(w.root) {
				return "This folder is already inside a Git repository.", nil
			}
			return w.runGit(ctx, "init")
		case "stage":
			if len(args.Paths) == 0 {
				return "stage requires one or more workspace-relative paths", errors.New("missing Git paths")
			}
			paths, err := validateGitPaths(args.Paths)
			if err != nil {
				return "", err
			}
			if _, err := w.runGit(ctx, "reset", "-q", "--", agentManagedMemoryPath, sigmaContainerHomePath, sigmaCommandEnvironmentPath, sigmaRunArtifactsPath, sigmaSessionArtifactsPath); err != nil {
				return "", err
			}
			stageArgs := append([]string{"add", "-A", "--"}, paths...)
			stageArgs = append(stageArgs, ":(exclude)"+agentManagedMemoryPath, ":(exclude)"+sigmaContainerHomePath, ":(exclude)"+sigmaCommandEnvironmentPath, ":(exclude)"+sigmaRunArtifactsPath, ":(exclude)"+sigmaSessionArtifactsPath)
			return w.runGit(ctx, stageArgs...)
		case "commit":
			message := strings.TrimSpace(args.Message)
			if message == "" || strings.ContainsAny(message, "\r\n\x00") {
				return "", errors.New("commit requires a single-line message")
			}
			if (args.AuthorName == "") != (args.AuthorEmail == "") || strings.ContainsAny(args.AuthorName+args.AuthorEmail, "\r\n\x00") {
				return "", errors.New("author_name and author_email must both be supplied as single-line values")
			}
			commandArgs := []string{}
			if args.AuthorName != "" {
				commandArgs = append(commandArgs, "-c", "user.name="+args.AuthorName, "-c", "user.email="+args.AuthorEmail)
			}
			commandArgs = append(commandArgs, "commit", "-m", message)
			return w.runGit(ctx, commandArgs...)
		default:
			return "", fmt.Errorf("unsupported Git action %q", args.Action)
		}
	})
}

func (w workspace) requireGitWorkspaceRoot(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, "git", "rev-parse", "--show-toplevel")
	command.Dir = w.root
	output, err := command.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return errors.New("Git is not installed or not available on the Sigma host PATH")
		}
		return errors.New("the selected workspace is not inside a Git repository")
	}
	root, err := filepath.Abs(strings.TrimSpace(string(output)))
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	workspaceRoot, err := filepath.EvalSymlinks(w.root)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(workspaceRoot)) {
		return errors.New("the selected workspace is inside a parent Git repository; run Sigma from the repository root for Git operations")
	}
	return nil
}

func (w workspace) runGit(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, "git", args...)
	command.Dir = w.root
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if commandCtx.Err() == context.DeadlineExceeded {
			return text, errors.New("Git command timed out after 30 seconds")
		}
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			return "", errors.New("Git is not installed or not available on the Sigma host PATH")
		}
		return text, fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	if text == "" {
		text = "Git command completed successfully."
	}
	if len(text) > 64*1024 {
		text = text[:64*1024] + "\n[output truncated]"
	}
	return text, nil
}

func (w workspace) gitStatus(ctx context.Context) (string, error) {
	output, err := w.runGit(ctx, "status", "--porcelain=v1", "--branch", "--untracked-files=all", "-z")
	if err != nil {
		return output, err
	}
	parts := strings.Split(output, "\x00")
	lines := make([]string, 0, len(parts))
	for index := 0; index < len(parts); index++ {
		entry := parts[index]
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "## ") {
			lines = append(lines, entry)
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			continue
		}
		path := entry[3:]
		status := entry[:2]
		if status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C' {
			if index+1 < len(parts) {
				oldPath := parts[index+1]
				index++
				if isSigmaManagedWorkspacePath(path) || isSigmaManagedWorkspacePath(oldPath) {
					continue
				}
				lines = append(lines, status+" "+path+" ← "+oldPath)
				continue
			}
		}
		if !isSigmaManagedWorkspacePath(path) {
			lines = append(lines, status+" "+path)
		}
	}
	if len(lines) == 0 {
		return "Working tree clean. Sigma run/session artifacts are omitted from this status.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func validateGitPaths(paths []string) ([]string, error) {
	validated := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsAny(path, "\x00\r\n") || strings.HasPrefix(path, ":") || filepath.IsAbs(path) {
			return nil, fmt.Errorf("Git path %q must be a workspace-relative path", path)
		}
		clean := filepath.Clean(path)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("Git path %q escapes the workspace", path)
		}
		validated = append(validated, filepath.ToSlash(clean))
	}
	return validated, nil
}
