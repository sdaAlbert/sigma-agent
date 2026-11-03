package pkgmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RegistryFileName is the file that records the installed packages of one
// scope.
const RegistryFileName = "packages.json"

// Entry is one installed package recorded in a registry.
type Entry struct {
	// Source is the source string the package was installed from.
	Source string `json:"source"`
	// Version is the version that was installed, when it is known.
	Version string `json:"version,omitempty"`
}

// Registry records the packages installed for one scope.
type Registry struct {
	// Path is the registry file.
	Path string
}

// LoadRegistry reads the packages recorded in a registry file. A missing file
// is an empty registry.
func LoadRegistry(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var document struct {
		Packages []Entry `json:"packages"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode package registry %s: %w", path, err)
	}
	return document.Packages, nil
}

// SaveRegistry writes the packages of one scope back to its registry file.
func SaveRegistry(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create package registry directory: %w", err)
	}
	document := struct {
		Packages []Entry `json:"packages"`
	}{Packages: entries}
	if document.Packages == nil {
		document.Packages = []Entry{}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Add records a package source in the registry.
func (r Registry) Add(entry Entry) ([]Entry, error) {
	entries, err := LoadRegistry(r.Path)
	if err != nil {
		return nil, err
	}
	replaced := false
	for index, existing := range entries {
		if samePackage(existing.Source, entry.Source) {
			entries[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	if err := SaveRegistry(r.Path, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Remove drops a package source from the registry.
func (r Registry) Remove(source string) ([]Entry, error) {
	entries, err := LoadRegistry(r.Path)
	if err != nil {
		return nil, err
	}
	kept := make([]Entry, 0, len(entries))
	for _, existing := range entries {
		if samePackage(existing.Source, source) {
			continue
		}
		kept = append(kept, existing)
	}
	if err := SaveRegistry(r.Path, kept); err != nil {
		return nil, err
	}
	return kept, nil
}

// samePackage compares two package sources by identity, ignoring the version
// and ref so that npm:pkg and npm:pkg@1.2.3 name the same package.
func samePackage(left, right string) bool {
	leftSource, leftErr := ParseSource(left)
	rightSource, rightErr := ParseSource(right)
	if leftErr != nil || rightErr != nil {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	if leftSource.Type != rightSource.Type {
		return false
	}
	switch leftSource.Type {
	case SourceNPM:
		return leftSource.Name == rightSource.Name
	case SourceGit:
		return leftSource.Name == rightSource.Name && leftSource.Spec == rightSource.Spec
	default:
		return leftSource.Path == rightSource.Path
	}
}

// UserDir returns the directory that holds user-scoped Sigma Agent state.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".sigma-agent"), nil
}

// PackagesRoot returns the directory that holds installed packages for one
// scope.
func PackagesRoot(scopeDir string) string {
	return filepath.Join(scopeDir, "packages")
}
