// Package mcpconfig loads the MCP server configuration Sigma Agent connects
// to. The configuration uses the mcpServers shape shared by other MCP
// clients, so an existing mcp.json can be reused as is.
//
// One field extends that shape: a server may name an installed package, in
// which case its command runs with the package directory as the working
// directory and its arguments are resolved against the package root.
package mcpconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/mcp"
	"github.com/sdaAlbert/sigma-agent/internal/pkgmgr"
)

// FileName is the configuration file name used in every scope.
const FileName = "mcp.json"

// Server describes one MCP server entry.
type Server struct {
	// Command is the executable that starts the server.
	Command string `json:"command,omitempty"`
	// Args are the arguments passed to the command.
	Args []string `json:"args,omitempty"`
	// Env holds environment overrides for the server process.
	Env map[string]string `json:"env,omitempty"`
	// Cwd is the working directory of the server process. It is resolved
	// against the directory of the configuration file.
	Cwd string `json:"cwd,omitempty"`
	// Package names an installed package the server is started from. The
	// server then runs with the package directory as its working directory
	// and its arguments are resolved against the package root.
	Package string `json:"package,omitempty"`
	// Enabled turns the server off. A missing value means enabled.
	Enabled *bool `json:"enabled,omitempty"`
	// URL configures an HTTP MCP server. Sigma Agent does not support HTTP
	// servers yet.
	URL string `json:"url,omitempty"`
}

// IsEnabled reports whether the server should be started.
func (s Server) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// file is the on-disk shape of a configuration file.
type file struct {
	Servers map[string]Server `json:"mcpServers"`
}

// Entry is one configured server together with the file it came from.
type Entry struct {
	// Name is the server name, used as the tool namespace.
	Name string
	// Server is the configured server.
	Server Server
	// File is the configuration file the entry came from.
	File string
	// Dir is the directory relative paths of the entry resolve against.
	Dir string
}

// Load reads the configuration files in order. A later file replaces the
// entry of an earlier file that uses the same server name. Missing files are
// skipped.
func Load(paths []string) ([]Entry, error) {
	var entries []Entry
	index := make(map[string]int)
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		loaded, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range loaded {
			if position, exists := index[entry.Name]; exists {
				entries[position] = entry
				continue
			}
			index[entry.Name] = len(entries)
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func loadFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read MCP configuration %s: %w", path, err)
	}
	var document file
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode MCP configuration %s: %w", path, err)
	}
	directory := filepath.Dir(path)
	entries := make([]Entry, 0, len(document.Servers))
	for name, server := range document.Servers {
		if strings.TrimSpace(name) == "" {
			continue
		}
		entries = append(entries, Entry{Name: name, Server: server, File: path, Dir: directory})
	}
	return entries, nil
}

// Resolved is a configured server that is ready to start.
type Resolved struct {
	// Name is the server name, used as the tool namespace.
	Name string
	// Config is the client configuration for the server process.
	Config mcp.Config
	// File is the configuration file the server came from.
	File string
}

// Resolve turns configured entries into runnable server configurations. It
// looks up installed packages so a server can be started from the directory
// of a package.
func Resolve(entries []Entry, installers ...*pkgmgr.Installer) ([]Resolved, error) {
	var resolved []Resolved
	for _, entry := range entries {
		if !entry.Server.IsEnabled() {
			continue
		}
		if strings.TrimSpace(entry.Server.URL) != "" {
			return nil, fmt.Errorf("MCP server %q uses an HTTP url; Sigma Agent only supports stdio servers", entry.Name)
		}
		if strings.TrimSpace(entry.Server.Command) == "" {
			return nil, fmt.Errorf("MCP server %q needs a command", entry.Name)
		}
		workingDirectory := entry.Dir
		arguments := entry.Server.Args
		if packageName := strings.TrimSpace(entry.Server.Package); packageName != "" {
			packageRoot, err := locatePackage(packageName, installers...)
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", entry.Name, err)
			}
			workingDirectory = packageRoot
			arguments = resolvePackageArguments(arguments, packageRoot)
		} else if entry.Server.Cwd != "" {
			resolvedCwd, err := resolvePath(entry.Dir, entry.Server.Cwd)
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", entry.Name, err)
			}
			workingDirectory = resolvedCwd
		}
		resolved = append(resolved, Resolved{
			Name: entry.Name,
			Config: mcp.Config{
				Name:    entry.Name,
				Command: entry.Server.Command,
				Args:    arguments,
				Env:     entry.Server.Env,
				Dir:     workingDirectory,
			},
			File: entry.File,
		})
	}
	return resolved, nil
}

func locatePackage(name string, installers ...*pkgmgr.Installer) (string, error) {
	source := strings.TrimSpace(name)
	if !strings.HasPrefix(source, "npm:") && !strings.HasPrefix(source, "git:") {
		source = "npm:" + source
	}
	parsed, err := pkgmgr.ParseSource(source)
	if err != nil {
		return "", err
	}
	if len(installers) == 0 {
		return "", fmt.Errorf("package %q is not available", parsed.Name)
	}
	for _, installer := range installers {
		if installer == nil {
			continue
		}
		root, err := installer.Locate(parsed)
		if err != nil {
			continue
		}
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			return root, nil
		}
	}
	return "", fmt.Errorf("package %q is not installed in any configured scope", parsed.Name)
}

// resolvePackageArguments rewrites relative arguments so they point into the
// package directory. An argument that is already absolute is kept.
func resolvePackageArguments(arguments []string, packageRoot string) []string {
	resolved := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "./") || strings.HasPrefix(argument, "../") {
			resolved = append(resolved, filepath.Join(packageRoot, filepath.FromSlash(argument)))
			continue
		}
		resolved = append(resolved, argument)
	}
	return resolved
}

func resolvePath(base string, candidate string) (string, error) {
	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate), nil
	}
	return filepath.Join(base, filepath.FromSlash(candidate)), nil
}
