package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Runtime contains tunable limits for one agent run. Values are intentionally
// kept together so a run can be reproduced from its config file.
type Runtime struct {
	MaxTurns                         int               `json:"max_turns"`
	MaxToolCalls                     int               `json:"max_tool_calls"`
	MaxRepeatedFailedCommands        int               `json:"max_repeated_failed_commands"`
	MaxContextTokens                 int               `json:"max_context_tokens"`
	ContextCompactionThresholdTokens int               `json:"context_compaction_threshold_tokens"`
	SummaryTokens                    int               `json:"summary_tokens"`
	MaxToolResultBytes               int               `json:"max_tool_result_bytes"`
	MaxRetrievalQueryBytes           int               `json:"max_retrieval_query_bytes"`
	ModelOutputTokens                int               `json:"model_output_tokens"`
	ModelReasoningEffort             string            `json:"model_reasoning_effort"`
	ToolTimeoutMS                    int               `json:"tool_timeout_ms"`
	ModelTimeoutMS                   int               `json:"model_timeout_ms"`
	MaxRetries                       int               `json:"max_retries"`
	CommandExecutionImage            string            `json:"command_execution_image"`
	CommandFilesystemScope           string            `json:"command_filesystem_scope"`
	AllowContainerNetwork            bool              `json:"allow_container_network"`
	DisableConfiguredMCPServers      bool              `json:"disable_configured_mcp_servers"`
	DisableWorkflowExtensions        bool              `json:"disable_workflow_extensions"`
	SkillsEnabled                    bool              `json:"skills_enabled"`
	MemoryEnabled                    bool              `json:"memory_enabled"`
	MemoryAutoWrite                  bool              `json:"memory_auto_write"`
	ProjectInstructionsFile          string            `json:"project_instructions_file,omitempty"`
	MemoryFile                       string            `json:"memory_file,omitempty"`
	MemoryRepositoryID               string            `json:"memory_repository_id,omitempty"`
	WebSearchEnabled                 bool              `json:"web_search_enabled"`
	DefaultToolPermission            string            `json:"default_tool_permission"`
	ToolPermissions                  map[string]string `json:"tool_permissions,omitempty"`
	CommandTools                     []CommandTool     `json:"command_tools,omitempty"`
}

// CommandTool describes a trusted external executable exposed as one Agent tool.
type CommandTool struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	InputSchema       json.RawMessage `json:"input_schema"`
	Command           string          `json:"command"`
	Args              []string        `json:"args,omitempty"`
	ModifiesWorkspace bool            `json:"modifies_workspace,omitempty"`
}

var commandToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
var memoryRepositoryIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

