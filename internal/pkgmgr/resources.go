package pkgmgr

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResourcePaths returns the files of one resource type inside an installed
// package. A package that declares the resource type in its "pi" manifest
// uses the declared patterns; otherwise the conventional directory is used.
func (p *Package) ResourcePaths(resourceType ResourceType) ([]string, error) {
	entries := p.Manifest.Entries(resourceType)
	if len(entries) == 0 {
		entries = []string{string(resourceType)}
	}
	var included []string
	excluded := make(map[string]bool)
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "!") {
			for _, path := range p.matchPattern(resourceType, strings.TrimPrefix(trimmed, "!")) {
				excluded[path] = true
			}
			continue
		}
		included = append(included, p.matchPattern(resourceType, trimmed)...)
	}
	result := make([]string, 0, len(included))
	seen := make(map[string]bool)
	for _, path := range included {
		if excluded[path] || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

// SkillFiles returns the SKILL.md files of an installed package. Each returned
// path is the root of one Agent Skill.
func (p *Package) SkillFiles() ([]string, error) {
	return p.ResourcePaths(ResourceSkills)
}

// matchPattern resolves one manifest pattern against the package root. A
// pattern that names a directory collects the matching files below it, and a
// pattern with wildcards is matched against every file in the package.
func (p *Package) matchPattern(resourceType ResourceType, pattern string) []string {
	normalized := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "./")
	if normalized == "" {
		return nil
	}
	rooted := filepath.Join(p.Root, filepath.FromSlash(normalized))
	info, err := os.Stat(rooted)
	if err == nil && info.IsDir() {
		return p.collectFromDirectory(resourceType, rooted)
	}
	if err == nil && info.Mode().IsRegular() {
		if matchesType(resourceType, rooted) {
			return []string{rooted}
		}
		return nil
	}
	return p.collectByGlob(resourceType, normalized)
}

func (p *Package) collectFromDirectory(resourceType ResourceType, directory string) []string {
	var result []string
	_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if matchesType(resourceType, path) {
			result = append(result, path)
		}
		return nil
	})
	return result
}

func (p *Package) collectByGlob(resourceType ResourceType, pattern string) []string {
	var result []string
	_ = filepath.WalkDir(p.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(p.Root, path)
		if err != nil {
			return nil
		}
		if !matchGlob(pattern, filepath.ToSlash(relative)) {
			return nil
		}
		if matchesType(resourceType, path) {
			result = append(result, path)
		}
		return nil
	})
	return result
}

// matchesType reports whether a file is a resource of the given type.
func matchesType(resourceType ResourceType, path string) bool {
	switch resourceType {
	case ResourceSkills:
		return strings.EqualFold(filepath.Base(path), "SKILL.md")
	case ResourceExtensions:
		extension := strings.ToLower(filepath.Ext(path))
		return extension == ".ts" || extension == ".js" || extension == ".mjs" || extension == ".cjs"
	case ResourcePrompts:
		return strings.EqualFold(filepath.Ext(path), ".md")
	case ResourceThemes:
		return strings.EqualFold(filepath.Ext(path), ".json")
	}
	return false
}

// matchGlob reports whether a slash-separated path matches a pattern that may
// contain **, * and ? wildcards.
func matchGlob(pattern, path string) bool {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	return matchSegments(patternParts, pathParts)
}

func matchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(path); skip++ {
			if matchSegments(pattern[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if !matchSegment(pattern[0], path[0]) {
		return false
	}
	return matchSegments(pattern[1:], path[1:])
}

func matchSegment(pattern, name string) bool {
	return matchSegmentAt([]rune(pattern), []rune(name))
}

func matchSegmentAt(pattern, name []rune) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for skip := 0; skip <= len(name); skip++ {
				if matchSegmentAt(pattern[1:], name[skip:]) {
					return true
				}
			}
			return false
		case '?':
			if len(name) == 0 {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		default:
			if len(name) == 0 || name[0] != pattern[0] {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		}
	}
	return len(name) == 0
}
