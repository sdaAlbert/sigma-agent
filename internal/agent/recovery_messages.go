package agent

import (
	"fmt"
	"strings"
)

func truncatedToolCallRecoveryPrompt() string {
	return "The previous tool-call output was truncated by the output-token limit. No partial tool call was executed. Re-issue the required tool call with complete arguments; do not assume the partial call succeeded."
}

func truncatedResponseRecoveryPrompt() string {
	return "Your previous response ended because it reached the output-token limit. Continue from exactly where it stopped, without repeating the completed part. If more output is needed, continue in another section."
}

func requiredSkillsRecoveryPrompt() string {
	return "Before any repository inspection or implementation, call read_skill for each required Skill by its exact name. The only available tool is read_skill until those Skills are loaded."
}

func missingStopReasonRecoveryPrompt() string {
	return "The previous model response ended without a finish reason. Continue the original task from the current repository state. Make one concrete tool call or provide a final answer; do not repeat completed exploration."
}

func verificationRecoveryPrompt(missing []string) string {
	return "Your attempted final response was held because the latest source changes still need " + strings.Join(missing, " and ") + ". Continue from the current patch, complete those checks, and revise the conclusion if the evidence changes."
}

func toolErrorOutput(toolName, message string) string {
	return "Tool error: " + message + "\nRecovery guidance: " + toolErrorRecoveryHint(toolName, message)
}

func toolErrorRecoveryHint(toolName, message string) string {
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "changed since it was read"):
		return "Read the current file again, then rebase the intended localized edit on that version."
	case strings.Contains(message, "old_text must match exactly once"):
		return "Read a narrower file range, copy the exact unique text from it, and retry the edit with a smaller replacement."
	case strings.Contains(message, "outside /workspace"), strings.Contains(message, "workspace-relative"), strings.Contains(message, "parent-directory traversal"):
		return "Use pwd to confirm the task workspace, then retry with a path relative to that workspace and without '..'."
	case strings.Contains(message, "would shrink the file"):
		return "Re-read the affected region and make a localized edit; then inspect the diff and run the focused test."
	case strings.Contains(message, "purpose=test commands cannot contain"):
		return "Run the test process directly with purpose=test; remove pipes and semicolons, and chain setup with &&."
	case strings.Contains(message, "must be successfully read with read_file"):
		return "Read the named evidence file with read_file, then retry recording the change contract."
	default:
		return fmt.Sprintf("Use this error to choose the smallest corrective action before retrying %s; change the path, arguments, command, or workspace state instead of repeating the unchanged call.", toolName)
	}
}
