package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkspaceCheckpoint is an immutable Git tree captured without changing the
// user's branch or index. Git-ignored files and Sigma-managed runtime files are
// intentionally outside the checkpoint.
type WorkspaceCheckpoint struct {
	ID        string    `json:"id"`
	Commit    string    `json:"commit"`
	CreatedAt time.Time `json:"created_at"`
	Label     string    `json:"label"`
	Turn      int       `json:"turn,omitempty"`
}

type WorkspaceRestorePreview struct {
	Checkpoint WorkspaceCheckpoint `json:"checkpoint"`
	Files      []string            `json:"files"`
}

type WorkspaceCheckpoints struct {
	root      string
	sessionID string
	mu        sync.Mutex
}

// NewWorkspaceCheckpoints enables checkpoints for a Git worktree with a HEAD
// commit. It does not initialize Git or modify the current branch/index.
func NewWorkspaceCheckpoints(root, sessionID string) (*WorkspaceCheckpoints, error) {
	if !ValidCheckpointSessionID(sessionID) {
		return nil, errors.New("invalid checkpoint session id")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if !IsGitRepository(absolute) || !HasGitCommit(absolute) {
		return nil, errors.New("version checkpoints require a Git worktree with a commit")
	}
	top, err := checkpointGit(absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top, err = filepath.Abs(strings.TrimSpace(top))
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Clean(top), filepath.Clean(absolute)) {
		return nil, errors.New("version checkpoints require Sigma to run from the Git repository root")
	}
	return &WorkspaceCheckpoints{root: filepath.Clean(absolute), sessionID: sessionID}, nil
}

// ValidCheckpointSessionID rejects characters that could escape the private
// refs namespace used for checkpoint commits.
func ValidCheckpointSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (c *WorkspaceCheckpoints) refPrefix() string {
	return "refs/sigma-agent/checkpoints/" + c.sessionID + "/"
}

// Create captures the current effective worktree state. Repeated identical
// states are deduplicated, even when the labels differ.
func (c *WorkspaceCheckpoints) Create(ctx context.Context, label string, turn int) (WorkspaceCheckpoint, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.createUnlocked(ctx, label, turn)
}

func (c *WorkspaceCheckpoints) createUnlocked(ctx context.Context, label string, turn int) (WorkspaceCheckpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceCheckpoint{}, false, err
	}
	head, err := checkpointGit(c.root, "rev-parse", "HEAD")
	if err != nil {
		return WorkspaceCheckpoint{}, false, err
	}
	index, err := os.CreateTemp("", "sigma-checkpoint-index-*")
	if err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("create temporary checkpoint index: %w", err)
	}
	indexPath := index.Name()
	if err := index.Close(); err != nil {
		_ = os.Remove(indexPath)
		return WorkspaceCheckpoint{}, false, err
	}
	defer os.Remove(indexPath)
	if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return WorkspaceCheckpoint{}, false, err
	}
	gitEnv := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := checkpointGitEnv(c.root, gitEnv, "read-tree", head); err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("initialize temporary checkpoint index: %w", err)
	}
	addArgs := []string{"add", "-A", "--", "."}
	for _, path := range []string{".sigma", ".sigma-agent", legacySigmaRunArtifactsPath, legacySigmaSessionArtifactsPath} {
		addArgs = append(addArgs, ":(exclude)"+path)
	}
	if _, err := checkpointGitEnv(c.root, gitEnv, addArgs...); err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("capture worktree files: %w", err)
	}
	tree, err := checkpointGitEnv(c.root, gitEnv, "write-tree")
	if err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("write checkpoint tree: %w", err)
	}
	tree = strings.TrimSpace(tree)
	prior, _ := c.List(ctx)
	if len(prior) > 0 {
		previous := prior[len(prior)-1]
		previousTree, treeErr := checkpointGit(c.root, "rev-parse", previous.Commit+"^{tree}")
		if treeErr == nil && strings.TrimSpace(previousTree) == tree {
			return previous, false, nil
		}
	}
	label = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(label, "\r", " "), "\n", " "))
	if label == "" {
		label = "workspace change"
	}
	created := time.Now().UTC()
	message := fmt.Sprintf("Sigma checkpoint: %s\n\nsession: %s\nturn: %d\ncreated: %s\n", label, c.sessionID, turn, created.Format(time.RFC3339Nano))
	commit, err := checkpointGitEnv(c.root, gitEnv, "-c", "user.name=Sigma Agent", "-c", "user.email=sigma-agent@localhost", "commit-tree", tree, "-p", strings.TrimSpace(head), "-m", message)
	if err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("create checkpoint object: %w", err)
	}
	commit = strings.TrimSpace(commit)
	id := created.Format("20060102T150405.000000000") + "-" + commit[:12]
	ref := c.refPrefix() + id
	if _, err := checkpointGit(c.root, "update-ref", ref, commit); err != nil {
		return WorkspaceCheckpoint{}, false, fmt.Errorf("save checkpoint reference: %w", err)
	}
	return WorkspaceCheckpoint{ID: id, Commit: commit, CreatedAt: created, Label: label, Turn: turn}, true, nil
}

