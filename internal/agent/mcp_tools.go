package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sdaAlbert/sigma-agent/internal/mcp"
)

// MCPToolOptions tunes how the tools of one MCP server are exposed to the
// model.
type MCPToolOptions struct {
	// Server is the namespace used in the exposed tool names.
	Server string
	// Describe builds the description shown to the model for one server
	// tool. A nil value uses the server tool description unchanged.
	Describe func(toolName, description string) string
	// ExecutionMode is the tool execution mode for the exposed tools.
	// Defaults to "sequential" because remote tools usually reach the
	// network and may change remote state.
	ExecutionMode string
	// Allow selects which server tools are exposed. An empty map exposes
	// every tool the server lists.
	Allow map[string]bool
	// CallTool optionally wraps remote calls with server-specific policy.
	// When nil, calls are forwarded directly to the connected MCP client.
	CallTool func(context.Context, string, json.RawMessage) (string, error)
}

// MCPTools converts the tools of a connected MCP server into Agent tools.
// Tool names are namespaced as mcp__<server>__<tool> so several servers can
// contribute tools without colliding.
func MCPTools(ctx context.Context, client *mcp.Client, options MCPToolOptions) ([]Tool, error) {
	if client == nil {
		return nil, fmt.Errorf("MCP server %q is not connected", options.Server)
	}
	remote, err := client.Tools(ctx)
	if err != nil {
		return nil, err
	}
	mode := options.ExecutionMode
	if mode == "" {
		mode = "sequential"
	}
	tools := make([]Tool, 0, len(remote))
	for _, descriptor := range remote {
		if len(options.Allow) > 0 && !options.Allow[descriptor.Name] {
			continue
		}
		description := descriptor.Description
		if options.Describe != nil {
			description = options.Describe(descriptor.Name, description)
		}
		remoteName := descriptor.Name
		tools = append(tools, Tool{
			Definition: ToolDefinition{
				Name:        mcp.ToolName(options.Server, remoteName),
				Description: description,
				InputSchema: append(json.RawMessage(nil), descriptor.InputSchema...),
			},
			ExecutionMode: mode,
			Handle: func(callCtx context.Context, arguments json.RawMessage) (string, error) {
				if options.CallTool != nil {
					return options.CallTool(callCtx, remoteName, arguments)
				}
				return client.CallTool(callCtx, remoteName, arguments)
			},
		})
	}
	return tools, nil
}
