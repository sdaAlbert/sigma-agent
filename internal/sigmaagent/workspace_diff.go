package sigmaagent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorkspaceDiff returns the working tree diff, including new non-ignored files.
func WorkspaceDiff(root string) (string, error) {
	list := exec.Command("git", "ls-files", "--others", "--exclude-standard", "-z")
	list.Dir = root
	untrackedOutput, err := list.Output()
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w", err)
	}
	files := splitNUL(untrackedOutput)
	if len(files) == 0 {
		return runWorkspaceDiff(root, "")
	}

	indexCommand := exec.Command("git", "rev-parse", "--git-path", "index")
	indexCommand.Dir = root
	indexOutput, err := indexCommand.Output()
	if err != nil {
		return "", fmt.Errorf("locate Git index: %w", err)
	}
	indexPath := strings.TrimSpace(string(indexOutput))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(root, indexPath)
	}
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("read Git index: %w", err)
	}
	temporaryIndex, err := os.CreateTemp("", "sigmaagent-git-index-*")
	if err != nil {
		return "", fmt.Errorf("create temporary Git index: %w", err)
	}
	temporaryIndexPath := temporaryIndex.Name()
	if err := temporaryIndex.Close(); err != nil {
		_ = os.Remove(temporaryIndexPath)
		return "", fmt.Errorf("close temporary Git index: %w", err)
	}
	defer os.Remove(temporaryIndexPath)
	if err := os.WriteFile(temporaryIndexPath, index, 0o600); err != nil {
		return "", fmt.Errorf("copy Git index: %w", err)
	}
	gitIndexEnv := "GIT_INDEX_FILE=" + temporaryIndexPath

	addArgs := append([]string{"add", "-N", "--"}, files...)
	add := exec.Command("git", addArgs...)
	add.Dir = root
	add.Env = append(os.Environ(), gitIndexEnv)
	if output, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("prepare untracked files for diff: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return runWorkspaceDiff(root, gitIndexEnv)
}

func runWorkspaceDiff(root, gitIndexEnv string) (string, error) {
	diff := exec.Command("git", "diff", "--no-ext-diff", "--binary", "--unified=3", "HEAD")
	diff.Dir = root
	if gitIndexEnv != "" {
		diff.Env = append(os.Environ(), gitIndexEnv)
	}
	output, err := diff.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("collect Git diff: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func splitNUL(output []byte) []string {
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			files = append(files, string(part))
		}
	}
	return files
}
