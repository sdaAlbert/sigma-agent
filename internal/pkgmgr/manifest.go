package pkgmgr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// Manifest is the "pi" field of a package's package.json.
type Manifest struct {
	Extensions    []string `json:"extensions,omitempty"`
	Skills        []string `json:"skills,omitempty"`
	Prompts       []string `json:"prompts,omitempty"`
	Themes        []string `json:"themes,omitempty"`
	ChangelogPath string   `json:"changelogPath,omitempty"`
}

// HasResources reports whether the manifest declares any resource type. A
// manifest that only carries metadata still uses directory auto-discovery.
func (m Manifest) HasResources() bool {
	return len(m.Extensions) > 0 || len(m.Skills) > 0 || len(m.Prompts) > 0 || len(m.Themes) > 0
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
}

// ReadManifest reads the "pi" field of a package's package.json. It returns a
// zero manifest when the file has no usable "pi" object.
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
	if len(decoded.Pi) == 0 {
		return manifest, nil
	}
	var raw struct {
		Extensions    []string `json:"extensions"`
		Skills        []string `json:"skills"`
		Prompts       []string `json:"prompts"`
		Themes        []string `json:"themes"`
		ChangelogPath string   `json:"changelogPath"`
	}
	if err := json.Unmarshal(decoded.Pi, &raw); err != nil {
		// A "pi" object that is not a resource manifest is ignored, the
		// same way Pi ignores a manifest it cannot read.
		return Manifest{}, nil
	}
	manifest.Extensions = stringList(raw.Extensions)
	manifest.Skills = stringList(raw.Skills)
	manifest.Prompts = stringList(raw.Prompts)
	manifest.Themes = stringList(raw.Themes)
	manifest.ChangelogPath = raw.ChangelogPath
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
