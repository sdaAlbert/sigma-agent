package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/mcp"
	"github.com/sdaAlbert/sigma-agent/internal/mcpconfig"
	"github.com/sdaAlbert/sigma-agent/internal/pkgmgr"
	workspaceagent "github.com/sdaAlbert/sigma-agent/internal/workspace"
)

// packageScope describes where the packages of one scope live.
type packageScope struct {
	// Name is the scope name, used in diagnostics.
	Name string
	// Dir is the scope directory.
	Dir string
}

// userScope returns the user-scoped Sigma Agent directory.
func userScope() (packageScope, error) {
	dir, err := pkgmgr.UserDir()
	if err != nil {
		return packageScope{}, err
	}
	return packageScope{Name: "user", Dir: dir}, nil
}

// projectScope returns the project-scoped Sigma Agent directory of a
// repository.
func projectScope(repo string) (packageScope, error) {
	if strings.TrimSpace(repo) == "" {
		return packageScope{}, fmt.Errorf("a repository path is required for project-scoped packages")
	}
	absolute, err := filepath.Abs(repo)
	if err != nil {
		return packageScope{}, err
	}
	return packageScope{Name: "project", Dir: filepath.Join(absolute, ".sigma-agent")}, nil
}

// packageScopes returns the scopes a package can be installed into, in
// lookup order. The project scope comes first so a project package wins over
// a user package with the same name.
func packageScopes(repo string) ([]packageScope, error) {
	user, err := userScope()
	if err != nil {
		return nil, err
	}
	scopes := []packageScope{user}
	if strings.TrimSpace(repo) != "" {
		project, err := projectScope(repo)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, project)
	}
	return scopes, nil
}

// registryPath returns the package registry file of one scope.
func registryPath(scope packageScope) string {
	return filepath.Join(scope.Dir, pkgmgr.RegistryFileName)
}

// installPackage installs a package into one scope and records it in the
// scope registry.
func installPackage(ctx context.Context, scope packageScope, source string) (*pkgmgr.Package, error) {
	parsed, err := pkgmgr.ParseSource(source)
	if err != nil {
		return nil, err
	}
	installer := &pkgmgr.Installer{
		Root: pkgmgr.PackagesRoot(scope.Dir),
		Log:  func(message string) { fmt.Fprintln(os.Stderr, "sigma-agent:", message) },
	}
	installed, err := installer.Install(ctx, parsed)
	if err != nil {
		return nil, err
	}
	registry := pkgmgr.Registry{Path: registryPath(scope)}
	if _, err := registry.Add(pkgmgr.Entry{Source: parsed.String(), Version: installed.Version}); err != nil {
		return nil, err
	}
	return installed, nil
}

// removePackage drops a package from a scope registry. The installed files
// are left on disk so a package can be reinstalled without a network round
// trip.
func removePackage(scope packageScope, source string) error {
	registry := pkgmgr.Registry{Path: registryPath(scope)}
	entries, err := registry.Remove(source)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "sigma-agent: no packages remain in the %s scope\n", scope.Name)
		return nil
	}
	return nil
}

// listPackages prints the packages recorded in every scope.
func listPackages(scopes []packageScope) error {
	for _, scope := range scopes {
		entries, err := pkgmgr.LoadRegistry(registryPath(scope))
		if err != nil {
			return err
		}
		fmt.Printf("%s scope (%s):\n", scope.Name, registryPath(scope))
		if len(entries) == 0 {
			fmt.Println("  no packages installed")
			continue
		}
		for _, entry := range entries {
			version := entry.Version
			if version == "" {
				version = "unknown version"
			}
			fmt.Printf("  %s (%s)\n", entry.Source, version)
		}
	}
	return nil
}

// installedPackages returns every package recorded in the given scopes, with
// the package contents located on disk.
func installedPackages(scopes []packageScope) ([]*pkgmgr.Package, error) {
	var packages []*pkgmgr.Package
	seen := make(map[string]bool)
	// Scopes are supplied from lowest to highest precedence. Discover the
	// project package first so duplicate Skill names resolve to the project.
	for scopeIndex := len(scopes) - 1; scopeIndex >= 0; scopeIndex-- {
		scope := scopes[scopeIndex]
		entries, err := pkgmgr.LoadRegistry(registryPath(scope))
		if err != nil {
			return nil, err
		}
		installer := &pkgmgr.Installer{Root: pkgmgr.PackagesRoot(scope.Dir)}
		for _, entry := range entries {
			parsed, err := pkgmgr.ParseSource(entry.Source)
			if err != nil {
				continue
			}
			root, err := installer.Locate(parsed)
			if err != nil {
				continue
			}
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				continue
			}
			if seen[root] {
				continue
			}
			seen[root] = true
			manifest, err := pkgmgr.ReadManifest(root)
			if err != nil {
				return nil, err
			}
			name, version, err := pkgmgr.ReadPackageInfo(root)
			if err != nil {
				return nil, err
			}
			if name == "" {
				name = parsed.Name
			}
			packages = append(packages, &pkgmgr.Package{
				Source: parsed, Root: root, Manifest: manifest, Name: name, Version: version,
			})
		}
	}
	return packages, nil
}

