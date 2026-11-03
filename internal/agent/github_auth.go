package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/credentials"
)

func SaveGitHubToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 16*1024 || strings.ContainsAny(token, "\r\n\x00") {
		return errors.New("GitHub token is empty or invalid")
	}
	file, err := credentials.Load()
	if err != nil {
		return err
	}
	file.GitHubToken = token
	return credentials.Save(file)
}

func RemoveGitHubToken() error {
	file, err := credentials.Load()
	if err != nil {
		return err
	}
	file.GitHubToken = ""
	return credentials.Save(file)
}

func GitHubMCPToken() (string, error) {
	file, err := credentials.Load()
	if err != nil {
		return "", err
	}
	if token := strings.TrimSpace(file.GitHubToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")); token != "" {
		return token, nil
	}
	data, err := os.ReadFile(filepath.Join(".secrets", "github-pat"))
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("read local GitHub token file: %w", err)
}
