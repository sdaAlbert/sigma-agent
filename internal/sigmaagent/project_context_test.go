package sigmaagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

func TestProjectGuidanceDisclosesSkillMetadataAndReadsBodyOnDemand(t *testing.T) {
	useIsolatedUserSkillDirectory(t)
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agents", "skills", "go-issue-workflow")
	writeFixture(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: go-issue-workflow\ndescription: Use when fixing a Go issue.\n---\nInspect the failing path, add a regression test, and run focused tests.\n")
	writeFixture(t, filepath.Join(skillDir, "scripts", "helper.ps1"), "Write-Output 'must not be exposed'\n")

	guidance, err := LoadProjectGuidance(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(guidance.Skills) != 1 || guidance.Skills[0].Name != "go-issue-workflow" || guidance.Skills[0].Description != "Use when fixing a Go issue." {
		t.Fatalf("unexpected skill metadata: %#v", guidance.Skills)
	}
	if strings.Contains(guidance.SystemPrompt("base"), "Inspect the failing path") {
		t.Fatal("skill body was disclosed before the skill was selected")
	}
	if !strings.Contains(guidance.SystemPrompt("base"), "go-issue-workflow") || !strings.Contains(guidance.SystemPrompt("base"), "Use when fixing a Go issue.") {
		t.Fatal("system prompt does not expose the skill name and description")
	}
	if !strings.Contains(guidance.SystemPrompt("base"), "ordered execution path") || !strings.Contains(guidance.SystemPrompt("base"), "route/method matching") {
		t.Fatal("system prompt does not require tracing dispatch and short-circuit boundaries")
	}

	tool := guidance.SkillTool()
	got, err := tool.Handle(context.Background(), json.RawMessage(`{"name":"go-issue-workflow"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Inspect the failing path") || strings.Contains(got, "must not be exposed") {
		t.Fatalf("unexpected on-demand skill content: %s", got)
	}
	if _, err := tool.Handle(context.Background(), json.RawMessage(`{"name":"../scripts/helper"}`)); err == nil {
		t.Fatal("unknown skill name should be rejected")
	}
}

func TestMatchingSkillNamesUsesRepositoryLanguageAndTaskName(t *testing.T) {
	useIsolatedUserSkillDirectory(t)
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "setup.py"), "")
	writeFixture(t, filepath.Join(root, ".agents", "skills", "python-issue-workflow", "SKILL.md"), "---\nname: python-issue-workflow\ndescription: Use when fixing an issue in a Python repository.\n---\nworkflow\n")
	writeFixture(t, filepath.Join(root, ".agents", "skills", "go-issue-workflow", "SKILL.md"), "---\nname: go-issue-workflow\ndescription: Use when fixing an issue in a Go repository.\n---\nworkflow\n")
	writeFixture(t, filepath.Join(root, ".agents", "skills", "xlsx", "SKILL.md"), "---\nname: xlsx\ndescription: Use when working with a spreadsheet or a standalone Python script.\n---\nworkflow\n")
	writeFixture(t, filepath.Join(root, ".agents", "skills", "manual-only", "SKILL.md"), "---\nname: manual-only\ndescription: Manual only.\n---\nworkflow\n")

	guidance, err := LoadProjectGuidance(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range guidance.Skills {
		if guidance.Skills[i].Name == "manual-only" {
			guidance.Skills[i].DisableModelInvocation = true
		}
	}
	got := guidance.MatchingSkillNames("Fix the issue")
	if len(got) != 1 || got[0] != "python-issue-workflow" {
		t.Fatalf("matching Skills = %v, want [python-issue-workflow]", got)
	}
	got = guidance.MatchingSkillNames("Please use go-issue-workflow")
	if len(got) != 2 || got[0] != "go-issue-workflow" || got[1] != "python-issue-workflow" {
		t.Fatalf("explicit task Skill match = %v, want sorted repository and explicitly named Skills", got)
	}
}

func TestProjectGuidanceLoadsRepositoryMemoryAsReadOnlyContext(t *testing.T) {
	useIsolatedUserSkillDirectory(t)
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".sigma-agent", "MEMORY.md"), "# Reviewed project memory\n\n- 2026-10-30 | source: docs/testing.md | Run focused Go package tests before the full suite.\n")
	guidance, err := LoadProjectGuidance(root)
	if err != nil {
		t.Fatal(err)
	}
	prompt := guidance.SystemPrompt("base")
	if strings.Contains(prompt, "Run focused Go package tests before the full suite") || strings.Contains(prompt, "source: docs/testing.md") {
		t.Fatalf("repository memory contents should be retrieved on demand, not injected into the prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "search_memory") {
		t.Fatalf("repository memory retrieval instructions missing from prompt: %s", prompt)
	}
	if _, err := guidance.SkillTool().Handle(context.Background(), json.RawMessage(`{"name":"missing"}`)); err == nil {
		t.Fatal("empty skill registry should reject lookup")
	}

	tools, err := BuiltinTools(root, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	findTool(t, tools, "search_memory")
	for _, name := range []string{"edit_file", "apply_patch"} {
		tool := findTool(t, tools, name)
		input := `{"path":".sigma-agent/MEMORY.md","old_text":"x","new_text":"y"}`
		if name == "apply_patch" {
			input = `{"path":".sigma-agent/MEMORY.md","content":"changed"}`
		}
		if _, err := tool.Handle(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("%s should not modify project memory", name)
		}
	}
}

func TestProjectGuidanceDoesNotInheritParentRepositoryContext(t *testing.T) {
	useIsolatedUserSkillDirectory(t)
	parent := t.TempDir()
	writeFixture(t, filepath.Join(parent, ".sigma-agent", "MEMORY.md"), "parent-only fact\n")
	writeFixture(t, filepath.Join(parent, ".agents", "skills", "parent-skill", "SKILL.md"), "---\nname: parent-skill\ndescription: Parent repository only.\n---\nparent skill body\n")
	child := filepath.Join(parent, "nested", "repo")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	guidance, err := LoadProjectGuidance(child)
	if err != nil {
		t.Fatal(err)
	}
	if guidance.Memory != "" || len(guidance.Skills) != 0 {
		t.Fatalf("child repo inherited parent context: memory=%q skills=%#v", guidance.Memory, guidance.Skills)
	}
}

func useIsolatedUserSkillDirectory(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestLoadMemoryEntriesPreservesSourceDateAndContent(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".sigma-agent", "MEMORY.md"), "# Project memory\n\n- 2026-10-30 | source: `docs/testing.md` | Run focused Go tests before the full suite.\n")
	entries, err := LoadMemoryEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d memory entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Source != "`docs/testing.md`" || entry.UpdatedAt != "2026-10-30" || entry.Text != "Run focused Go tests before the full suite." || entry.Line != 3 {
		t.Fatalf("unexpected parsed entry: %#v", entry)
	}
	if entry.ID == "" || entry.ContentHash == "" {
		t.Fatalf("entry must have stable ID and content hash: %#v", entry)
	}
}

func TestLoadMemoryEntriesRejectsUnattributedFacts(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".sigma-agent", "MEMORY.md"), "# Project memory\n\n- An unattributed fact that must not enter the vector index.\n")
	if _, err := LoadMemoryEntries(root); err == nil {
		t.Fatal("unattributed memory entry should be rejected")
	}
}

func TestMemorySearchPromptDoesNotInjectWholeMemory(t *testing.T) {
	guidance := ProjectGuidance{Memory: "sensitive architecture detail", Skills: nil}
	prompt := guidance.SystemPromptWithMemorySearch("base")
	if strings.Contains(prompt, "sensitive architecture detail") || !strings.Contains(prompt, "search_memory") {
		t.Fatalf("memory search prompt should direct retrieval without injecting every memory: %s", prompt)
	}
}

func TestWritingToolsRejectProjectGuidance(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".agents", "skills", "go-issue-workflow", "SKILL.md"), "---\nname: go-issue-workflow\ndescription: Go issues.\n---\n")
	writeFixture(t, filepath.Join(root, "main.go"), "package main\n")
	data, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	tools, err := BuiltinTools(root, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".agents/skills/go-issue-workflow/SKILL.md", ".sigma-agent/MEMORY.md"} {
		for _, name := range []string{"edit_file", "apply_patch"} {
			tool := findTool(t, tools, name)
			args := map[string]any{"path": path, "old_text": "package", "new_text": "changed", "content": "changed"}
			raw, _ := json.Marshal(args)
			if _, err := tool.Handle(context.Background(), raw); err == nil {
				t.Fatalf("%s unexpectedly accepted write to %s", name, path)
			}
		}
	}
	updated, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil || string(updated) != string(data) {
		t.Fatalf("protected write changed an unrelated file: %q, %v", updated, err)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findTool(t *testing.T, tools []agent.Tool, name string) agent.Tool {
	t.Helper()
	for _, candidate := range tools {
		if candidate.Definition.Name == name {
			return candidate
		}
	}
	t.Fatalf("tool %q not found", name)
	return agent.Tool{}
}
