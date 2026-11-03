package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/workflow"
)

type consoleWorkflowHandler struct{}

func (consoleWorkflowHandler) Notify(message string) {
	fmt.Fprintln(os.Stderr, "[extension]", safeTerminalText(message))
}

func (consoleWorkflowHandler) Confirm(context.Context, string) (bool, error) {
	return false, fmt.Errorf("confirm is available only in the interactive TUI")
}

func workflowEvent(event agent.Event) (workflow.Event, bool) {
	projected := workflow.Event{SessionID: event.Session, Turn: event.Turn, Tool: event.Tool, Status: event.Status, Error: event.Error}
	switch event.Type {
	case "turn_start", "turn_end":
		projected.Type = event.Type
	case "tool_execution_end":
		projected.Type = "tool_complete"
	default:
		return workflow.Event{}, false
	}
	return projected, true
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func registerAgentTools(host *workflow.Host, tools []agent.Tool) {
	for _, tool := range tools {
		workflowTool := workflow.Tool{
			Name: tool.Definition.Name, Description: tool.Definition.Description,
			InputSchema:   append(json.RawMessage(nil), tool.Definition.InputSchema...),
			ExecutionMode: tool.ExecutionMode, ModifiesWorkspace: tool.ModifiesWorkspace,
		}
		if err := host.RegisterTool(workflowTool, tool.Handle); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent: warning: skip workflow tool:", err)
		}
	}
}

func workflowAgentTools(host *workflow.Host, existing []agent.Tool) []agent.Tool {
	used := make(map[string]bool, len(existing))
	for _, tool := range existing {
		used[tool.Definition.Name] = true
	}
	registered := host.Tools()
	sort.Slice(registered, func(i, j int) bool { return registered[i].Name < registered[j].Name })
	tools := make([]agent.Tool, 0, len(registered))
	for _, tool := range registered {
		if used[tool.Name] {
			fmt.Fprintf(os.Stderr, "sigma-agent: warning: workflow tool %q conflicts with a built-in tool and was skipped\n", tool.Name)
			continue
		}
		used[tool.Name] = true
		name := tool.Name
		tools = append(tools, agent.Tool{
			Definition: agent.ToolDefinition{
				Name: tool.Name, Description: tool.Description,
				InputSchema: append(json.RawMessage(nil), tool.InputSchema...),
			},
			ExecutionMode:     tool.ExecutionMode,
			ModifiesWorkspace: tool.ModifiesWorkspace,
			Handle: func(ctx context.Context, arguments json.RawMessage) (string, error) {
				return host.RunTool(ctx, name, arguments)
			},
		})
	}
	return tools
}
