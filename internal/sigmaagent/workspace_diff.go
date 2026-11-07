package sigmaagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const agentManagedMemoryPath = ".sigma-agent/MEMORY.md"

// stageTaskChanges stages workspace changes without including Sigma-managed
// memory in the candidate patch. Task worktrees are expected to begin with a
// clean index, as benchmark sessions do.
func stageTaskChanges(ctx context.Context, root string) error {
	reset := exec.CommandContext(ctx, "git", "reset", "-q", "--", agentManagedMemoryPath)
	reset.Dir = root
	if output, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("exclude agent-managed memory from index: %w: %s", err, strings.TrimSpace(string(output)))
	}
	add := exec.CommandContext(ctx, "git", "add", "-A", "--", ".", ":(exclude)"+agentManagedMemoryPath)
	add.Dir = root
	if output, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("stage task changes: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// WorkspaceDiff returns task changes, including new non-ignored files, while
// omitting Sigma-managed repository memory from candidate patches.
func WorkspaceDiff(root string, baseCommit ...string) (string, error) {
	if !HasGitCommit(root) {
		return workspaceFingerprint(root)
	}
	base := "HEAD"
	if len(baseCommit) > 0 && strings.TrimSpace(baseCommit[0]) != "" {
		base = baseCommit[0]
	}
	list := exec.Command("git", "ls-files", "--others", "--exclude-standard", "-z")
	list.Dir = root
	untrackedOutput, err := list.Output()
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w", err)
	}
	files := splitNUL(untrackedOutput)
	filteredFiles := files[:0]
	for _, file := range files {
		if filepath.ToSlash(file) == agentManagedMemoryPath {
			continue
		}
		filteredFiles = append(filteredFiles, file)
	}
	files = filteredFiles
	if len(files) == 0 {
		return runWorkspaceDiff(root, "", base)
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
	return runWorkspaceDiff(root, gitIndexEnv, base)
}

// IsGitRepository reports whether root is inside a Git worktree.
func IsGitRepository(root string) bool {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = root
	return command.Run() == nil
}

// HasGitCommit reports whether root is inside a Git worktree with a HEAD commit.
func HasGitCommit(root string) bool {
	command := exec.Command("git", "rev-parse", "--verify", "HEAD")
	command.Dir = root
	return command.Run() == nil
}

func workspaceFingerprint(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	visited := 0
	err = filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absolute {
			return nil
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			name := strings.ToLower(entry.Name())
			if ignoredDirs[name] || relative == "var/runs" || relative == "var/sessions" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if sensitiveName(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		visited++
		if visited > 50000 {
			return fmt.Errorf("workspace snapshot exceeds the 50000-file limit")
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(info.Size(), 10)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatUint(uint64(info.Mode()), 10)))
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("snapshot workspace: %w", err)
	}
	return "workspace-snapshot:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func runWorkspaceDiff(root, gitIndexEnv, base string) (string, error) {
	diff := exec.Command("git", "diff", "--no-ext-diff", "--binary", "--unified=3", base, "--", ".", ":(exclude)"+agentManagedMemoryPath)
	diff.Dir = root
	if gitIndexEnv != "" {
		diff.Env = append(os.Environ(), gitIndexEnv)
	}
	var stderr bytes.Buffer
	diff.Stderr = &stderr
	output, err := diff.Output()
	if err != nil {
		return "", fmt.Errorf("collect Git diff: %w: %s", err, strings.TrimSpace(stderr.String()))
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