// packageInstallers returns package roots from highest to lowest precedence.
func packageInstallers(scopes []packageScope) []*pkgmgr.Installer {
	installers := make([]*pkgmgr.Installer, 0, len(scopes))
	for index := len(scopes) - 1; index >= 0; index-- {
		installers = append(installers, &pkgmgr.Installer{Root: pkgmgr.PackagesRoot(scopes[index].Dir)})
	}
	return installers
}

// resolveMCPEntries resolves servers independently so one unsupported or
// incomplete entry does not suppress valid MCP servers from the same config.
func resolveMCPEntries(entries []mcpconfig.Entry, installers []*pkgmgr.Installer) ([]mcpconfig.Resolved, []error) {
	var resolved []mcpconfig.Resolved
	var failures []error
	for _, entry := range entries {
		entryResolved, err := mcpconfig.Resolve([]mcpconfig.Entry{entry}, installers...)
		if err != nil {
			failures = append(failures, fmt.Errorf("MCP server %q configuration is invalid: %w", entry.Name, err))
			continue
		}
		resolved = append(resolved, entryResolved...)
	}
	return resolved, failures
}

// packageSkills returns the Agent Skills contributed by the installed
// packages.
func packageSkills(scopes []packageScope) ([]workspaceagent.PackageSkills, error) {
	packages, err := installedPackages(scopes)
	if err != nil {
		return nil, err
	}
	var result []workspaceagent.PackageSkills
	for _, installed := range packages {
		skillFiles, err := installed.SkillFiles()
		if err != nil {
			return nil, err
		}
		if len(skillFiles) == 0 {
			continue
		}
		directories := make([]string, 0, len(skillFiles))
		for _, file := range skillFiles {
			directory := filepath.Dir(file)
			if !containsString(directories, directory) {
				directories = append(directories, directory)
			}
		}
		sort.Strings(directories)
		result = append(result, workspaceagent.PackageSkills{
			Root:   installed.Root,
			Dirs:   directories,
			Source: installed.Name,
		})
	}
	return result, nil
}

func packageExtensions(scopes []packageScope) ([]string, error) {
	packages, err := installedPackages(scopes)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, installed := range packages {
		found, err := installed.ResourcePaths(pkgmgr.ResourceExtensions)
		if err != nil {
			return nil, fmt.Errorf("discover extensions in package %q: %w", installed.Name, err)
		}
		for _, path := range found {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".js", ".mjs", ".cjs":
				paths = append(paths, path)
			}
		}
	}
	return paths, nil
}

func packageExtensionLabels(scopes []packageScope) []string {
	packages, err := installedPackages(scopes)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var labels []string
	for _, installed := range packages {
		paths, err := installed.ResourcePaths(pkgmgr.ResourceExtensions)
		if err != nil {
			continue
		}
		for _, path := range paths {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".js", ".mjs", ".cjs":
			default:
				continue
			}
			relative, err := filepath.Rel(installed.Root, path)
			if err != nil {
				continue
			}
			parts := strings.Split(filepath.ToSlash(relative), "/")
			labelPath := parts[0]
			if labelPath == "." {
				labelPath = filepath.Base(relative)
			}
			label := installed.Name + ":" + labelPath
			if !seen[label] {
				seen[label] = true
				labels = append(labels, label)
			}
		}
	}
	sort.Strings(labels)
	return labels
}

// mcpConfigPaths returns the MCP configuration files of the given scopes,
// from the lowest to the highest precedence.
func mcpConfigPaths(scopes []packageScope) []string {
	var paths []string
	for _, scope := range scopes {
		paths = append(paths, filepath.Join(scope.Dir, mcpconfig.FileName))
	}
	// The Pi Agent location is read as well so an existing mcp.json can be
	// reused unchanged.
	if len(scopes) > 0 {
		paths = append(paths, filepath.Join(scopes[len(scopes)-1].Dir, ".pi", mcpconfig.FileName))
	}
	return paths
}

