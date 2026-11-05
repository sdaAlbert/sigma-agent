package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdaAlbert/sigma-agent/internal/mcpconfig"
	"github.com/sdaAlbert/sigma-agent/internal/pkgmgr"
)

func TestInstalledPackagesPreferProjectScope(t *testing.T) {
	root := t.TempDir()
	user := packageScope{Name: "user", Dir: filepath.Join(root, "user", ".sigma-agent")}
	project := packageScope{Name: "project", Dir: filepath.Join(root, "project", ".sigma-agent")}
	for _, scope := range []packageScope{user, project} {
		packageDir := filepath.Join(pkgmgr.PackagesRoot(scope.Dir), "npm", "node_modules", "shared")
		if err := os.MkdirAll(packageDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"shared","version":"1.0.0"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		registry := pkgmgr.Registry{Path: registryPath(scope)}
		if _, err := registry.Add(pkgmgr.Entry{Source: "npm:shared"}); err != nil {
			t.Fatal(err)
		}
	}

	packages, err := installedPackages([]packageScope{user, project})
	if err != nil {
		t.Fatalf("installedPackages() = %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("installedPackages() returned %d packages, want both scopes", len(packages))
	}
	wantProjectRoot := filepath.Join(pkgmgr.PackagesRoot(project.Dir), "npm", "node_modules", "shared")
	if packages[0].Root != wantProjectRoot {
		t.Fatalf("first package root = %q, want project package %q", packages[0].Root, wantProjectRoot)
	}
}

func TestResolveMCPEntriesKeepsValidServersWhenOneEntryIsInvalid(t *testing.T) {
	entries := []mcpconfig.Entry{
		{Name: "remote", Server: mcpconfig.Server{URL: "https://example.invalid/mcp"}},
		{Name: "local", Server: mcpconfig.Server{Command: "node", Args: []string{"server.js"}}},
	}
	resolved, failures := resolveMCPEntries(entries, nil)
	if len(failures) != 1 {
		t.Fatalf("resolveMCPEntries() failures = %d, want one", len(failures))
	}
	if len(resolved) != 1 || resolved[0].Name != "local" {
		t.Fatalf("resolveMCPEntries() = %+v, want the valid local server", resolved)
	}
}

func TestConfiguredMCPServersCanBeDisabledWithoutLoadingUserConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, mcpconfig.FileName), []byte("not valid JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools, shutdown, err := startConfiguredMCPServers(context.Background(), []packageScope{{Name: "user", Dir: dir}}, true)
	if err != nil {
		t.Fatalf("startConfiguredMCPServers(disabled) error = %v", err)
	}
	defer shutdown()
	if len(tools) != 0 {
		t.Fatalf("disabled MCP tools = %d, want none", len(tools))
	}
}

func TestPackageInstallersPreferProjectAndFallBackToUser(t *testing.T) {
	user := packageScope{Name: "user", Dir: filepath.Join(t.TempDir(), "user")}
	project := packageScope{Name: "project", Dir: filepath.Join(t.TempDir(), "project")}
	installers := packageInstallers([]packageScope{user, project})
	if len(installers) != 2 || installers[0].Root != pkgmgr.PackagesRoot(project.Dir) || installers[1].Root != pkgmgr.PackagesRoot(user.Dir) {
		t.Fatalf("packageInstallers() roots = %+v, want project then user", installers)
	}
}
