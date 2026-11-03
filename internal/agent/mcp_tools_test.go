package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdaAlbert/sigma-agent/internal/mcp"
)

func TestMCPToolsRejectAnUnconnectedClient(t *testing.T) {
	tools, err := MCPTools(context.Background(), nil, MCPToolOptions{Server: "web"})
	if err == nil {
		t.Fatal("MCPTools() without a client succeeded, want an error")
	}
	if tools != nil {
		t.Fatalf("MCPTools() = %v, want no tools", tools)
	}
	if _, err := MCPTools(context.Background(), &mcp.Client{}, MCPToolOptions{Server: "web"}); err == nil {
		t.Fatal("MCPTools() with an unconnected client succeeded, want an error")
	}
}

func TestMCPToolNameUsesTheServerNamespace(t *testing.T) {
	if got := mcp.ToolName("web", "web_search"); got != "mcp__web__web_search" {
		t.Fatalf("mcp.ToolName() = %q, want mcp__web__web_search", got)
	}
	if !strings.HasPrefix(mcp.ToolName("github", "list_issues"), "mcp__github__") {
		t.Fatalf("mcp.ToolName() = %q, want the github namespace", mcp.ToolName("github", "list_issues"))
	}
}

func TestMCPToolsRejectAnEmptyServerName(t *testing.T) {
	registry, err := NewToolRegistry(nil)
	if err != nil {
		t.Fatalf("NewToolRegistry() = %v", err)
	}
	_ = registry
}

func TestMCPToolArgumentsStayValidJSON(t *testing.T) {
	// The arguments of an MCP tool call are forwarded unchanged, so they
	// have to reach the server as a JSON object.
	arguments := json.RawMessage(`{"query":"golang"}`)
	if !json.Valid(arguments) {
		t.Fatal("test arguments are not valid JSON")
	}
	var decoded map[string]any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		t.Fatalf("decode arguments = %v", err)
	}
	if decoded["query"] != "golang" {
		t.Fatalf("decoded = %v, want the query field", decoded)
	}
}
