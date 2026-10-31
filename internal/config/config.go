package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Runtime contains tunable limits for one agent run. Values are intentionally
// kept together so a run can be reproduced from its config file.
type Runtime struct {
	MaxTurns               int               `json:"max_turns"`
	MaxToolCalls           int               `json:"max_tool_calls"`
	MaxContextTokens       int               `json:"max_context_tokens"`
	SummaryTokens          int               `json:"summary_tokens"`
	MaxToolResultBytes     int               `json:"max_tool_result_bytes"`
	MaxRetrievalQueryBytes int               `json:"max_retrieval_query_bytes"`
	ModelOutputTokens      int               `json:"model_output_tokens"`
	ToolTimeoutMS          int               `json:"tool_timeout_ms"`
	ModelTimeoutMS         int               `json:"model_timeout_ms"`
	MaxRetries             int               `json:"max_retries"`
	GoTestImage            string            `json:"go_test_image"`
	SkillsEnabled          bool              `json:"skills_enabled"`
	MemoryEnabled          bool              `json:"memory_enabled"`
	WebSearchEnabled       bool              `json:"web_search_enabled"`
	DefaultToolPermission  string            `json:"default_tool_permission"`
	ToolPermissions        map[string]string `json:"tool_permissions,omitempty"`
}

func Defaults() Runtime {
	return Runtime{
		MaxTurns: 32, MaxToolCalls: 64, MaxContextTokens: 64000,
		SummaryTokens: 800, MaxToolResultBytes: 16 * 1024,
		MaxRetrievalQueryBytes: 8 * 1024,
		ModelOutputTokens:      16384, ToolTimeoutMS: 60000,
		ModelTimeoutMS: 120000, MaxRetries: 2,
		GoTestImage: "golang:1.26", SkillsEnabled: true, MemoryEnabled: true, DefaultToolPermission: "allow",
	}
}

func Load(path string) (Runtime, error) {
	cfg := Defaults()
	if path == "" {
		return cfg, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Runtime{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Runtime{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Runtime{}, fmt.Errorf("decode config %s: expected one JSON object", path)
	}
	if err := cfg.Validate(); err != nil {
		return Runtime{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (c Runtime) Validate() error {
	if c.MaxTurns < 1 || c.MaxTurns > 512 {
		return errors.New("max_turns must be between 1 and 512")
	}
	if c.MaxToolCalls < 1 || c.MaxToolCalls > 4096 {
		return errors.New("max_tool_calls must be between 1 and 4096")
	}
	if c.MaxContextTokens < 512 || c.MaxContextTokens > 1000000 {
		return errors.New("max_context_tokens must be between 512 and 1000000")
	}
	if c.SummaryTokens < 0 || c.SummaryTokens >= c.MaxContextTokens {
		return errors.New("summary_tokens must be non-negative and less than max_context_tokens")
	}
	if c.MaxToolResultBytes < 1024 || c.MaxToolResultBytes > 4*1024*1024 {
		return errors.New("max_tool_result_bytes must be between 1024 and 4194304")
	}
	if c.MaxRetrievalQueryBytes < 500 || c.MaxRetrievalQueryBytes > 64*1024 {
		return errors.New("max_retrieval_query_bytes must be between 500 and 65536")
	}
	if c.ModelOutputTokens < 256 || c.ModelOutputTokens > 65536 {
		return errors.New("model_output_tokens must be between 256 and 65536")
	}
	if c.ToolTimeoutMS < 1000 || c.ToolTimeoutMS > 30*60*1000 {
		return errors.New("tool_timeout_ms must be between 1000 and 1800000")
	}
	if c.ModelTimeoutMS < 1000 || c.ModelTimeoutMS > 30*60*1000 {
		return errors.New("model_timeout_ms must be between 1000 and 1800000")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return errors.New("max_retries must be between 0 and 10")
	}
	if c.GoTestImage == "" || strings.TrimSpace(c.GoTestImage) != c.GoTestImage || strings.ContainsAny(c.GoTestImage, " \t\r\n") || strings.HasPrefix(c.GoTestImage, "-") {
		return errors.New("go_test_image must be a non-empty Docker image reference without whitespace")
	}
	if !validToolPermission(c.DefaultToolPermission) {
		return errors.New("default_tool_permission must be 'allow' or 'deny'")
	}
	for name, permission := range c.ToolPermissions {
		if strings.TrimSpace(name) == "" {
			return errors.New("tool_permissions keys must be non-empty tool names")
		}
		if !validToolPermission(permission) {
			return fmt.Errorf("tool_permissions[%q] must be 'allow' or 'deny'", name)
		}
	}
	return nil
}

func validToolPermission(permission string) bool {
	return permission == "allow" || permission == "deny"
}
