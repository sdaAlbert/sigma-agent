package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// workflowPolicy owns per-run verification, recovery, and progress feedback.
// It keeps changing workflow state out of the system-prompt prefix.
type workflowPolicy struct {
	maxRepeatedFailedCommands int
	repeatedFailedCommands    map[string]int
	toolOrder                 int
	lastWriteOrder            int
	lastTestOrder             int
	lastDiffOrder             int
	lastTestPassed            bool
}

type workflowReview struct {
	TestsNeeded          bool
	DiffNeeded           bool
	VerificationComplete bool
}

func newWorkflowPolicy(maxRepeatedFailedCommands int) *workflowPolicy {
	return &workflowPolicy{
		maxRepeatedFailedCommands: maxRepeatedFailedCommands,
		repeatedFailedCommands:    make(map[string]int),
	}
}

func (p *workflowPolicy) review() workflowReview {
	testsNeeded := p.lastWriteOrder > 0 && p.lastTestOrder <= p.lastWriteOrder
	diffNeeded := p.lastWriteOrder > 0 && p.lastDiffOrder <= p.lastWriteOrder
	return workflowReview{
		TestsNeeded:          testsNeeded,
		DiffNeeded:           diffNeeded,
		VerificationComplete: p.lastWriteOrder > 0 && p.lastTestOrder > p.lastWriteOrder && p.lastDiffOrder > p.lastWriteOrder && p.lastTestPassed,
	}
}

// observe applies the outcome of one tool call to workflow state and returns
// concise feedback for the tool result plus any repeated-command stop reason.
func (p *workflowPolicy) observe(call, result ContentBlock, registry *ToolRegistry) (string, string) {
	if toolCallChangedWorkspaceOrEnvironment(call, result, registry) {
		clear(p.repeatedFailedCommands)
	}
	if p.maxRepeatedFailedCommands > 0 {
		if fingerprint, failedCommand := repeatedFailedCommandFingerprint(call, result); failedCommand {
			for previous := range p.repeatedFailedCommands {
				if previous != fingerprint {
					delete(p.repeatedFailedCommands, previous)
				}
			}
			p.repeatedFailedCommands[fingerprint]++
			if p.repeatedFailedCommands[fingerprint] >= p.maxRepeatedFailedCommands {
				return "Command failure repeated without a workspace change. Inspect the saved output and choose a different command or corrective action.", fmt.Sprintf("no-progress guard paused the run: the same failed command was repeated %d times without a workspace change; inspect the saved output and choose a different next action", p.repeatedFailedCommands[fingerprint])
			}
		} else if successfulTestCommand(call, result) {
			clear(p.repeatedFailedCommands)
		}
	}

	p.toolOrder++
	feedback := ""
	if call.Name == "run_command" {
		statusLine := resultStatusLine(result.Content, "SIGMA_RUN_COMMAND ")
		workspaceChanged := strings.Contains(statusLine, "workspace_changed=true")
		if workspaceChanged {
			p.lastWriteOrder = p.toolOrder
		}
		var commandInput struct {
			Purpose string `json:"purpose"`
			Action  string `json:"action"`
		}
		_ = json.Unmarshal(call.Input, &commandInput)
		status, responsePurpose, responseAction := "", "", ""
		if statusLine != "" {
			for _, field := range strings.Fields(statusLine) {
				if strings.HasPrefix(field, "status=") {
					status = strings.TrimPrefix(field, "status=")
				} else if strings.HasPrefix(field, "purpose=") {
					responsePurpose = strings.TrimPrefix(field, "purpose=")
				} else if strings.HasPrefix(field, "action=") {
					responseAction = strings.TrimPrefix(field, "action=")
				}
			}
		}
		if commandInput.Purpose == "" {
			commandInput.Purpose = responsePurpose
		}
		if commandInput.Action == "" {
			commandInput.Action = responseAction
			if commandInput.Action == "" {
				commandInput.Action = "run"
			}
		}
		if commandInput.Purpose == "test" && commandInput.Action != "start" && status != "running" {
			p.lastTestOrder = p.toolOrder
			p.lastTestPassed = !result.IsError && !workspaceChanged && strings.Contains(statusLine, "status=passed") && strings.Contains(statusLine, "exit_code=0")
			if p.lastTestPassed {
				clear(p.repeatedFailedCommands)
			}
		}
		if workspaceChanged {
			feedback = "Workflow status: this command changed the workspace, so earlier checks are stale. After the remaining edits, inspect git_diff and run a relevant test if the project provides one that can run; otherwise state why testing was not applicable or available."
		} else if commandInput.Purpose == "test" && commandInput.Action != "start" && status != "running" {
			if p.lastTestPassed {
				feedback = p.verificationFeedback()
			} else {
				feedback = "Test command failed. Inspect its first actionable failure and distinguish a code failure from an environment/tool failure. Make a targeted correction only when the failure is actionable; rerun only after that correction, then inspect git_diff. If the failure is environmental or the test is inapplicable, state that limitation and continue with diff review."
			}
		} else if status == "failed" {
			feedback = "Command failed. Use its exit code and output to identify the first concrete cause; distinguish repository/test failures from infrastructure failures before changing source. Choose one focused recovery action and change the command or workspace state before retrying."
		}
		return feedback, ""
	}

	if result.IsError {
		return "", ""
	}
	if tool, ok := registry.byName[call.Name]; ok && tool.ModifiesWorkspace {
		p.lastWriteOrder = p.toolOrder
		feedback = "Workflow status: this tool changed the workspace, so earlier checks are stale. After the remaining edits, inspect git_diff and run a relevant test if the project provides one that can run; otherwise state why testing was not applicable or available."
		return feedback, ""
	}
	switch call.Name {
	case "create_file", "edit_file", "apply_patch":
		p.lastWriteOrder = p.toolOrder
		feedback = "Workflow status: the source changed, so earlier checks are stale. After the remaining edits, inspect git_diff and run a relevant test if the project provides one that can run; otherwise state why testing was not applicable or available."
	case "git_diff":
		p.lastDiffOrder = p.toolOrder
		feedback = p.verificationFeedback()
	}
	return feedback, ""
}

