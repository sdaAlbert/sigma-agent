package workspace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

type implementChildModel struct {
	calls     int
	toolNames []string
}

func (*implementChildModel) ModelID() string { return "implement-child-test" }

func (m *implementChildModel) Stream(_ context.Context, _ string, _ []agent.Message, tools []agent.ToolDefinition, _ func(agent.ModelEvent)) (agent.ModelResponse, error) {
	m.calls++
	if m.calls == 1 {
		for _, tool := range tools {
			m.toolNames = append(m.toolNames, tool.Name)
		}
		return agent.ModelResponse{Content: []agent.ContentBlock{{
			Type: "tool_use", ID: "create-file", Name: "create_file",
			Input: json.RawMessage(`{"path":"child.txt","content":"from child\n"}`),
		}}, StopReason: "tool_use", InputTokens: 11, OutputTokens: 3}, nil
	}
	return agent.ModelResponse{Content: []agent.ContentBlock{{Type: "text", Text: "created child.txt"}}, StopReason: "end_turn", InputTokens: 7, OutputTokens: 5}, nil
}

func TestImplementChildEditsOnlyItsWorktreeAndReportsDiffAndUsage(t *testing.T) {
	repository := t.TempDir()
	runGitForDelegateTest(t, repository, "init")
	runGitForDelegateTest(t, repository, "config", "user.email", "sigma-test@example.invalid")
	runGitForDelegateTest(t, repository, "config", "user.name", "Sigma test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForDelegateTest(t, repository, "add", "README.md")
	runGitForDelegateTest(t, repository, "commit", "-m", "base")

	manager := &investigationManager{tasks: make(map[string]*investigationTask)}
	model := &implementChildModel{}
	started, err := manager.start(context.Background(), model, nil, nil, "create one small file", "implement", repository)
	if err != nil {
		t.Fatal(err)
	}
	var initial investigationTask
	if err := json.Unmarshal([]byte(started), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Status != "running" || initial.Mode != "implement" || initial.Workspace == "" {
		t.Fatalf("initial child status = %#v", initial)
	}
	defer func() {
		_, _ = gitWorktree(repository, "worktree", "remove", "--force", initial.Workspace)
		_ = os.RemoveAll(initial.Workspace)
	}()

	finished, err := manager.wait(context.Background(), initial.ID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var result investigationTask
	if err := json.Unmarshal([]byte(finished), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Report != "created child.txt" {
		t.Fatalf("finished child status/report = %q/%q", result.Status, result.Report)
	}
	if result.Input != 18 || result.Output != 8 || !strings.Contains(result.Diff, "child.txt") {
		t.Fatalf("child diff/usage = %q/%d/%d", result.Diff, result.Input, result.Output)
	}
	if result.Workspace != initial.Workspace {
		t.Fatalf("changed worktree path = %q, want %q", result.Workspace, initial.Workspace)
	}
	if _, err := os.Stat(filepath.Join(repository, "child.txt")); !os.IsNotExist(err) {
		t.Fatalf("child wrote into parent checkout: stat error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(initial.Workspace, "child.txt")); err != nil || string(content) != "from child\n" {
		t.Fatalf("child worktree file = %q, %v", content, err)
	}

	for _, forbidden := range []string{"run_command", "git", "delegate_investigation"} {
		for _, available := range model.toolNames {
			if available == forbidden {
				t.Errorf("implement child unexpectedly has tool %q", forbidden)
			}
		}
	}
}

func runGitForDelegateTest(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}
