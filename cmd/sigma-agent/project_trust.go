package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/fsx"
	"github.com/sdaAlbert/sigma-agent/internal/mcpconfig"
)

type projectTrustSettings struct {
	Version  int             `json:"version"`
	Projects map[string]bool `json:"projects"`
}

func projectTrustFile() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sigma-agent", "project-trust.json"), nil
}

func canonicalProjectTrustKey(project string) (string, error) {
	absolute, err := filepath.Abs(project)
	if err != nil {
		return "", fmt.Errorf("resolve project folder: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		absolute = strings.ToLower(absolute)
	}
	return absolute, nil
}

func loadProjectTrustSettings() (projectTrustSettings, string, error) {
	path, err := projectTrustFile()
	if err != nil {
		return projectTrustSettings{}, "", err
	}
	settings := projectTrustSettings{Version: 1, Projects: make(map[string]bool)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, path, nil
	}
	if err != nil {
		return projectTrustSettings{}, "", err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return projectTrustSettings{}, "", fmt.Errorf("decode project trust settings: %w", err)
	}
	if settings.Version != 1 || settings.Projects == nil {
		return projectTrustSettings{}, "", errors.New("unsupported project trust settings format")
	}
	return settings, path, nil
}

func projectTrustDecision(project string) (bool, error) {
	key, err := canonicalProjectTrustKey(project)
	if err != nil {
		return false, err
	}
	settings, _, err := loadProjectTrustSettings()
	if err != nil {
		return false, err
	}
	return settings.Projects[key], nil
}

func saveProjectTrustDecision(project string, trusted bool) error {
	key, err := canonicalProjectTrustKey(project)
	if err != nil {
		return err
	}
	settings, path, err := loadProjectTrustSettings()
	if err != nil {
		return err
	}
	settings.Projects[key] = trusted
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create project trust settings directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("restrict project trust settings permissions: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "project-trust-*.tmp")
	if err != nil {
		return err
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
	if err := fsx.ReplaceFile(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("save project trust settings: %w", err)
	}
	return nil
}

func executablePackageScopes(scopes []packageScope, project string) ([]packageScope, bool, error) {
	trusted, err := projectTrustDecision(project)
	if trusted {
	}
	filtered := make([]packageScope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Name != "project" {
			filtered = append(filtered, scope)
		}
	}
	return filtered, false, err
}

func projectHasExecutableResources(project string) bool {
	scope, err := projectScope(project)
	if err != nil {
		return false
	}
	if paths, err := packageExtensions([]packageScope{scope}); err == nil && len(paths) > 0 {
		return true
	}
	for _, path := range []string{
		filepath.Join(scope.Dir, mcpconfig.FileName),
		filepath.Join(project, ".pi", mcpconfig.FileName),
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
