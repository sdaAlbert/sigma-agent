package sigmaagent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/fsx"
)

const (
	maxBackgroundCommands = 1
	maxBackgroundHistory  = 32
	backgroundCommandTTL  = 30 * time.Minute
	backgroundDockerLimit = 20 * time.Second
)

type backgroundCommand struct {
	ID               string    `json:"id"`
	Container        string    `json:"container"`
	Network          string    `json:"network"`
	Image            string    `json:"image,omitempty"`
	Command          string    `json:"command"`
	WorkingDirectory string    `json:"working_directory"`
	Purpose          string    `json:"purpose"`
	StartedAt        time.Time `json:"started_at"`
	Status           string    `json:"status"`
	ExitCode         int       `json:"exit_code"`
	WorkspaceChanged bool      `json:"workspace_changed"`
	StartingDiffHash string    `json:"starting_diff_hash"`
	Output           string    `json:"output,omitempty"`
}

type backgroundCommandSnapshot struct {
	Version int                 `json:"version"`
	Jobs    []backgroundCommand `json:"jobs"`
}

type BackgroundCommandManager struct {
	mu           sync.Mutex
	root         string
	image        string
	base         string
	directory    string
	allowNetwork bool
	jobs         map[string]backgroundCommand
}

func NewBackgroundCommandManager(root, image, directory, base string, allowNetwork ...bool) (*BackgroundCommandManager, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absoluteDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create background task directory: %w", err)
	}
	if err := os.Chmod(absoluteDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("restrict background task directory permissions: %w", err)
	}
	manager := &BackgroundCommandManager{
		root: filepath.Clean(absoluteRoot), image: image, base: strings.TrimSpace(base),
		directory: filepath.Clean(absoluteDirectory), jobs: make(map[string]backgroundCommand),
	}
	manager.allowNetwork = len(allowNetwork) > 0 && allowNetwork[0]
	data, err := os.ReadFile(manager.snapshotPath())
	if errors.Is(err, os.ErrNotExist) {
		return manager, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read background task state: %w", err)
	}
	var snapshot backgroundCommandSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode background task state: %w", err)
	}
	if snapshot.Version != 1 {
		return nil, errors.New("unsupported background task state version")
	}
	for _, job := range snapshot.Jobs {
		if !validBackgroundTaskID(job.ID) || job.Container == "" || job.Network == "" {
			return nil, errors.New("background task state contains an invalid job")
		}
		manager.jobs[job.ID] = job
	}
	return manager, nil
}

func (m *BackgroundCommandManager) Start(ctx context.Context, command, workingDirectory, purpose string) (backgroundCommand, error) {
	return m.StartWithImage(ctx, m.image, command, workingDirectory, purpose)
}

