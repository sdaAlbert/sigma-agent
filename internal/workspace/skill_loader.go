package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type skillLocation struct {
	path                   string
	boundary               string
	source                 string
	standaloneRootMarkdown bool
}

// PackageSkills describes the Skill directories contributed by an installed
// package. The package directory bounds the Skill resources so a Skill can
// only read files that ship with its own package.
type PackageSkills struct {
	// Root is the package directory that bounds the Skill resources.
	Root string
	// Dirs are the directories that hold SKILL.md files.
	Dirs []string
	// Source names the package in diagnostics.
	Source string
}

// discoverSkills follows the portable Pi/Agent Skills directory layouts.
// Project Skills take precedence over user Skills with the same name. Skills
// contributed by installed packages are searched last.
func discoverSkills(projectRoot string, packageSkills []PackageSkills) ([]SkillSummary, error) {
	locations := []skillLocation{
		{path: filepath.Join(projectRoot, ".agents", "skills"), boundary: projectRoot, source: "project .agents/skills"},
		{path: filepath.Join(projectRoot, ".pi", "skills"), boundary: projectRoot, source: "project .pi/skills", standaloneRootMarkdown: true},
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		locations = append(locations,
			skillLocation{path: filepath.Join(home, ".agents", "skills"), boundary: home, source: "user .agents/skills"},
			skillLocation{path: filepath.Join(home, ".pi", "agent", "skills"), boundary: home, source: "user .pi/agent/skills", standaloneRootMarkdown: true},
		)
	}
	for _, packageSkill := range packageSkills {
		for _, dir := range packageSkill.Dirs {
			locations = append(locations, skillLocation{path: dir, boundary: packageSkill.Root, source: "package " + packageSkill.Source})
		}
	}

	var skills []SkillSummary
	seen := make(map[string]bool)
	for _, location := range locations {
		if err := discoverSkillsAt(location, &skills, seen); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			return skills[i].source < skills[j].source
		}
		return skills[i].Name < skills[j].Name
	})
	return skills, nil
}

func discoverSkillsAt(location skillLocation, skills *[]SkillSummary, seen map[string]bool) error {
	boundary, err := filepath.Abs(location.boundary)
	if err != nil {
		return nil
	}
	boundary, err = filepath.EvalSymlinks(boundary)
	if err != nil {
		return nil
	}
	requestedRoot, err := filepath.Abs(location.path)
	if err != nil {
		return nil
	}
	root, err := projectPath(boundary, requestedRoot)
	if err != nil {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		isSkillMarkdown := strings.EqualFold(entry.Name(), "SKILL.md")
		isMarkdown := strings.EqualFold(filepath.Ext(entry.Name()), ".md")
		if !isSkillMarkdown && !isMarkdown {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		depth := strings.Count(relative, string(filepath.Separator))
		if !isSkillMarkdown && depth == 0 && !location.standaloneRootMarkdown {
			return nil
		}

		content, err := readProjectFile(root, path, maxSkillBytes)
		if err != nil {
			return nil
		}
		summary, _, err := parseSkill(content)
		if err != nil || summary.Name == "" || seen[summary.Name] {
			return nil
		}
		summary.path = path
		summary.dir = filepath.Dir(path)
		summary.source = location.source
		summary.standalone = !isSkillMarkdown && depth == 0
		seen[summary.Name] = true
		*skills = append(*skills, summary)
		return nil
	})
}

func (g ProjectGuidance) skillByName(name string) (SkillSummary, bool) {
	for _, skill := range g.Skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return SkillSummary{}, false
}

func readSkillResource(skill SkillSummary, requestedPath string) (string, error) {
	if skill.standalone {
		return "", fmt.Errorf("standalone Markdown Skills do not have an isolated resource directory; use a Skill folder containing SKILL.md for references")
	}
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" || strings.ContainsRune(requestedPath, '\x00') || strings.Contains(requestedPath, ":") {
		return "", fmt.Errorf("resource path must be a relative text file inside the Skill directory")
	}
	clean := filepath.Clean(filepath.FromSlash(requestedPath))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resource path must stay inside the Skill directory")
	}
	extension := strings.ToLower(filepath.Ext(clean))
	switch extension {
	case ".md", ".txt", ".json", ".jsonc", ".yaml", ".yml", ".toml", ".csv", ".tsv", ".xml", ".html", ".htm", ".svg", ".sql", ".ini", ".conf", ".properties":
	default:
		return "", fmt.Errorf("unsupported Skill resource type %q; scripts and binary assets are not loaded", extension)
	}
	path, err := projectPath(skill.dir, filepath.Join(skill.dir, clean))
	if err != nil {
		return "", err
	}
	content, err := readBounded(path, maxSkillResourceBytes)
	if err != nil {
		return "", err
	}
	if strings.IndexByte(string(content), 0) >= 0 {
		return "", fmt.Errorf("Skill resource is not a text file")
	}
	return string(content), nil
}
