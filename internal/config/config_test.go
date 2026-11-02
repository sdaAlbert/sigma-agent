package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesValuesOverDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"max_turns":48,"max_context_tokens":96000,"go_test_image":"mswebench/zeromicro_m_go-zero:pr-2537"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxTurns != 48 || cfg.MaxContextTokens != 96000 {
		t.Fatalf("loaded limits = (%d, %d), want (48, 96000)", cfg.MaxTurns, cfg.MaxContextTokens)
	}
	if cfg.MaxToolCalls != Defaults().MaxToolCalls {
		t.Fatalf("unspecified max_tool_calls = %d, want default %d", cfg.MaxToolCalls, Defaults().MaxToolCalls)
	}
	if !cfg.SkillsEnabled {
		t.Fatal("skills_enabled should default to true")
	}
	if cfg.GoTestImage != "mswebench/zeromicro_m_go-zero:pr-2537" {
		t.Fatalf("go_test_image = %q, want configured benchmark image", cfg.GoTestImage)
	}
}

func TestSkillsCanBeDisabledForMatchedEvaluations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"skills_enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillsEnabled {
		t.Fatal("skills_enabled=false was ignored")
	}
}

func TestLoadRejectsUnknownKeysAndOutOfRangeValues(t *testing.T) {
	for _, contents := range []string{
		`{"max_turn":48}`,
		`{"max_turns":0}`,
		`{"go_test_image":" "}`,
		`{"go_test_image":"-not-an-image"}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load(%s) succeeded, want validation error", contents)
		}
	}
}

func TestAllowContainerNetworkDefaultsOffAndLoadsFromFile(t *testing.T) {
	defaults := Defaults()
	if defaults.AllowContainerNetwork {
		t.Fatal("Defaults().AllowContainerNetwork = true, want false")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"allow_container_network":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AllowContainerNetwork {
		t.Fatal("Load().AllowContainerNetwork = false, want true")
	}
}

func TestAllowContainerNetworkStaysOffWithoutOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"max_turns":48}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AllowContainerNetwork {
		t.Fatal("Load().AllowContainerNetwork = true, want false")
	}
}
