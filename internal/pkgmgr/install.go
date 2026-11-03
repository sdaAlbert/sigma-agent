package pkgmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Package is an installed package Sigma Agent can load resources from.
type Package struct {
	// Source is the source the package was installed from.
	Source Source
	// Root is the directory that holds the package contents.
	Root string
	// Manifest is the "pi" field of the package's package.json.
	Manifest Manifest
	// Name is the package name declared in package.json.
	Name string
	// Version is the package version declared in package.json.
	Version string
}

// Installer installs packages into a packages root directory.
type Installer struct {
	// Root is the directory that holds installed packages. Npm packages
	// live under <Root>/npm and git packages under <Root>/git.
	Root string
	// NPMCommand is the package manager executable. Empty uses "npm".
	NPMCommand string
	// Log receives progress messages.
	Log func(string)
}

func (i *Installer) npmCommand() string {
	if strings.TrimSpace(i.NPMCommand) != "" {
		return i.NPMCommand
	}
	return "npm"
}

func (i *Installer) log(message string) {
	if i.Log != nil {
		i.Log(message)
	}
}

// Install installs a package and returns its location. Local packages are
// used in place and are never copied.
func (i *Installer) Install(ctx context.Context, source Source) (*Package, error) {
	switch source.Type {
	case SourceLocal:
		return i.describeLocal(source)
	case SourceNPM:
		return i.installNpm(ctx, source)
	case SourceGit:
		return i.installGit(ctx, source)
	}
	return nil, fmt.Errorf("unsupported package source type %q", source.Type)
}

