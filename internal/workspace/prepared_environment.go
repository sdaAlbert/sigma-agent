package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/fsx"
)

const environmentStateVersion = 1
const maxEnvironmentSetupBytes = 16 * 1024

var dockerImageReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@_-]{0,511}$`)

type commandEnvironmentSnapshot struct {
	Version int    `json:"version"`
	Image   string `json:"image"`
}

type commandEnvironmentState struct {
	mu    sync.RWMutex
	path  string
	image string
}

func newCommandEnvironmentState(root string) (*commandEnvironmentState, error) {
	directory := filepath.Join(root, ".sigma-agent")
	state := &commandEnvironmentState{path: filepath.Join(directory, "command-environment.json")}
	directoryInfo, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect command environment directory: %w", err)
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return nil, errors.New(".sigma-agent must be a regular workspace directory to persist command environment settings")
	}
	info, err := os.Lstat(state.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect command environment state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, errors.New("command environment state must be a small regular file")
	}
	data, err := os.ReadFile(state.path)
	if err != nil {
		return nil, fmt.Errorf("read command environment state: %w", err)
	}
	var snapshot commandEnvironmentSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode command environment state: %w", err)
	}
	if snapshot.Version != environmentStateVersion || !validDockerImageReference(snapshot.Image) {
		return nil, errors.New("command environment state contains an invalid image")
	}
	state.image = snapshot.Image
	return state, nil
}

func (s *commandEnvironmentState) Image() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.image
}

func (s *commandEnvironmentState) Set(image string) error {
	if s == nil {
		return errors.New("command environment state is unavailable")
	}
	if !validDockerImageReference(image) {
		return errors.New("invalid Docker image reference")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create command environment state directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New(".sigma-agent must be a regular workspace directory to persist command environment settings")
	}
	if existing, err := os.Lstat(s.path); err == nil && (existing.Mode()&os.ModeSymlink != 0 || !existing.Mode().IsRegular()) {
		return errors.New("command environment state path is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect command environment state path: %w", err)
	}
	data, err := json.Marshal(commandEnvironmentSnapshot{Version: environmentStateVersion, Image: image})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".command-environment-*.tmp")
	if err != nil {
		return fmt.Errorf("create command environment state file: %w", err)
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
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := fsx.ReplaceFile(temporaryPath, s.path); err != nil {
		cleanup()
		return fmt.Errorf("save command environment state: %w", err)
	}
	s.image = image
	return nil
}

func validDockerImageReference(image string) bool {
	return image != "" && len(image) <= 512 && dockerImageReference.MatchString(image)
}

func (w workspace) prepareEnvironmentTool() agent.Tool {
	networkMode := "disabled inside build steps; enable --allow-container-network to download OS packages"
	if w.allowNetwork {
		networkMode = "enabled inside build steps"
	}
	description := "Prepare a reusable Docker image for this task. Inspect repository Dockerfiles, devcontainer configuration, CI workflows, and dependency manifests first; choose any valid Docker image reference that matches the repository's toolchain. setup_command may install required OS packages and libraries. Sigma builds a derived image once and persists its image reference for later run_command calls and session resumes. The build context contains only a generated Dockerfile, not the repository. Docker network access for build steps is " + networkMode + "."
	return tool("prepare_environment", description, `{"type":"object","properties":{"base_image":{"type":"string","minLength":1,"maxLength":512,"pattern":"^[A-Za-z0-9][A-Za-z0-9._:/@_-]*$","description":"Any Docker image reference, for example an official compiler/runtime image or an image specified by the repository."},"setup_command":{"type":"string","maxLength":16384,"description":"Optional shell command to install system packages, headers, or tools into a derived cached image."}},"required":["base_image"],"additionalProperties":false}`, "sequential", func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args struct {
			BaseImage    string `json:"base_image"`
			SetupCommand string `json:"setup_command"`
		}
		if err := decodeArgs(raw, &args); err != nil {
			return "", err
		}
		args.BaseImage = strings.TrimSpace(args.BaseImage)
		args.SetupCommand = strings.TrimSpace(args.SetupCommand)
		if !validDockerImageReference(args.BaseImage) {
			return "", errors.New("base_image must be a valid Docker image reference")
		}
		if len(args.SetupCommand) > maxEnvironmentSetupBytes {
			return "", fmt.Errorf("setup_command must be at most %d bytes", maxEnvironmentSetupBytes)
		}
		if err := w.ensureNoActiveBackgroundTask(); err != nil {
			return "", err
		}
		selectedImage := args.BaseImage
		buildOutput := ""
		if args.SetupCommand != "" {
			var err error
			selectedImage, buildOutput, err = buildCommandEnvironment(ctx, args.BaseImage, args.SetupCommand, w.allowNetwork)
			if err != nil {
				return "", err
			}
		}
		if err := w.commandEnvironment.Set(selectedImage); err != nil {
			return "", err
		}
		result, err := encode(map[string]any{
			"base_image": args.BaseImage, "prepared_image": selectedImage,
			"setup_command": args.SetupCommand, "network_enabled": w.allowNetwork,
			"status": "prepared", "build_output": buildOutput,
		})
		if err != nil {
			return "", err
		}
		return "SIGMA_ENVIRONMENT prepared: subsequent run_command calls use prepared_image unless they specify another image\n" + result, nil
	})
}

func buildCommandEnvironment(ctx context.Context, baseImage, setupCommand string, allowNetwork bool) (string, string, error) {
	buildDigest := sha256.Sum256([]byte("sigma-environment-v1\x00" + baseImage + "\x00" + setupCommand))
	preparedImage := "sigma-agent/env:" + hex.EncodeToString(buildDigest[:12])
	buildDirectory, err := os.MkdirTemp("", "sigma-agent-environment-")
	if err != nil {
		return "", "", fmt.Errorf("create Docker environment build context: %w", err)
	}
	defer os.RemoveAll(buildDirectory)
	commandJSON, err := json.Marshal(setupCommand)
	if err != nil {
		return "", "", fmt.Errorf("encode environment setup command: %w", err)
	}
	dockerfile := "FROM " + baseImage + "\nUSER root\nRUN [\"/bin/sh\", \"-lc\", " + string(commandJSON) + "]\n"
	dockerfilePath := filepath.Join(buildDirectory, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o600); err != nil {
		return "", "", fmt.Errorf("write Docker environment definition: %w", err)
	}
	buildCtx, cancel := context.WithTimeout(ctx, 9*time.Minute)
	defer cancel()
	args := []string{"build", "--rm", "--file", dockerfilePath, "--tag", preparedImage}
	if !allowNetwork {
		args = append(args, "--network", "none")
	}
	args = append(args, buildDirectory)
	command := exec.CommandContext(buildCtx, "docker", args...)
	command.Dir = buildDirectory
	output := &limitedCommandBuffer{limit: maxCommandOutputBytes}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		if buildCtx.Err() != nil {
			return "", output.String(), fmt.Errorf("Docker environment build timed out or was cancelled: %w\n%s", buildCtx.Err(), output.String())
		}
		return "", output.String(), fmt.Errorf("build Docker environment image %s: %w\n%s", preparedImage, err, output.String())
	}
	return preparedImage, output.String(), nil
}
