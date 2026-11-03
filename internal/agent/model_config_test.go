package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateModelConfig points the user configuration directory at a temporary
// folder and clears every provider environment variable so a test only observes
// the credential source it sets up itself.
func isolateModelConfig(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", directory)
	t.Setenv("HOME", directory)
	t.Setenv("APPDATA", directory)
	for _, name := range []string{
		"SIGMA_AGENT_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"MINIMAX_API_KEY", "OPENAI_API_KEY", "GITHUB_PERSONAL_ACCESS_TOKEN",
		"GITHUB_TOKEN", "GH_TOKEN",
	} {
		t.Setenv(name, "")
	}
	return directory
}

func TestSaveAndRemoveProviderAPIKey(t *testing.T) {
	isolateModelConfig(t)

	if err := SaveProviderAPIKey("openai", "saved-openai-key"); err != nil {
		t.Fatalf("SaveProviderAPIKey error = %v", err)
	}
	configured, err := ModelProviderHasAPIKey("openai")
	if err != nil {
		t.Fatalf("ModelProviderHasAPIKey error = %v", err)
	}
	if !configured {
		t.Fatal("ModelProviderHasAPIKey(openai) = false, want true after SaveProviderAPIKey")
	}
	resolved, err := providerAPIKey("openai")
	if err != nil {
		t.Fatalf("providerAPIKey error = %v", err)
	}
	if resolved != "saved-openai-key" {
		t.Fatalf("providerAPIKey(openai) = %q, want %q", resolved, "saved-openai-key")
	}

	if err := RemoveProviderAPIKey("openai"); err != nil {
		t.Fatalf("RemoveProviderAPIKey error = %v", err)
	}
	configured, err = ModelProviderHasAPIKey("openai")
	if err != nil {
		t.Fatalf("ModelProviderHasAPIKey after removal error = %v", err)
	}
	if configured {
		t.Fatal("ModelProviderHasAPIKey(openai) = true after RemoveProviderAPIKey, want false")
	}
}

func TestSaveAndRemoveGitHubToken(t *testing.T) {
	isolateModelConfig(t)

	if err := SaveGitHubToken("ghp_saved_token"); err != nil {
		t.Fatalf("SaveGitHubToken error = %v", err)
	}
	token, err := GitHubMCPToken()
	if err != nil {
		t.Fatalf("GitHubMCPToken error = %v", err)
	}
	if token != "ghp_saved_token" {
		t.Fatalf("GitHubMCPToken() = %q, want %q", token, "ghp_saved_token")
	}
	if !GitHubMCPConfigured() {
		t.Fatal("GitHubMCPConfigured() = false after SaveGitHubToken, want true")
	}

	if err := RemoveGitHubToken(); err != nil {
		t.Fatalf("RemoveGitHubToken error = %v", err)
	}
	token, err = GitHubMCPToken()
	if err != nil {
		t.Fatalf("GitHubMCPToken after removal error = %v", err)
	}
	if token != "" {
		t.Fatalf("GitHubMCPToken() = %q after RemoveGitHubToken, want empty", token)
	}
	if GitHubMCPConfigured() {
		t.Fatal("GitHubMCPConfigured() = true after RemoveGitHubToken, want false")
	}
}

