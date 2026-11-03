package mcpconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sdaAlbert/sigma-agent/internal/pkgmgr"
)

func TestLoadMergesFilesInPrecedenceOrder(t *testing.T) {
	directory := t.TempDir()
	user := filepath.Join(directory, "user", FileName)
	project := filepath.Join(directory, "project", FileName)
	write(t, user, `{"mcpServers":{"web":{"command":"node","args":["a.js"]},"docs":{"command":"docs-server"}}}`)
	write(t, project, `{"mcpServers":{"web":{"command":"node","args":["b.js"]}}}`)

	entries, err := Load([]string{user, project})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Load() = %+v, want two entries", entries)
	}
	web := entryByName(t, entries, "web")
	if len(web.Server.Args) != 1 || web.Server.Args[0] != "b.js" {
		t.Fatalf("web.Args = %v, want the project value", web.Server.Args)
	}
	if web.File != project {
		t.Fatalf("web.File = %s, want %s", web.File, project)
	}
	docs := entryByName(t, entries, "docs")
	if docs.File != user {
		t.Fatalf("docs.File = %s, want %s", docs.File, user)
	}
}

func TestLoadSkipsMissingFiles(t *testing.T) {
	entries, err := Load([]string{filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Load() = %+v, want no entries", entries)
	}
}

func TestResolveSkipsDisabledServers(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, FileName)
	write(t, path, `{"mcpServers":{"web":{"command":"node","enabled":false}}}`)
	entries, err := Load([]string{path})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	resolved, err := Resolve(entries, nil)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("Resolve() = %+v, want no servers", resolved)
	}
}

func TestResolveRejectsHTTPServers(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, FileName)
	write(t, path, `{"mcpServers":{"remote":{"url":"https://example.com/mcp"}}}`)
	entries, err := Load([]string{path})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if _, err := Resolve(entries, nil); err == nil {
		t.Fatal("Resolve() accepted an HTTP server, want an error")
	}
}

func TestResolveStartsAServerFromAnInstalledPackage(t *testing.T) {
	scope := t.TempDir()
	packageRoot := filepath.Join(scope, "packages", "npm", "node_modules", "demo")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(packageRoot, "package.json"), `{"name":"demo","version":"1.0.0"}`)

	path := filepath.Join(scope, FileName)
	write(t, path, `{"mcpServers":{"web":{"command":"node","args":["./dist/server.js"],"package":"demo"}}}`)
	entries, err := Load([]string{path})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	installer := &pkgmgr.Installer{Root: filepath.Join(scope, "packages")}
	resolved, err := Resolve(entries, installer)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("Resolve() = %+v, want one server", resolved)
	}
	server := resolved[0]
	if server.Config.Dir != packageRoot {
		t.Fatalf("Config.Dir = %s, want %s", server.Config.Dir, packageRoot)
	}
	want := filepath.Join(packageRoot, "dist", "server.js")
	if len(server.Config.Args) != 1 || server.Config.Args[0] != want {
		t.Fatalf("Config.Args = %v, want %v", server.Config.Args, want)
	}
}

func TestResolveFindsUserPackageWhenProjectScopeIsAlsoConfigured(t *testing.T) {
	temp := t.TempDir()
	userRoot := filepath.Join(temp, "user", "packages")
	projectRoot := filepath.Join(temp, "project", "packages")
	userPackage := filepath.Join(userRoot, "npm", "node_modules", "web-package")
	if err := os.MkdirAll(userPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := Entry{
		Name:   "web",
		Server: Server{Command: "node", Args: []string{"./server.js"}, Package: "web-package"},
		File:   filepath.Join(temp, "user", FileName),
		Dir:    filepath.Join(temp, "user"),
	}
	resolved, err := Resolve([]Entry{entry},
		&pkgmgr.Installer{Root: projectRoot},
		&pkgmgr.Installer{Root: userRoot},
	)
	if err != nil {
		t.Fatalf("Resolve() = %v, want user-scope package fallback", err)
	}
	if len(resolved) != 1 || resolved[0].Config.Dir != userPackage {
		t.Fatalf("Resolve() = %+v, want package directory %s", resolved, userPackage)
	}
}

func TestResolvePrefersProjectPackageOverUserPackage(t *testing.T) {
	temp := t.TempDir()
	userRoot := filepath.Join(temp, "user", "packages")
	projectRoot := filepath.Join(temp, "project", "packages")
	userPackage := filepath.Join(userRoot, "npm", "node_modules", "web-package")
	projectPackage := filepath.Join(projectRoot, "npm", "node_modules", "web-package")
	for _, path := range []string{userPackage, projectPackage} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entry := Entry{Name: "web", Server: Server{Command: "node", Package: "web-package"}}
	resolved, err := Resolve([]Entry{entry},
		&pkgmgr.Installer{Root: projectRoot},
		&pkgmgr.Installer{Root: userRoot},
	)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	if len(resolved) != 1 || resolved[0].Config.Dir != projectPackage {
		t.Fatalf("Resolve() = %+v, want project package directory %s", resolved, projectPackage)
	}
}

func TestResolveReportsAMissingPackage(t *testing.T) {
	scope := t.TempDir()
	path := filepath.Join(scope, FileName)
	write(t, path, `{"mcpServers":{"web":{"command":"node","args":["./x.js"],"package":"missing"}}}`)
	entries, err := Load([]string{path})
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	installer := &pkgmgr.Installer{Root: filepath.Join(scope, "packages")}
	if _, err := Resolve(entries, installer); err == nil {
		t.Fatal("Resolve() accepted a missing package, want an error")
	}
}

func entryByName(t *testing.T, entries []Entry, name string) Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("entry %q not found in %+v", name, entries)
	return Entry{}
}

func write(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
