package pkgmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceRecognizesEveryKind(t *testing.T) {
	npm, err := ParseSource("npm:pi-web-access")
	if err != nil {
		t.Fatalf("ParseSource(npm) = %v", err)
	}
	if npm.Type != SourceNPM || npm.Name != "pi-web-access" || npm.Spec != "pi-web-access" {
		t.Fatalf("ParseSource(npm) = %+v", npm)
	}
	scoped, err := ParseSource("npm:@scope/pkg@1.2.3")
	if err != nil {
		t.Fatalf("ParseSource(scoped) = %v", err)
	}
	if scoped.Name != "@scope/pkg" {
		t.Fatalf("ParseSource(scoped).Name = %q, want @scope/pkg", scoped.Name)
	}
	git, err := ParseSource("git:github.com/owner/repo@v1.2.3")
	if err != nil {
		t.Fatalf("ParseSource(git) = %v", err)
	}
	if git.Type != SourceGit || git.Name != "repo" || git.Ref != "v1.2.3" || git.Spec != "github.com/owner/repo" {
		t.Fatalf("ParseSource(git) = %+v", git)
	}
	local, err := ParseSource("./somewhere")
	if err != nil {
		t.Fatalf("ParseSource(local) = %v", err)
	}
	if local.Type != SourceLocal || !filepath.IsAbs(local.Path) {
		t.Fatalf("ParseSource(local) = %+v", local)
	}
	if _, err := ParseSource("  "); err == nil {
		t.Fatal("ParseSource(empty) succeeded, want an error")
	}
}

func TestParseNpmSpecKeepsScopedNames(t *testing.T) {
	cases := map[string][2]string{
		"pi-web-access":        {"pi-web-access", ""},
		"pi-web-access@0.46.0": {"pi-web-access", "0.46.0"},
		"@scope/pkg":           {"@scope/pkg", ""},
		"@scope/pkg@2.0.0":     {"@scope/pkg", "2.0.0"},
	}
	for spec, want := range cases {
		name, version := ParseNpmSpec(spec)
		if name != want[0] || version != want[1] {
			t.Errorf("ParseNpmSpec(%q) = %q, %q; want %q, %q", spec, name, version, want[0], want[1])
		}
	}
}

func TestReadManifestReadsThePiField(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"demo","version":"1.0.0","pi":{"extensions":["./dist"],"skills":["./skills"]}}`)
	manifest, err := ReadManifest(root)
	if err != nil {
		t.Fatalf("ReadManifest() = %v", err)
	}
	if len(manifest.Extensions) != 1 || manifest.Extensions[0] != "./dist" {
		t.Fatalf("manifest.Extensions = %v", manifest)
	}
	if len(manifest.Skills) != 1 || manifest.Skills[0] != "./skills" {
		t.Fatalf("manifest.Skills = %v", manifest)
	}
	if !manifest.HasResources() {
		t.Fatal("manifest.HasResources() = false, want true")
	}
	name, version, err := ReadPackageInfo(root)
	if err != nil || name != "demo" || version != "1.0.0" {
		t.Fatalf("ReadPackageInfo() = %q, %q, %v", name, version, err)
	}
}

func TestReadManifestIgnoresUnusablePiFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"demo","pi":{"extensions":"not-a-list"}}`)
	manifest, err := ReadManifest(root)
	if err != nil {
		t.Fatalf("ReadManifest() = %v", err)
	}
	if manifest.HasResources() {
		t.Fatalf("manifest = %+v, want no resources", manifest)
	}
}

func TestResourcePathsUsesTheConventionalDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"demo"}`)
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nbody\n")
	writeFile(t, filepath.Join(root, "skills", "beta", "SKILL.md"), "---\nname: beta\ndescription: b\n---\nbody\n")
	writeFile(t, filepath.Join(root, "skills", "alpha", "notes.md"), "notes\n")
	installed := &Package{Root: root}
	paths, err := installed.SkillFiles()
	if err != nil {
		t.Fatalf("SkillFiles() = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("SkillFiles() = %v, want the two SKILL.md files", paths)
	}
	for _, path := range paths {
		if filepath.Base(path) != "SKILL.md" {
			t.Fatalf("SkillFiles() returned %s, want only SKILL.md files", path)
		}
	}
}

func TestResourcePathsHonorsTheManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"demo","pi":{"skills":["./docs/skills"]}}`)
	writeFile(t, filepath.Join(root, "docs", "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nbody\n")
	writeFile(t, filepath.Join(root, "skills", "beta", "SKILL.md"), "---\nname: beta\ndescription: b\n---\nbody\n")
	installed := &Package{Root: root, Manifest: Manifest{Skills: []string{"./docs/skills"}}}
	paths, err := installed.SkillFiles()
	if err != nil {
		t.Fatalf("SkillFiles() = %v", err)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], filepath.Join("docs", "skills")) {
		t.Fatalf("SkillFiles() = %v, want only the manifest skills", paths)
	}
}

func TestResourcePathsSupportsExclusions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"demo","pi":{"skills":["./skills","!./skills/beta"]}}`)
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nbody\n")
	writeFile(t, filepath.Join(root, "skills", "beta", "SKILL.md"), "---\nname: beta\ndescription: b\n---\nbody\n")
	installed := &Package{Root: root, Manifest: Manifest{Skills: []string{"./skills", "!./skills/beta"}}}
	paths, err := installed.SkillFiles()
	if err != nil {
		t.Fatalf("SkillFiles() = %v", err)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "alpha") {
		t.Fatalf("SkillFiles() = %v, want only the alpha skill", paths)
	}
}

func TestMatchGlobSupportsWildcards(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"skills/*/SKILL.md", "skills/alpha/SKILL.md", true},
		{"skills/*/SKILL.md", "skills/alpha/beta/SKILL.md", false},
		{"skills/**/SKILL.md", "skills/alpha/beta/SKILL.md", true},
		{"skills/**", "skills/alpha/SKILL.md", true},
		{"*.md", "notes.md", true},
		{"*.md", "notes.txt", false},
		{"skills/alpha", "skills/alpha", true},
		{"skills/alpha", "skills/beta", false},
	}
	for _, test := range cases {
		if got := matchGlob(test.pattern, test.path); got != test.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", test.pattern, test.path, got, test.want)
		}
	}
}

func TestRegistryRecordsAndRemovesPackages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.json")
	registry := Registry{Path: path}
	entries, err := registry.Add(Entry{Source: "npm:pi-web-access", Version: "0.46.0"})
	if err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Add() = %v, want one entry", entries)
	}
	if _, err := registry.Add(Entry{Source: "npm:pi-web-access", Version: "0.47.0"}); err != nil {
		t.Fatalf("Add() again = %v", err)
	}
	entries, err = LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry() = %v", err)
	}
	if len(entries) != 1 || entries[0].Version != "0.47.0" {
		t.Fatalf("LoadRegistry() = %v, want the updated entry", entries)
	}
	if _, err := registry.Remove("npm:pi-web-access@0.47.0"); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	entries, err = LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry() = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("LoadRegistry() = %v, want no entries", entries)
	}
}

func TestInstallerInstallsALocalPackageInPlace(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "local-package")
	writeFile(t, filepath.Join(source, "package.json"), `{"name":"local-package","version":"0.1.0","pi":{"skills":["./skills"]}}`)
	writeFile(t, filepath.Join(source, "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: a\n---\nbody\n")

	installer := &Installer{Root: filepath.Join(root, "packages")}
	parsed, err := ParseSource(source)
	if err != nil {
		t.Fatalf("ParseSource() = %v", err)
	}
	installed, err := installer.Install(t.Context(), parsed)
	if err != nil {
		t.Fatalf("Install() = %v", err)
	}
	if installed.Root != filepath.Clean(source) {
		t.Fatalf("Install().Root = %s, want %s", installed.Root, source)
	}
	if installed.Name != "local-package" || installed.Version != "0.1.0" {
		t.Fatalf("Install() = %+v, want the package name and version", installed)
	}
	skills, err := installed.SkillFiles()
	if err != nil || len(skills) != 1 {
		t.Fatalf("SkillFiles() = %v, %v, want one skill", skills, err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