func (m *BackgroundCommandManager) StartWithImage(ctx context.Context, image, command, workingDirectory, purpose string) (backgroundCommand, error) {
	if image == "" || strings.TrimSpace(image) != image || strings.ContainsAny(image, " \t\r\n") || strings.HasPrefix(image, "-") {
		return backgroundCommand{}, errors.New("background command requires a valid Docker image reference")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if isActiveBackgroundStatus(job.Status) {
			if _, err := m.refreshLocked(ctx, job.ID); err != nil {
				return backgroundCommand{}, err
			}
		}
	}
	active := 0
	for _, job := range m.jobs {
		if isActiveBackgroundStatus(job.Status) {
			active++
		}
	}
	if active >= 1 {
		return backgroundCommand{}, fmt.Errorf("background task limit (%d) reached; inspect or stop an existing task first", maxBackgroundCommands)
	}
	m.pruneHistoryLocked()

	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return backgroundCommand{}, fmt.Errorf("create background task id: %w", err)
	}
	id := hex.EncodeToString(idBytes[:])
	container := "sigma-bg-" + id[:20]
	// Offline tasks use Docker's built-in none network; only online tasks need
	// a per-task bridge network.
	network, err := commandContainerNetwork(ctx, m.allowNetwork)
	if err != nil {
		return backgroundCommand{}, fmt.Errorf("create background task network: %w", err)
	}
	startingDiff, err := WorkspaceDiff(m.root, m.base)
	if err != nil {
		_ = removeIsolatedTestNetwork(network)
		return backgroundCommand{}, fmt.Errorf("inspect workspace before background task: %w", err)
	}
	startingHash := sha256.Sum256([]byte(startingDiff))
	containerWorkdir := "/workspace"
	if workingDirectory != m.root {
		if relative, relErr := filepath.Rel(m.root, workingDirectory); relErr == nil && relative != "." {
			containerWorkdir += "/" + filepath.ToSlash(relative)
		}
	}
	volume := filepath.ToSlash(m.root) + ":/workspace"
	const wrapper = `mkdir -p "$HOME"; command="$1"; timeout_seconds="$2"; /bin/sh -lc "$command" & child=$!; ( sleep "$timeout_seconds"; kill -TERM "$child" 2>/dev/null ) & watchdog=$!; wait "$child"; code=$?; kill "$watchdog" 2>/dev/null || true; wait "$watchdog" 2>/dev/null || true; exit "$code"`
	args := []string{
		"run", "--detach", "--init", "--name", container,
		"--label", "sigma.session-task=true", "--label", "sigma.task-id=" + id,
		"--network", network, "--memory", "2g", "--cpus", "2", "--pids-limit", "128",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--env", "HOME=/workspace/.sigma-agent/container-home", "--env", "GOPATH=/workspace/.sigma-agent/container-home/go",
		"--env", "GOMODCACHE=/workspace/.sigma-agent/container-home/go/pkg/mod", "--env", "GOCACHE=/workspace/.sigma-agent/container-home/go/build-cache",
		"--env", "TMPDIR=/tmp", "--tmpfs", "/tmp:rw,nosuid,size=256m",
	}
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		args = append(args, "--user", fmt.Sprintf("%d:%d", uid, gid))
	}
	args = append(args, "--volume", volume, "--workdir", containerWorkdir, image,
		"/bin/sh", "-lc", wrapper, "sigma-background", command, strconv.Itoa(int(backgroundCommandTTL.Seconds())))
	job := backgroundCommand{
		ID: id, Container: container, Network: network, Image: image, Command: command,
		WorkingDirectory: filepath.ToSlash(containerWorkdir), Purpose: purpose,
		StartedAt: time.Now().UTC(), Status: "starting", StartingDiffHash: hex.EncodeToString(startingHash[:]),
	}
	m.jobs[id] = job
	if err := m.saveLocked(); err != nil {
		delete(m.jobs, id)
		_ = removeIsolatedTestNetwork(network)
		return backgroundCommand{}, err
	}
	dockerCtx, cancel := context.WithTimeout(ctx, backgroundDockerLimit)
	defer cancel()
	cmd := exec.CommandContext(dockerCtx, "docker", args...)
	cmd.Dir = m.root
	output, err := cmd.CombinedOutput()
	if err != nil {
		job.Status = "failed"
		job.ExitCode = 1
		job.Output = strings.TrimSpace(string(output))
		m.jobs[id] = job
		_ = m.saveLocked()
		_ = exec.Command("docker", "rm", "--force", container).Run()
		_ = removeIsolatedTestNetwork(network)
		return backgroundCommand{}, fmt.Errorf("start background Docker task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	job.Status = "running"
	m.jobs[id] = job
	if err := m.saveLocked(); err != nil {
		_ = exec.Command("docker", "rm", "--force", container).Run()
		_ = removeIsolatedTestNetwork(network)
		return backgroundCommand{}, err
	}
	return job, nil
}

func (m *BackgroundCommandManager) Status(ctx context.Context, id string) (backgroundCommand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshLocked(ctx, id)
}

func (m *BackgroundCommandManager) List() []backgroundCommand {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]backgroundCommand, 0, len(m.jobs))
	for _, job := range m.jobs {
		job.Output = ""
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].StartedAt.After(jobs[j].StartedAt) })
	return jobs
}

