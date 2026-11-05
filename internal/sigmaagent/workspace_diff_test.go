package sigmaagent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceDiffExcludesAgentMemory(t *testing.T) {
	for _, memoryTracked := range []bool{false, true} {
		name := "untracked memory"
		if memoryTracked {
			name = "tracked memory"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			gitTestCommand(t, root, "init", "-q")
			gitTestCommand(t, root, "config", "user.email", "sigma-test@example.invalid")
			gitTestCommand(t, root, "config", "user.name", "Sigma Test")

			writeDiffFixture(t, root, "app.go", "package app\n")
			memoryPath := filepath.Join(root, ".sigma-agent", "MEMORY.md")
			if memoryTracked {
				if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
					t.Fatal(err)
				}
				writeDiffFixture(t, root, filepath.Join(".sigma-agent", "MEMORY.md"), "# reviewed memory\n")
			}
			gitTestCommand(t, root, "add", ".")
			gitTestCommand(t, root, "commit", "-qm", "base")

			writeDiffFixture(t, root, "app.go", "package app\n\n// changed\n")
			if memoryTracked {
				writeDiffFixture(t, root, filepath.Join(".sigma-agent", "MEMORY.md"), "# updated memory\n")
			} else {
				if err := os.MkdirAll(filepath.Dir(memoryPath), 0o755); err != nil {
					t.Fatal(err)
				}
				writeDiffFixture(t, root, filepath.Join(".sigma-agent", "MEMORY.md"), "# session memory\n")
			}
			writeDiffFixture(t, root, "new.go", "package app\n")

			patch, err := WorkspaceDiff(root, "HEAD")
			if err != nil {
				t.Fatalf("WorkspaceDiff() error = %v", err)
			}
			if strings.Contains(patch, ".sigma-agent/MEMORY.md") {
				t.Fatalf("WorkspaceDiff() included agent-managed memory in patch:\n%s", patch)
			}
			for _, want := range []string{"diff --git a/app.go b/app.go", "diff --git a/new.go b/new.go"} {
				if !strings.Contains(patch, want) {
					t.Errorf("WorkspaceDiff() missing %q:\n%s", want, patch)
				}
			}
		})
	}
}

func TestStageTaskChangesExcludesAgentMemory(t *testing.T) {
	root := t.TempDir()
	gitTestCommand(t, root, "init", "-q")
	gitTestCommand(t, root, "config", "user.email", "sigma-test@example.invalid")
	gitTestCommand(t, root, "config", "user.name", "Sigma Test")
	writeDiffFixture(t, root, "app.go", "package app\n")
	writeDiffFixture(t, root, filepath.Join(".sigma-agent", "MEMORY.md"), "# reviewed memory\n")
	gitTestCommand(t, root, "add", ".")
	gitTestCommand(t, root, "commit", "-qm", "base")

	writeDiffFixture(t, root, "app.go", "package app\n\n// changed\n")
	writeDiffFixture(t, root, filepath.Join(".sigma-agent", "MEMORY.md"), "# updated memory\n")
	if err := stageTaskChanges(context.Background(), root); err != nil {
		t.Fatalf("stageTaskChanges() error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff --cached --name-only: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "app.go" {
		t.Fatalf("staged paths = %q, want only app.go", got)
	}
}

func gitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeDiffFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
