// Package pkgmgr installs and locates the packages that extend Sigma Agent
// with skills, prompts, themes, and MCP servers.
//
// The package format follows the convention used by Pi Agent: a package is an
// ordinary npm or git package that either declares its resources in the "pi"
// field of its package.json, or exposes the conventional extensions/, skills/,
// prompts/, and themes/ directories.
package pkgmgr

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SourceType identifies where a package comes from.
type SourceType string

const (
	// SourceNPM is a package published to the npm registry.
	SourceNPM SourceType = "npm"
	// SourceGit is a package cloned from a git repository.
	SourceGit SourceType = "git"
	// SourceLocal is a package that already exists on disk.
	SourceLocal SourceType = "local"
)

// Source is a parsed package source.
type Source struct {
	// Type is the source kind.
	Type SourceType
	// Spec is the npm spec, or the git repository URL.
	Spec string
	// Ref is the git branch, tag, or commit to check out. Empty means the
	// remote default branch.
	Ref string
	// Path is the directory of a local package.
	Path string
	// Name is the package name, or the repository directory name.
	Name string
}

// String renders the source the way it is written in settings.
func (s Source) String() string {
	switch s.Type {
	case SourceNPM:
		return "npm:" + s.Spec
	case SourceGit:
		if s.Ref != "" {
			return "git:" + s.Spec + "@" + s.Ref
		}
		return "git:" + s.Spec
	default:
		return s.Path
	}
}

// ParseSource parses a package source string. It accepts "npm:<spec>",
// "git:<repo>[@<ref>]", a git URL, or a local directory path.
func ParseSource(raw string) (Source, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Source{}, fmt.Errorf("package source must not be empty")
	}
	switch {
	case strings.HasPrefix(trimmed, "npm:"):
		spec := strings.TrimSpace(strings.TrimPrefix(trimmed, "npm:"))
		if spec == "" {
			return Source{}, fmt.Errorf("npm package source needs a package name")
		}
		name, _ := ParseNpmSpec(spec)
		return Source{Type: SourceNPM, Spec: spec, Name: name}, nil
	case strings.HasPrefix(trimmed, "git:"):
		return parseGitSource(strings.TrimSpace(strings.TrimPrefix(trimmed, "git:")))
	case isGitURL(trimmed):
		return parseGitSource(trimmed)
	default:
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			return Source{}, fmt.Errorf("resolve local package path %q: %w", trimmed, err)
		}
		return Source{Type: SourceLocal, Path: filepath.Clean(absolute), Name: filepath.Base(absolute)}, nil
	}
}

func parseGitSource(value string) (Source, error) {
	if value == "" {
		return Source{}, fmt.Errorf("git package source needs a repository URL")
	}
	repo := value
	ref := ""
	if index := strings.LastIndex(value, "@"); index > 0 {
		candidate := value[index+1:]
		if !strings.Contains(candidate, "/") {
			repo = value[:index]
			ref = candidate
		}
	}
	repo = strings.TrimSuffix(repo, ".git")
	name := repoName(repo)
	if name == "" {
		return Source{}, fmt.Errorf("git package source %q has no repository name", value)
	}
	return Source{Type: SourceGit, Spec: repo, Ref: ref, Name: name}, nil
}

func isGitURL(value string) bool {
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "git@") || strings.HasPrefix(value, "ssh://") ||
		strings.HasPrefix(value, "git://")
}

func repoName(repo string) string {
	trimmed := strings.TrimSuffix(repo, "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	if index := strings.Index(trimmed, ":"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

// ParseNpmSpec splits an npm spec into its package name and version. Scoped
// names keep their leading @, and a trailing @version is returned separately.
func ParseNpmSpec(spec string) (name string, version string) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return "", ""
	}
	if strings.HasPrefix(trimmed, "@") {
		slash := strings.Index(trimmed, "/")
		if slash < 0 {
			return trimmed, ""
		}
		remainder := trimmed[slash+1:]
		if at := strings.Index(remainder, "@"); at >= 0 {
			return trimmed[:slash+1] + remainder[:at], remainder[at+1:]
		}
		return trimmed, ""
	}
	if at := strings.LastIndex(trimmed, "@"); at > 0 {
		return trimmed[:at], trimmed[at+1:]
	}
	return trimmed, ""
}