func (i *Installer) describeLocal(source Source) (*Package, error) {
	info, err := os.Stat(source.Path)
	if err != nil {
		return nil, fmt.Errorf("open local package %s: %w", source.Path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local package %s is not a directory", source.Path)
	}
	return i.describe(source, source.Path)
}

func (i *Installer) installNpm(ctx context.Context, source Source) (*Package, error) {
	installRoot := filepath.Join(i.Root, "npm")
	if err := ensureNpmProject(installRoot); err != nil {
		return nil, err
	}
	arguments := []string{"install", source.Spec, "--prefix", installRoot, "--legacy-peer-deps", "--no-audit", "--no-fund"}
	i.log(fmt.Sprintf("installing %s with %s", source.Spec, i.npmCommand()))
	if output, err := runCommand(ctx, "", i.npmCommand(), arguments...); err != nil {
		return nil, fmt.Errorf("install %s: %w%s", source.Spec, err, formatOutput(output))
	}
	name, _ := ParseNpmSpec(source.Spec)
	packageRoot := filepath.Join(installRoot, "node_modules", filepath.FromSlash(name))
	if _, err := os.Stat(packageRoot); err != nil {
		return nil, fmt.Errorf("install %s: package directory %s was not created", source.Spec, packageRoot)
	}
	return i.describe(source, packageRoot)
}

func (i *Installer) installGit(ctx context.Context, source Source) (*Package, error) {
	target := filepath.Join(i.Root, "git", source.Name)
	if _, err := os.Stat(target); err == nil {
		i.log(fmt.Sprintf("updating existing git package %s", source.Name))
		if output, err := runCommand(ctx, target, "git", "fetch", "--all", "--tags"); err != nil {
			return nil, fmt.Errorf("fetch %s: %w%s", source.Spec, err, formatOutput(output))
		}
		if source.Ref != "" {
			if output, err := runCommand(ctx, target, "git", "checkout", source.Ref); err != nil {
				return nil, fmt.Errorf("checkout %s: %w%s", source.Ref, err, formatOutput(output))
			}
		}
	} else {
		i.log(fmt.Sprintf("cloning %s", source.Spec))
		if output, err := runCommand(ctx, "", "git", "clone", source.Spec, target); err != nil {
			return nil, fmt.Errorf("clone %s: %w%s", source.Spec, err, formatOutput(output))
		}
		if source.Ref != "" {
			if output, err := runCommand(ctx, target, "git", "checkout", source.Ref); err != nil {
				return nil, fmt.Errorf("checkout %s: %w%s", source.Ref, err, formatOutput(output))
			}
		}
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err == nil {
		i.log(fmt.Sprintf("installing dependencies for %s", source.Name))
		if output, err := runCommand(ctx, target, i.npmCommand(), "install", "--legacy-peer-deps", "--no-audit", "--no-fund"); err != nil {
			return nil, fmt.Errorf("install dependencies for %s: %w%s", source.Name, err, formatOutput(output))
		}
	}
	return i.describe(source, target)
}

func (i *Installer) describe(source Source, packageRoot string) (*Package, error) {
	manifest, err := ReadManifest(packageRoot)
	if err != nil {
		return nil, fmt.Errorf("read manifest for %s: %w", source.String(), err)
	}
	name, version, err := ReadPackageInfo(packageRoot)
	if err != nil {
		return nil, fmt.Errorf("read package info for %s: %w", source.String(), err)
	}
	if name == "" {
		name = source.Name
	}
	return &Package{Source: source, Root: packageRoot, Manifest: manifest, Name: name, Version: version}, nil
}

// Locate returns the installed directory of a package without installing it.
func (i *Installer) Locate(source Source) (string, error) {
	switch source.Type {
	case SourceLocal:
		return source.Path, nil
	case SourceNPM:
		return filepath.Join(i.Root, "npm", "node_modules", filepath.FromSlash(source.Name)), nil
	case SourceGit:
		return filepath.Join(i.Root, "git", source.Name), nil
	}
	return "", fmt.Errorf("unsupported package source type %q", source.Type)
}

// List returns every installed package found under the packages root.
func (i *Installer) List() ([]*Package, error) {
	var packages []*Package
	npmRoot := filepath.Join(i.Root, "npm", "node_modules")
	entries, err := os.ReadDir(npmRoot)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if strings.HasPrefix(name, "@") {
				scoped, scopedErr := os.ReadDir(filepath.Join(npmRoot, name))
				if scopedErr != nil {
					continue
				}
				for _, inner := range scoped {
					if !inner.IsDir() || strings.HasPrefix(inner.Name(), ".") {
						continue
					}
					packages = append(packages, i.describePath(filepath.Join(npmRoot, name, inner.Name()), name+"/"+inner.Name()))
				}
				continue
			}
			packages = append(packages, i.describePath(filepath.Join(npmRoot, name), name))
		}
	}
	gitRoot := filepath.Join(i.Root, "git")
	gitEntries, err := os.ReadDir(gitRoot)
	if err == nil {
		for _, entry := range gitEntries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			packages = append(packages, i.describePath(filepath.Join(gitRoot, entry.Name()), entry.Name()))
		}
	}
	return packages, nil
}

func (i *Installer) describePath(packageRoot string, fallbackName string) *Package {
	manifest, _ := ReadManifest(packageRoot)
	name, version, _ := ReadPackageInfo(packageRoot)
	if name == "" {
		name = fallbackName
	}
	return &Package{Source: Source{Type: SourceLocal, Path: packageRoot, Name: name}, Root: packageRoot, Manifest: manifest, Name: name, Version: version}
}

func ensureNpmProject(installRoot string) error {
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		return fmt.Errorf("create npm install root %s: %w", installRoot, err)
	}
	manifestPath := filepath.Join(installRoot, "package.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content, err := json.Marshal(map[string]any{
		"name":        "sigma-agent-packages",
		"private":     true,
		"description": "Packages installed for Sigma Agent.",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, content, 0o644)
}

func runCommand(ctx context.Context, dir string, name string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	if dir != "" {
		command.Dir = dir
	}
	output, err := command.CombinedOutput()
	return string(output), err
}

func formatOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 4096 {
		trimmed = trimmed[len(trimmed)-4096:]
	}
	return ": " + trimmed
}