// startMCPServers connects the configured MCP servers and returns their tools
// together with a function that shuts the servers down.
func startMCPServers(ctx context.Context, scopes []packageScope) ([]agent.Tool, func(), error) {
	noop := func() {}
	entries, err := mcpconfig.Load(mcpConfigPaths(scopes))
	if err != nil {
		return nil, noop, err
	}
	if len(entries) == 0 {
		return nil, noop, nil
	}
	installers := packageInstallers(scopes)
	resolved, failures := resolveMCPEntries(entries, installers)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "sigma-agent: warning:", failure)
	}
	var tools []agent.Tool
	var clients []*mcp.Client
	shutdown := func() {
		for _, client := range clients {
			if err := client.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent: warning: close MCP server:", err)
			}
		}
	}
	for _, server := range resolved {
		client, err := mcp.Start(ctx, server.Config)
		if err != nil {
			// A server that cannot start must not take the session down.
			// The failure is reported once and the other servers continue.
			fmt.Fprintf(os.Stderr, "sigma-agent: warning: MCP server %q did not start: %v\n", server.Name, err)
			continue
		}
		serverTools, err := agent.MCPTools(ctx, client, agent.MCPToolOptions{
			Server: server.Name,
			Describe: func(toolName, description string) string {
				return fmt.Sprintf("MCP tool %q from server %q: %s", toolName, server.Name, description)
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "sigma-agent: warning: MCP server %q did not list its tools: %v\n", server.Name, err)
			_ = client.Close()
			continue
		}
		clients = append(clients, client)
		tools = append(tools, serverTools...)
	}
	return tools, shutdown, nil
}

func startConfiguredMCPServers(ctx context.Context, scopes []packageScope, disabled bool) ([]agent.Tool, func(), error) {
	if disabled {
		return nil, func() {}, nil
	}
	return startMCPServers(ctx, scopes)
}

// runPackageCommand implements the package management flags.
func runPackageCommand(install string, uninstall string, list bool, listMCP bool, local bool, repo string) error {
	if local && strings.TrimSpace(repo) == "" {
		workspace, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve current folder for project packages: %w", err)
		}
		repo = workspace
	}
	scopes, err := packageScopes(repo)
	if err != nil {
		return err
	}
	if list {
		return listPackages(scopes)
	}
	if listMCP {
		return listMCPServers(context.Background(), scopes)
	}
	scope := scopes[0]
	if local {
		scope = scopes[len(scopes)-1]
	}
	if install != "" {
		installed, err := installPackage(context.Background(), scope, install)
		if err != nil {
			return err
		}
		fmt.Printf("Installed %s%s into the %s scope (%s).\n", installed.Name, versionSuffix(installed.Version), scope.Name, installed.Root)
		printPackageResources(installed)
		return nil
	}
	if err := removePackage(scope, uninstall); err != nil {
		return err
	}
	fmt.Printf("Removed %s from the %s scope.\n", uninstall, scope.Name)
	return nil
}

func versionSuffix(version string) string {
	if strings.TrimSpace(version) == "" {
		return ""
	}
	return "@" + version
}

// printPackageResources reports the resources an installed package contributes.
func printPackageResources(installed *pkgmgr.Package) {
	for _, resourceType := range pkgmgr.ResourceTypes {
		paths, err := installed.ResourcePaths(resourceType)
		if err != nil || len(paths) == 0 {
			continue
		}
		fmt.Printf("  %s: %d\n", resourceType, len(paths))
		for _, path := range paths {
			relative, relErr := filepath.Rel(installed.Root, path)
			if relErr != nil {
				relative = path
			}
			fmt.Printf("    %s\n", filepath.ToSlash(relative))
		}
	}
}

// listMCPServers connects to the configured MCP servers and prints the tools
// they contribute.
func listMCPServers(ctx context.Context, scopes []packageScope) error {
	entries, err := mcpconfig.Load(mcpConfigPaths(scopes))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No MCP servers are configured.")
		return nil
	}
	installers := packageInstallers(scopes)
	resolved, failures := resolveMCPEntries(entries, installers)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "sigma-agent: warning:", failure)
	}
	if len(resolved) == 0 {
		fmt.Println("No MCP servers are available to inspect.")
		return nil
	}
	for _, server := range resolved {
		client, err := mcp.Start(ctx, server.Config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sigma-agent: warning: MCP server %q did not start: %v\n", server.Name, err)
			continue
		}
		tools, err := client.Tools(ctx)
		if err != nil {
			_ = client.Close()
			fmt.Fprintf(os.Stderr, "sigma-agent: warning: MCP server %q did not list its tools: %v\n", server.Name, err)
			continue
		}
		fmt.Printf("%s (%s): %d tools\n", server.Name, server.File, len(tools))
		for _, tool := range tools {
			fmt.Printf("  %s\n", mcp.ToolName(server.Name, tool.Name))
			fmt.Printf("    %s\n", strings.TrimSpace(tool.Description))
		}
		if err := client.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent: warning: close MCP server:", err)
		}
	}
	return nil
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
