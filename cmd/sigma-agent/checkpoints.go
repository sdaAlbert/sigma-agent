package main

import (
	"context"
	"fmt"
	"strings"

	workspaceagent "github.com/sdaAlbert/sigma-agent/internal/workspace"
)

func listWorkspaceCheckpoints(ctx context.Context, checkpoints *workspaceagent.WorkspaceCheckpoints) (string, error) {
	if checkpoints == nil {
		return "Code checkpoints require a Git repository with a commit.", nil
	}
	items, err := checkpoints.List(ctx)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "No code checkpoints yet.", nil
	}
	var output strings.Builder
	output.WriteString("Code checkpoints (use /restore <id> to preview):\n")
	for _, item := range items {
		fmt.Fprintf(&output, "- %s  %s  turn %d  %s\n", item.ID, item.CreatedAt.Local().Format("2006-01-02 15:04:05"), item.Turn, item.Label)
	}
	return strings.TrimRight(output.String(), "\n"), nil
}

func previewWorkspaceRestore(ctx context.Context, checkpoints *workspaceagent.WorkspaceCheckpoints, id string) (string, error) {
	if checkpoints == nil {
		return "", fmt.Errorf("code checkpoints require a Git repository with a commit")
	}
	preview, err := checkpoints.PreviewRestore(ctx, id)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Restore preview for %s (%s):\n", preview.Checkpoint.ID, preview.Checkpoint.Label)
	if len(preview.Files) == 0 {
		output.WriteString("No workspace file changes.")
	} else {
		for _, path := range preview.Files {
			fmt.Fprintf(&output, "- %s\n", path)
		}
		fmt.Fprintf(&output, "\nThis will change %d file(s). Type /restore %s confirm to apply. Current state is backed up first.", len(preview.Files), preview.Checkpoint.ID)
	}
	return strings.TrimRight(output.String(), "\n"), nil
}

func restoreWorkspaceCheckpoint(ctx context.Context, checkpoints *workspaceagent.WorkspaceCheckpoints, id string) (string, error) {
	if checkpoints == nil {
		return "", fmt.Errorf("code checkpoints require a Git repository with a commit")
	}
	preview, err := checkpoints.Restore(ctx, id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Restored checkpoint %s; saved the previous workspace state as a checkpoint. Changed %d file(s).", preview.Checkpoint.ID, len(preview.Files)), nil
}

func isCheckpointMutationTool(name string) bool {
	switch name {
	case "create_file", "edit_file", "apply_patch", "run_command":
		return true
	default:
		return false
	}
}