func Defaults() Runtime {
	return Runtime{
		MaxTurns: 128, MaxToolCalls: 256, MaxContextTokens: 64000,
		ContextCompactionThresholdTokens: 0,
		SummaryTokens:                    800, MaxToolResultBytes: 16 * 1024,
		MaxRetrievalQueryBytes: 8 * 1024,
		ModelOutputTokens:      16384, ModelReasoningEffort: "max", ToolTimeoutMS: 600000,
		ModelTimeoutMS: 120000, MaxRetries: 2,
		CommandExecutionImage:  "debian:trixie-slim",
		CommandFilesystemScope: "unrestricted",
		AllowContainerNetwork:  false,
		SkillsEnabled:          true, MemoryEnabled: true, MemoryAutoWrite: false, DefaultToolPermission: "allow",
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
	configPath, err := filepath.Abs(path)
	if err != nil {
		return Runtime{}, fmt.Errorf("resolve config path: %w", err)
	}
	for _, configuredPath := range []*string{&cfg.ProjectInstructionsFile, &cfg.MemoryFile} {
		if *configuredPath == "" || filepath.IsAbs(*configuredPath) {
			continue
		}
		resolved, err := filepath.Abs(filepath.Join(filepath.Dir(configPath), *configuredPath))
		if err != nil {
			return Runtime{}, fmt.Errorf("resolve configured project context path: %w", err)
		}
		*configuredPath = resolved
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
	if c.MaxRepeatedFailedCommands < 0 || c.MaxRepeatedFailedCommands > 20 {
		return errors.New("max_repeated_failed_commands must be between 0 and 20")
	}
	if c.MaxContextTokens < 512 || c.MaxContextTokens > 1000000 {
		return errors.New("max_context_tokens must be between 512 and 1000000")
	}
	if c.ContextCompactionThresholdTokens < 0 || c.ContextCompactionThresholdTokens > c.MaxContextTokens {
		return errors.New("context_compaction_threshold_tokens must be 0 or no greater than max_context_tokens")
	}
	if c.SummaryTokens >= c.EffectiveContextCompactionThresholdTokens() {
		return errors.New("summary_tokens must be less than context_compaction_threshold_tokens")
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
	switch c.ModelReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return errors.New("model_reasoning_effort must be one of low, medium, high, xhigh, or max")
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
	if c.MemoryAutoWrite && !c.MemoryEnabled {
		return errors.New("memory_auto_write requires memory_enabled")
	}
	for name, path := range map[string]string{
		"project_instructions_file": c.ProjectInstructionsFile,
		"memory_file":               c.MemoryFile,
	} {
		if path != "" && (!filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n")) {
			return fmt.Errorf("%s must be an absolute filesystem path", name)
		}
	}
	if c.MemoryFile == "" && c.MemoryRepositoryID != "" {
		return errors.New("memory_repository_id requires memory_file")
	}
	if c.MemoryFile != "" && c.MemoryRepositoryID == "" {
		return errors.New("memory_file requires a stable memory_repository_id")
	}
	if len(c.MemoryRepositoryID) > 128 || (c.MemoryRepositoryID != "" && !memoryRepositoryIDPattern.MatchString(c.MemoryRepositoryID)) {
		return errors.New("memory_repository_id must be 1–128 letters, digits, '.', '_', ':', '/', or '-' and start with a letter or digit")
	}
	if c.CommandExecutionImage == "" || strings.TrimSpace(c.CommandExecutionImage) != c.CommandExecutionImage || strings.ContainsAny(c.CommandExecutionImage, " \t\r\n") || strings.HasPrefix(c.CommandExecutionImage, "-") {
		return errors.New("command_execution_image must be a non-empty Docker image reference without whitespace")
	}
	if c.CommandFilesystemScope != "unrestricted" && c.CommandFilesystemScope != "workspace" {
		return errors.New("command_filesystem_scope must be 'unrestricted' or 'workspace'")
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
	seenCommandTools := make(map[string]bool, len(c.CommandTools))
	for i, tool := range c.CommandTools {
		if !commandToolName.MatchString(tool.Name) {
			return fmt.Errorf("command_tools[%d].name must start with a letter and contain only letters, digits, '_' or '-'", i)
		}
		if seenCommandTools[tool.Name] {
			return fmt.Errorf("command_tools contains duplicate name %q", tool.Name)
		}
		seenCommandTools[tool.Name] = true
		if strings.TrimSpace(tool.Description) == "" || len(tool.Description) > 4096 {
			return fmt.Errorf("command_tools[%d].description must contain 1–4096 characters", i)
		}
		if strings.TrimSpace(tool.Command) == "" || strings.TrimSpace(tool.Command) != tool.Command {
			return fmt.Errorf("command_tools[%d].command must be a non-empty executable", i)
		}
		if len(tool.Args) > 64 {
			return fmt.Errorf("command_tools[%d].args may contain at most 64 arguments", i)
		}
		for _, arg := range tool.Args {
			if len(arg) > 4096 {
				return fmt.Errorf("command_tools[%d] argument exceeds 4096 characters", i)
			}
		}
		if len(tool.InputSchema) == 0 || len(tool.InputSchema) > 32*1024 || !json.Valid(tool.InputSchema) {
			return fmt.Errorf("command_tools[%d].input_schema must be valid JSON no larger than 32 KiB", i)
		}
		var schema struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema.Type != "object" {
			return fmt.Errorf("command_tools[%d].input_schema must define an object schema", i)
		}
	}
	return nil
}

// EffectiveContextCompactionThresholdTokens returns the configured threshold,
// or a bounded default that fits within the model context budget.
func (c Runtime) EffectiveContextCompactionThresholdTokens() int {
	if c.ContextCompactionThresholdTokens > 0 {
		return c.ContextCompactionThresholdTokens
	}
	if c.MaxContextTokens > 0 && c.MaxContextTokens < 30000 {
		return c.MaxContextTokens
	}
	return 30000
}

func validToolPermission(permission string) bool {
	return permission == "allow" || permission == "deny"
}
