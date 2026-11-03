// Package credentials owns Sigma's user-scoped credential and model-selection file.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sdaAlbert/sigma-agent/internal/fsx"
)

const configVersion = 1

// Config is persisted under the user's configuration directory, never in a repository.
type Config struct {
	Version      int               `json:"version"`
	APIKeys      map[string]string `json:"api_keys,omitempty"`
	DefaultModel struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"default_model,omitempty"`
	GitHubToken string `json:"github_token,omitempty"`
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: configVersion, APIKeys: make(map[string]string)}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("inspect credential file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return Config{}, errors.New("credential file must be a small regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read credential file: %w", err)
	}
	var file Config
	if err := json.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("decode credential file: %w", err)
	}
	if file.Version != configVersion {
		return Config{}, fmt.Errorf("unsupported credential file version %d", file.Version)
	}
	if file.APIKeys == nil {
		file.APIKeys = make(map[string]string)
	}
	return file, nil
}

func Save(file Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil && !isWindows() {
		return fmt.Errorf("restrict credential directory permissions: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return errors.New("credential file path is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect credential file path: %w", err)
	}
	file.Version = configVersion
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".model-auth-*.tmp")
	if err != nil {
		return fmt.Errorf("create credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := fsx.ReplaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("save credential file: %w", err)
	}
	return nil
}

func Path() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, "sigma-agent", "model-auth.json"), nil
}

func isWindows() bool { return os.PathSeparator == '\\' }