// Regression: ModelProviderHasAPIKey used to consult the environment before the
// saved configuration file while providerAPIKey - the resolver that actually
// builds the model - consults the saved configuration file first. When both
// sources were present the two disagreed, so the setup prompt could report a
// provider as "key ready" while a different secret was the live one.
//
// The presence check must agree with the canonical resolver.
func TestModelProviderHasAPIKeyAgreesWithTheCanonicalResolver(t *testing.T) {
	isolateModelConfig(t)

	if err := SaveProviderAPIKey("openai", "saved-config-key"); err != nil {
		t.Fatalf("SaveProviderAPIKey error = %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "env-key")

	configured, err := ModelProviderHasAPIKey("openai")
	if err != nil {
		t.Fatalf("ModelProviderHasAPIKey error = %v", err)
	}
	if !configured {
		t.Fatal("ModelProviderHasAPIKey(openai) = false, want true when both sources are configured")
	}
	resolved, err := providerAPIKey("openai")
	if err != nil {
		t.Fatalf("providerAPIKey error = %v", err)
	}
	if resolved != "saved-config-key" {
		t.Fatalf("providerAPIKey(openai) = %q, want %q (the saved configuration wins)", resolved, "saved-config-key")
	}
}

// An environment-only credential must still work: the setup prompt tells the
// operator that an environment variable can provide the key.
func TestModelProviderUsesEnvironmentKeyWhenNoSavedKeyExists(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("OPENAI_API_KEY", "env-key")

	configured, err := ModelProviderHasAPIKey("openai")
	if err != nil {
		t.Fatalf("ModelProviderHasAPIKey error = %v", err)
	}
	if !configured {
		t.Fatal("ModelProviderHasAPIKey(openai) = false, want true for an environment-only key")
	}
	resolved, err := providerAPIKey("openai")
	if err != nil {
		t.Fatalf("providerAPIKey error = %v", err)
	}
	if resolved != "env-key" {
		t.Fatalf("providerAPIKey(openai) = %q, want %q", resolved, "env-key")
	}
}

func TestModelProviderWithoutAnyKeyIsNotConfigured(t *testing.T) {
	isolateModelConfig(t)

	configured, err := ModelProviderHasAPIKey("openai")
	if err != nil {
		t.Fatalf("ModelProviderHasAPIKey error = %v", err)
	}
	if configured {
		t.Fatal("ModelProviderHasAPIKey(openai) = true with no credential configured, want false")
	}
	resolved, err := providerAPIKey("openai")
	if err != nil {
		t.Fatalf("providerAPIKey error = %v", err)
	}
	if resolved != "" {
		t.Fatalf("providerAPIKey(openai) = %q with no credential configured, want empty", resolved)
	}
}

func TestUnsupportedProviderIsRejected(t *testing.T) {
	isolateModelConfig(t)

	if _, ok := NormalizeModelProvider("not-a-provider"); ok {
		t.Fatal("NormalizeModelProvider(not-a-provider) reported a supported provider")
	}
	if err := SaveProviderAPIKey("not-a-provider", "key"); err == nil {
		t.Fatal("SaveProviderAPIKey accepted an unsupported provider, want error")
	}
	if err := RemoveProviderAPIKey("not-a-provider"); err == nil {
		t.Fatal("RemoveProviderAPIKey accepted an unsupported provider, want error")
	}
	if _, err := ModelProviderHasAPIKey("not-a-provider"); err == nil {
		t.Fatal("ModelProviderHasAPIKey accepted an unsupported provider, want error")
	}
}

func TestCredentialsWithControlCharactersAreRejected(t *testing.T) {
	isolateModelConfig(t)

	for _, key := range []string{"", "   ", "key\r\nInjected: value", "key\nvalue", "key\x00value"} {
		if err := SaveProviderAPIKey("openai", key); err == nil {
			t.Fatalf("SaveProviderAPIKey accepted %q, want error", key)
		}
		if err := SaveGitHubToken(key); err == nil {
			t.Fatalf("SaveGitHubToken accepted %q, want error", key)
		}
	}
}

// A credential must never be written into the repository worktree.
func TestCredentialsAreNotWrittenIntoTheWorkingDirectory(t *testing.T) {
	directory := isolateModelConfig(t)
	worktree := t.TempDir()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error = %v", err)
	}
	if err := os.Chdir(worktree); err != nil {
		t.Fatalf("Chdir error = %v", err)
	}
	defer func() { _ = os.Chdir(previous) }()

	if err := SaveProviderAPIKey("openai", "secret-value"); err != nil {
		t.Fatalf("SaveProviderAPIKey error = %v", err)
	}
	if err := SaveGitHubToken("ghp_secret_value"); err != nil {
		t.Fatalf("SaveGitHubToken error = %v", err)
	}

	entries, err := os.ReadDir(worktree)
	if err != nil {
		t.Fatalf("ReadDir error = %v", err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(worktree, entry.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "secret-value") || strings.Contains(string(data), "ghp_secret_value") {
			t.Fatalf("credential leaked into %s", entry.Name())
		}
	}

	// The credential must live under the user configuration directory instead.
	configPath := filepath.Join(directory, "sigma-agent", "model-auth.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", configPath, err)
	}
	if !strings.Contains(string(data), "secret-value") {
		t.Fatalf("saved configuration %s does not contain the stored key", configPath)
	}
}

func TestDefaultModelSelectionRoundTrip(t *testing.T) {
	isolateModelConfig(t)

	if err := SaveDefaultModelSelection(ModelSelection{Provider: "anthropic", Model: "claude-sonnet-4"}); err != nil {
		t.Fatalf("SaveDefaultModelSelection error = %v", err)
	}
	selection, err := LoadDefaultModelSelection()
	if err != nil {
		t.Fatalf("LoadDefaultModelSelection error = %v", err)
	}
	if selection.Provider != "anthropic" || selection.Model != "claude-sonnet-4" {
		t.Fatalf("LoadDefaultModelSelection() = %#v, want anthropic/claude-sonnet-4", selection)
	}
}