func (m *BackgroundCommandManager) EnsureNoActive() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if isActiveBackgroundStatus(job.Status) {
			return fmt.Errorf("workspace mutation is paused while background task %s is running; inspect or stop that task first", job.ID)
		}
	}
	return nil
}

func (m *BackgroundCommandManager) Stop(ctx context.Context, id string) (backgroundCommand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return backgroundCommand{}, errors.New("background task was not found in this session")
	}
	if !isActiveBackgroundStatus(job.Status) {
		return job, nil
	}
	if _, err := m.refreshLocked(ctx, id); err != nil {
		return backgroundCommand{}, err
	}
	job = m.jobs[id]
	if !isActiveBackgroundStatus(job.Status) {
		return job, nil
	}
	dockerCtx, cancel := context.WithTimeout(ctx, backgroundDockerLimit)
	defer cancel()
	if output, err := exec.CommandContext(dockerCtx, "docker", "stop", "--time", "10", job.Container).CombinedOutput(); err != nil {
		return backgroundCommand{}, fmt.Errorf("stop background task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return m.finishLocked(ctx, job, "stopped", 143)
}

func (m *BackgroundCommandManager) refreshLocked(ctx context.Context, id string) (backgroundCommand, error) {
	job, ok := m.jobs[id]
	if !ok {
		return backgroundCommand{}, errors.New("background task was not found in this session")
	}
	if !isActiveBackgroundStatus(job.Status) {
		return job, nil
	}
	dockerCtx, cancel := context.WithTimeout(ctx, backgroundDockerLimit)
	defer cancel()
	inspect := exec.CommandContext(dockerCtx, "docker", "inspect", "--format", "{{.State.Status}}|{{.State.ExitCode}}", job.Container)
	inspect.Dir = m.root
	output, err := inspect.CombinedOutput()
	if err != nil {
		message := strings.ToLower(string(output))
		if strings.Contains(message, "no such object") || strings.Contains(message, "no such container") {
			currentDiff, diffErr := WorkspaceDiff(m.root, m.base)
			if diffErr != nil {
				return backgroundCommand{}, fmt.Errorf("inspect workspace after lost background task: %w", diffErr)
			}
			currentHash := sha256.Sum256([]byte(currentDiff))
			job.Status = "lost"
			job.WorkspaceChanged = hex.EncodeToString(currentHash[:]) != job.StartingDiffHash
			job.Output = "Docker no longer has this task container; its final exit status and output are unavailable."
			m.jobs[id] = job
			if saveErr := m.saveLocked(); saveErr != nil {
				return backgroundCommand{}, saveErr
			}
			_ = removeIsolatedTestNetwork(job.Network)
			return job, nil
		}
		return backgroundCommand{}, fmt.Errorf("inspect background task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	parts := strings.SplitN(strings.TrimSpace(string(output)), "|", 2)
	if len(parts) != 2 {
		return backgroundCommand{}, errors.New("Docker returned an invalid background task status")
	}
	if parts[0] == "running" || parts[0] == "created" || parts[0] == "restarting" {
		if time.Since(job.StartedAt) > backgroundCommandTTL {
			dockerCtx, cancel := context.WithTimeout(ctx, backgroundDockerLimit)
			output, err := exec.CommandContext(dockerCtx, "docker", "stop", "--time", "10", job.Container).CombinedOutput()
			cancel()
			if err != nil {
				return backgroundCommand{}, fmt.Errorf("expire background task: %w: %s", err, strings.TrimSpace(string(output)))
			}
			return m.finishLocked(ctx, job, "timed_out", 124)
		}
		if job.Status == "starting" {
			job.Status = "running"
			m.jobs[id] = job
			if err := m.saveLocked(); err != nil {
				return backgroundCommand{}, err
			}
		}
		job.Output, err = m.readLogsLocked(ctx, job)
		if err != nil {
			return backgroundCommand{}, err
		}
		return job, nil
	}
	code, parseErr := strconv.Atoi(parts[1])
	if parseErr != nil {
		return backgroundCommand{}, fmt.Errorf("parse background task exit code: %w", parseErr)
	}
	status := "failed"
	if code == 0 {
		status = "passed"
	}
	return m.finishLocked(ctx, job, status, code)
}

func (m *BackgroundCommandManager) finishLocked(ctx context.Context, job backgroundCommand, status string, exitCode int) (backgroundCommand, error) {
	output, err := m.readLogsLocked(ctx, job)
	if err != nil {
		return backgroundCommand{}, err
	}
	currentDiff, diffErr := WorkspaceDiff(m.root, m.base)
	if diffErr != nil {
		return backgroundCommand{}, fmt.Errorf("inspect workspace after background task: %w", diffErr)
	}
	currentHash := sha256.Sum256([]byte(currentDiff))
	job.WorkspaceChanged = hex.EncodeToString(currentHash[:]) != job.StartingDiffHash
	job.Status, job.ExitCode, job.Output = status, exitCode, output
	m.jobs[job.ID] = job
	if saveErr := m.saveLocked(); saveErr != nil {
		return backgroundCommand{}, saveErr
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), backgroundDockerLimit)
	defer cleanupCancel()
	_ = exec.CommandContext(cleanupCtx, "docker", "rm", "--force", job.Container).Run()
	_ = removeIsolatedTestNetwork(job.Network)
	return job, nil
}

func (m *BackgroundCommandManager) readLogsLocked(ctx context.Context, job backgroundCommand) (string, error) {
	logCtx, cancel := context.WithTimeout(ctx, backgroundDockerLimit)
	defer cancel()
	logs := exec.CommandContext(logCtx, "docker", "logs", "--tail", "1000", job.Container)
	logs.Dir = m.root
	buffer := &limitedCommandBuffer{limit: maxCommandOutputBytes}
	logs.Stdout, logs.Stderr = buffer, buffer
	if err := logs.Run(); err != nil {
		return "", fmt.Errorf("read background task output: %w", err)
	}
	return buffer.String(), nil
}

func (m *BackgroundCommandManager) saveLocked() error {
	jobs := make([]backgroundCommand, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	data, err := json.MarshalIndent(backgroundCommandSnapshot{Version: 1, Jobs: jobs}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode background task state: %w", err)
	}
	temporary, err := os.CreateTemp(m.directory, "background-tasks-*.tmp")
	if err != nil {
		return fmt.Errorf("create background task state temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write background task state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("sync background task state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := fsx.ReplaceFile(temporaryPath, m.snapshotPath()); err != nil {
		cleanup()
		return fmt.Errorf("publish background task state: %w", err)
	}
	return nil
}

func (m *BackgroundCommandManager) snapshotPath() string {
	return filepath.Join(m.directory, "tasks.json")
}

func (m *BackgroundCommandManager) pruneHistoryLocked() {
	if len(m.jobs) < maxBackgroundHistory {
		return
	}
	finished := make([]backgroundCommand, 0, len(m.jobs))
	for _, job := range m.jobs {
		if job.Status != "running" {
			finished = append(finished, job)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].StartedAt.Before(finished[j].StartedAt) })
	remove := len(m.jobs) - maxBackgroundHistory + 1
	for _, job := range finished {
		if remove <= 0 {
			break
		}
		delete(m.jobs, job.ID)
		remove--
	}
}

func validBackgroundTaskID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func isActiveBackgroundStatus(status string) bool {
	return status == "starting" || status == "running"
}
