package agent

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// PushBranchOptions describes an explicitly authorized branch push for one run.
type PushBranchOptions struct {
	// Branch is the feature branch the run may push. It must not be a
	// protected branch such as main.
	Branch string
	// Remote is the Git remote to push to. Defaults to "origin".
	Remote string
	// BaseBranch is the branch the pull request is opened against. It is also
	// the branch that must not be pushed to directly.
	BaseBranch string
}

// remoteName returns the configured remote, defaulting to "origin".
func (o PushBranchOptions) remoteName() string {
	if remote := strings.TrimSpace(o.Remote); remote != "" {
		return remote
	}
	return "origin"
}

// baseBranch returns the configured base branch, defaulting to "main".
func (o PushBranchOptions) baseBranch() string {
	if base := strings.TrimSpace(o.BaseBranch); base != "" {
		return base
	}
	return "main"
}

// Validate rejects a branch name that cannot be used as a feature branch. The
// base branch is rejected as well, so a run can never push directly to it.
func (o PushBranchOptions) Validate() error {
	branch := strings.TrimSpace(o.Branch)
	if branch == "" {
		return errors.New("a feature branch name is required")
	}
	if err := ValidateBranchName(branch); err != nil {
		return err
	}
	if branch == o.baseBranch() {
		return fmt.Errorf("branch %q is the base branch; push a separate feature branch", branch)
	}
	if branch == "HEAD" {
		return errors.New("branch name HEAD is not allowed")
	}
	return nil
}

// ValidateBranchName rejects branch names that are unsafe or ambiguous to pass
// to Git.
func ValidateBranchName(branch string) error {
	if strings.TrimSpace(branch) != branch {
		return errors.New("branch name must not start or end with whitespace")
	}
	if strings.ContainsAny(branch, " \t\r\n~^:?*[\\") {
		return fmt.Errorf("branch name %q contains a character Git does not allow", branch)
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("branch name %q must not start with a dash", branch)
	}
	if strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") {
		return fmt.Errorf("branch name %q must not start or end with a slash", branch)
	}
	if strings.Contains(branch, "..") {
		return fmt.Errorf("branch name %q must not contain '..'", branch)
	}
	if strings.Contains(branch, "//") {
		return fmt.Errorf("branch name %q must not contain an empty path segment", branch)
	}
	if strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("branch name %q must not end with '.lock'", branch)
	}
	for _, segment := range strings.Split(branch, "/") {
		if segment == "" {
			return fmt.Errorf("branch name %q must not contain an empty path segment", branch)
		}
		if strings.HasPrefix(segment, ".") {
			return fmt.Errorf("branch name %q must not contain a segment starting with '.'", branch)
		}
	}
	return nil
}

// OriginRepository returns the "owner/name" repository of the checkout's
// configured remote. The result is the only repository a pull request may be
// created against.
func OriginRepository(repoRoot, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	command := exec.Command("git", "remote", "get-url", remote)
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve remote %q: %w: %s", remote, err, strings.TrimSpace(string(output)))
	}
	return RepositoryFromRemoteURL(strings.TrimSpace(string(output)))
}

// RepositoryFromRemoteURL extracts the "owner/name" repository from a Git
// remote URL. Both SSH and HTTPS forms are supported.
func RepositoryFromRemoteURL(remoteURL string) (string, error) {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return "", errors.New("remote URL is empty")
	}
	if strings.HasPrefix(trimmed, "git@") {
		// git@github.com:owner/name.git
		trimmed = strings.TrimPrefix(trimmed, "git@")
		if index := strings.Index(trimmed, ":"); index >= 0 {
			trimmed = trimmed[index+1:]
		} else {
			trimmed = trimmed[strings.Index(trimmed, "/")+1:]
		}
	} else {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse remote URL: %w", err)
		}
		trimmed = parsed.Path
	}
	trimmed = strings.Trim(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("remote URL %q does not identify an owner/name repository", remoteURL)
	}
	return parts[0] + "/" + parts[1], nil
}

// PushBranch commits the current task worktree onto the authorized feature
// branch and pushes it to the configured remote. It refuses to push the base
// branch and never merges anything.
func PushBranch(repoRoot, worktree string, options PushBranchOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	remote := options.remoteName()
	branch := strings.TrimSpace(options.Branch)

	// A resumed task may already be checked out on its authorized local feature
	// branch. Allow that exact branch, but never reset another local branch.
	currentBranch, err := gitOutput(worktree, "branch", "--show-current")
	if err != nil {
		return fmt.Errorf("inspect current task branch: %w", err)
	}
	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch != branch {
		if _, err := gitOutput(worktree, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return fmt.Errorf("local branch %q already exists; choose a new feature branch", branch)
		}
	}
	remoteRefs, err := gitOutput(repoRoot, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("check remote branch %q: %w", branch, err)
	}
	if strings.TrimSpace(remoteRefs) != "" {
		return fmt.Errorf("remote branch %q already exists; choose a new feature branch", branch)
	}

	// Session worktrees can start detached or already be on the authorized
	// branch when resuming the same task. Create the branch only in the former.
	if currentBranch != branch {
		if _, err := gitOutput(worktree, "checkout", "-b", branch); err != nil {
			return fmt.Errorf("create feature branch %q: %w", branch, err)
		}
	}
	if _, err := gitOutput(worktree, "add", "--all"); err != nil {
		return fmt.Errorf("stage changes on %q: %w", branch, err)
	}
	status, err := gitOutput(worktree, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("inspect staged changes on %q: %w", branch, err)
	}
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("no changes to push on %q", branch)
	}
	message := "feat: apply Sigma Agent changes for " + branch
	if _, err := gitOutput(worktree, "commit", "-m", message); err != nil {
		return fmt.Errorf("commit changes on %q: %w", branch, err)
	}
	if _, err := gitOutput(worktree, "push", "--set-upstream", remote, branch+":"+branch); err != nil {
		return fmt.Errorf("push %q to %s: %w", branch, remote, err)
	}
	return nil
}

func gitOutput(directory string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