func (p *workflowPolicy) verificationFeedback() string {
	review := p.review()
	switch {
	case review.TestsNeeded && review.DiffNeeded:
		return "Workflow status: the latest source changes still need a git_diff review. Run a relevant test if one exists and can run; otherwise explain why it was skipped."
	case review.TestsNeeded:
		return "Workflow status: the latest diff has been inspected. Run a relevant test if one exists and can run; otherwise explain why it was skipped."
	case review.DiffNeeded:
		return "Workflow status: the latest source changes still need a git_diff review. The relevant test has run; if it was unavailable or inapplicable, state why."
	case p.lastTestOrder > p.lastWriteOrder && p.lastDiffOrder > p.lastWriteOrder && p.lastTestPassed:
		return "Workflow status: the latest source revision has a passing test and a reviewed diff. Compare both against the acceptance checklist; if you make another edit, repeat those checks."
	default:
		return ""
	}
}

func (p *workflowPolicy) missingChecks() []string {
	if p.lastWriteOrder == 0 {
		return nil
	}
	if p.lastDiffOrder <= p.lastWriteOrder {
		return []string{"git_diff review"}
	}
	return nil
}

func (p *workflowPolicy) verificationStatus() string {
	return currentVerificationStatus(p.lastWriteOrder, p.lastTestOrder, p.lastDiffOrder, p.lastTestPassed)
}

func (p *workflowPolicy) continuationAtLimit() string {
	if missing := p.missingChecks(); len(missing) > 0 {
		return "The run reached its turn limit before all recommended checks were complete (" + strings.Join(missing, " and ") + "). Resume with the relevant available checks, then inspect the final diff. Do not keep retrying tests that are unavailable or inapplicable."
	}
	if p.lastWriteOrder > 0 && p.lastTestOrder > p.lastWriteOrder && !p.lastTestPassed {
		return "The run reached its turn limit after a failed test. Resume by inspecting the first actionable failure, make a targeted correction if supported by the evidence, and rerun the focused test only if the failure is actionable; then inspect the final diff."
	}
	return "The run reached its turn limit. Resume from the current patch and verification evidence; run a relevant test if one exists and can run, otherwise state why testing is unavailable or inapplicable. Do not repeat completed exploration."
}

func (p *workflowPolicy) checkpoint(turn, maxTurns int) string {
	interval := maxTurns / 4
	if interval < 8 {
		interval = 8
	}
	if interval > 32 {
		interval = 32
	}
	if turn >= maxTurns || turn%interval != 0 {
		return ""
	}
	review := p.review()
	status := "no source changes recorded"
	if p.lastWriteOrder > 0 {
		if missing := p.missingChecks(); len(missing) > 0 {
			status = "source changed; pending review: " + strings.Join(missing, " and ")
		} else if p.lastTestOrder > p.lastWriteOrder && !p.lastTestPassed {
			status = "the latest test failed; inspect its first actionable failure and choose one targeted next step"
		} else if review.VerificationComplete {
			status = "the latest source revision passed a test and has a reviewed diff"
		} else if review.TestsNeeded {
			status = "diff reviewed; run a relevant test if one exists and can run, or note why it was skipped"
		}
	}
	return "Progress checkpoint: reassess the original issue against tracked source changes and available verification evidence. Keep only findings that change the diagnosis or next action. If recent work repeats an unchanged hypothesis, choose one decisive source edit or focused diagnostic; continue exploring if the evidence supports it. Current verification status: " + status + ". Reserve the remaining budget for a focused test when applicable, final diff review, and any needed correction."
}