func (c *WorkspaceCheckpoints) List(ctx context.Context) ([]WorkspaceCheckpoint, error) {
	format := "%(refname:strip=4)%00%(objectname)%00%(creatordate:iso-strict)%00%(subject)"
	output, err := checkpointGit(c.root, "for-each-ref", "--sort=creatordate", "--format="+format, c.refPrefix())
	if err != nil {
		return nil, err
	}
	var result []WorkspaceCheckpoint
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			continue
		}
		created, _ := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
		label := strings.TrimPrefix(parts[3], "Sigma checkpoint: ")
		turn := 0
		if body, bodyErr := checkpointGit(ctxRoot(c.root), "show", "-s", "--format=%B", strings.TrimSpace(parts[1])); bodyErr == nil {
			for _, item := range strings.Split(body, "\n") {
				if strings.HasPrefix(item, "turn: ") {
					_, _ = fmt.Sscanf(strings.TrimPrefix(item, "turn: "), "%d", &turn)
				}
			}
		}
		result = append(result, WorkspaceCheckpoint{ID: strings.TrimSpace(parts[0]), Commit: strings.TrimSpace(parts[1]), CreatedAt: created, Label: label, Turn: turn})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

// PreviewRestore lists paths whose effective worktree contents will change.
func (c *WorkspaceCheckpoints) PreviewRestore(ctx context.Context, id string) (WorkspaceRestorePreview, error) {
	checkpoint, err := c.find(ctx, id)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	current, _, err := c.Create(ctx, "pre-restore backup", 0)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	output, err := checkpointGit(c.root, "diff", "--name-only", "-z", checkpoint.Commit, current.Commit)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	files := splitNUL([]byte(output))
	return WorkspaceRestorePreview{Checkpoint: checkpoint, Files: files}, nil
}

// Restore applies a selected snapshot to the worktree only. The Git branch and
// index remain untouched. A backup checkpoint is created before any file is
// changed, so a restore can itself be reversed.
func (c *WorkspaceCheckpoints) Restore(ctx context.Context, id string) (WorkspaceRestorePreview, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	target, err := c.findUnlocked(ctx, id)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	backup, _, err := c.createUnlocked(ctx, "pre-restore backup", 0)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	currentFiles, err := checkpointTreePaths(ctx, c.root, backup.Commit)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	targetFiles, err := checkpointTreePaths(ctx, c.root, target.Commit)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	targetSet := make(map[string]bool, len(targetFiles))
	for _, path := range targetFiles {
		targetSet[path] = true
	}
	for _, path := range currentFiles {
		if targetSet[path] {
			continue
		}
		absolute := filepath.Join(c.root, filepath.FromSlash(path))
		if err := ensureCheckpointPath(c.root, absolute); err != nil {
			return WorkspaceRestorePreview{}, err
		}
		if err := os.Remove(absolute); err != nil && !errors.Is(err, os.ErrNotExist) {
			return WorkspaceRestorePreview{}, fmt.Errorf("remove %s while restoring checkpoint: %w", path, err)
		}
	}
	for _, path := range targetFiles {
		absolute := filepath.Join(c.root, filepath.FromSlash(path))
		if err := ensureCheckpointPath(c.root, absolute); err != nil {
			return WorkspaceRestorePreview{}, err
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return WorkspaceRestorePreview{}, err
		}
		if _, err := checkpointGit(c.root, "restore", "--source", target.Commit, "--worktree", "--", path); err != nil {
			return WorkspaceRestorePreview{}, fmt.Errorf("restore %s: %w", path, err)
		}
	}
	diff, err := checkpointGit(c.root, "diff", "--name-only", "-z", target.Commit, backup.Commit)
	if err != nil {
		return WorkspaceRestorePreview{}, err
	}
	return WorkspaceRestorePreview{Checkpoint: target, Files: splitNUL([]byte(diff))}, nil
}

func (c *WorkspaceCheckpoints) find(ctx context.Context, id string) (WorkspaceCheckpoint, error) {
	items, err := c.List(ctx)
	if err != nil {
		return WorkspaceCheckpoint{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return WorkspaceCheckpoint{}, fmt.Errorf("checkpoint %q not found for this session", id)
}

func (c *WorkspaceCheckpoints) findUnlocked(ctx context.Context, id string) (WorkspaceCheckpoint, error) {
	return c.find(ctx, id)
}

func checkpointTreePaths(ctx context.Context, root, commit string) ([]string, error) {
	output, err := checkpointGit(root, "ls-tree", "-r", "-z", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	paths := splitNUL([]byte(output))
	filtered := paths[:0]
	for _, path := range paths {
		if !isSigmaManagedWorkspacePath(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered, nil
}

func ensureCheckpointPath(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("checkpoint path escapes workspace: %s", path)
	}
	return nil
}

func checkpointGit(root string, args ...string) (string, error) {
	return checkpointGitEnv(root, os.Environ(), args...)
}

func checkpointGitEnv(root string, env []string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func ctxRoot(root string) string { return root }
