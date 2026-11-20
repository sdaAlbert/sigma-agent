package pkgmgr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ResourceType is one kind of resource a package can contribute.
type ResourceType string

const (
	// ResourceExtensions are JavaScript and TypeScript extension modules.
	ResourceExtensions ResourceType = "extensions"
	// ResourceSkills are Agent Skills, each rooted at a SKILL.md file.
	ResourceSkills ResourceType = "skills"
	// ResourcePrompts are Markdown prompt templates.
	ResourcePrompts ResourceType = "prompts"
	// ResourceThemes are JSON theme definitions.
	ResourceThemes ResourceType = "themes"
)

// ResourceTypes lists every resource type a package can declare.
var ResourceTypes = []ResourceType{ResourceExtensions, ResourceSkills, ResourcePrompts, ResourceThemes}

// Manifest contains Pi-compatible resource declarations and Sigma-specific
// executable tool declarations from a package's package.json.
type Manifest struct {
	Extensions    []string      `json:"extensions,omitempty"`
	Skills        []string      `json:"skills,omitempty"`
	Prompts       []string      `json:"prompts,omitempty"`
	Themes        []string      `json:"themes,omitempty"`
	ChangelogPath string        `json:"changelogPath,omitempty"`
	CommandTools  []CommandTool `json:"commandTools,omitempty"`
}

// CommandTool declares an executable-backed Agent tool in a Sigma package.
// Command and Args are fixed by the package author; model input is sent as JSON
// on stdin. ${package} and ${workspace} are expanded before execution.
type CommandTool struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	InputSchema       json.RawMessage `json:"input_schema"`
	Command           string          `json:"command"`
	Args              []string        `json:"args,omitempty"`
	ModifiesWorkspace bool            `json:"modifies_workspace,omitempty"`
}

// Validate checks the manifest fields needed to expose a command as a tool.
func (t CommandTool) Validate() error {
	if !validPackageToolName(t.Name) {
		return errors.New("name must start with a letter and contain only letters, digits, '_' or '-' (max 64 characters)")
	}
	if strings.TrimSpace(t.Description) == "" || len(t.Description) > 4096 {
		return errors.New("description must contain 1–4096 characters")
	}
	if strings.TrimSpace(t.Command) == "" || strings.TrimSpace(t.Command) != t.Command {
		return errors.New("command must be a non-empty executable")
	}
	if len(t.Args) > 64 {
		return errors.New("args may contain at most 64 arguments")
	}
	for _, arg := range t.Args {
		if len(arg) > 4096 {
			return errors.New("an argument exceeds 4096 characters")
		}
	}
	if len(t.InputSchema) == 0 || len(t.InputSchema) > 32*1024 || !json.Valid(t.InputSchema) {
		return errors.New("input_schema must be valid JSON no larger than 32 KiB")
	}
	var schema struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(t.InputSchema, &schema); err != nil || schema.Type != "object" {
		return errors.New("input_schema must define an object schema")
	}
	return nil
}

func validPackageToolName(name string) bool {
	if len(name) == 0 || len(name) > 64 || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return false
	}
	for _, r := range name[1:] {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// HasResources reports whether the package declares resources or tools. A
// manifest that only carries metadata still uses directory auto-discovery.
func (m Manifest) HasResources() bool {
	return len(m.Extensions) > 0 || len(m.Skills) > 0 || len(m.Prompts) > 0 || len(m.Themes) > 0 || len(m.CommandTools) > 0
}

// Entries returns the declared patterns for one resource type.
func (m Manifest) Entries(resourceType ResourceType) []string {
	switch resourceType {
	case ResourceExtensions:
		return m.Extensions
	case ResourceSkills:
		return m.Skills
	case ResourcePrompts:
		return m.Prompts
	case ResourceThemes:
		return m.Themes
	}
	return nil
}

type packageJSON struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Pi      json.RawMessage `json:"pi"`
	Sigma   json.RawMessage `json:"sigma"`
}

// ReadManifest reads Pi-compatible resources and Sigma tools from a package's
// package.json. An unusable optional "pi" object is ignored for compatibility;
// malformed Sigma tool declarations are rejected because they are executable.
func ReadManifest(packageRoot string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return manifest, nil
		}
		return manifest, err
	}
	var decoded packageJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return manifest, err
	}
	if len(decoded.Pi) > 0 {
		var raw struct {
			Extensions    []string `json:"extensions"`
			Skills        []string `json:"skills"`
			Prompts       []string `json:"prompts"`
			Themes        []string `json:"themes"`
			ChangelogPath string   `json:"changelogPath"`
		}
		if err := json.Unmarshal(decoded.Pi, &raw); err == nil {
			manifest.Extensions = stringList(raw.Extensions)
			manifest.Skills = stringList(raw.Skills)
			manifest.Prompts = stringList(raw.Prompts)
			manifest.Themes = stringList(raw.Themes)
			manifest.ChangelogPath = raw.ChangelogPath
		}
	}
	if len(decoded.Sigma) > 0 {
		var raw struct {
			Tools []CommandTool `json:"tools"`
		}
		if err := json.Unmarshal(decoded.Sigma, &raw); err != nil {
			return Manifest{}, errors.New("sigma package manifest must be a JSON object")
		}
		seen := make(map[string]bool, len(raw.Tools))
		for index, tool := range raw.Tools {
			if err := tool.Validate(); err != nil {
				return Manifest{}, errors.New("sigma.tools[" + strconv.Itoa(index) + "]: " + err.Error())
			}
			if seen[tool.Name] {
				return Manifest{}, errors.New("sigma.tools contains duplicate tool " + tool.Name)
			}
			seen[tool.Name] = true
		}
		manifest.CommandTools = raw.Tools
	}
	return manifest, nil
}

// ReadPackageInfo reads the name and version of an installed package.
func ReadPackageInfo(packageRoot string) (name string, version string, err error) {
	data, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", nil
		}
		return "", "", err
	}
	var decoded packageJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", "", err
	}
	return decoded.Name, decoded.Version, nil
}

func stringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
