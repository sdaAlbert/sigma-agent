package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The fake server runs as a child of this test binary so the client can be
// exercised over a real pipe without depending on an installed MCP server.
func TestMain(m *testing.M) {
	if os.Getenv("SIGMA_FAKE_MCP_SERVER") == "1" {
		runFakeServer()
		return
	}
	os.Exit(m.Run())
}

type fakeRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func runFakeServer() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 32*1024), MaxFrameBytes)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request fakeRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{ModernProtocolVersion}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "Echo the input", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}}},
				{"name": "boom", "description": "Always fails", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if params.Name == "hang" {
				// Never answer, so the caller can exercise its timeout.
				continue
			}
			if params.Name == "boom" {
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "tool failed"}}, "isError": true}
				break
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "echo:" + string(params.Arguments)}}}
		default:
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}

func startFakeServer(t *testing.T) *Client {
	t.Helper()
	client, err := Start(context.Background(), Config{
		Name:    "fake",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMain"},
		Env:     map[string]string{"SIGMA_FAKE_MCP_SERVER": "1"},
	})
	if err != nil {
		t.Fatalf("Start(fake MCP server) = %v, want no error", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestClientListsServerTools(t *testing.T) {
	client := startFakeServer(t)
	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatalf("Tools() = %v, want no error", err)
	}
	if len(tools) != 2 {
		t.Fatalf("Tools() returned %d tools, want 2", len(tools))
	}
	if tools[0].Name != "echo" || tools[0].Description != "Echo the input" {
		t.Fatalf("Tools()[0] = %+v, want echo", tools[0])
	}
	if !strings.Contains(string(tools[0].InputSchema), "\"text\"") {
		t.Fatalf("Tools()[0].InputSchema = %s, want a text property", tools[0].InputSchema)
	}
}

func TestClientCallsServerTool(t *testing.T) {
	client := startFakeServer(t)
	output, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool(echo) = %v, want no error", err)
	}
	if !strings.Contains(output, "echo:") {
		t.Fatalf("CallTool(echo) = %q, want an echo result", output)
	}
}

func TestClientReportsToolErrors(t *testing.T) {
	client := startFakeServer(t)
	output, err := client.CallTool(context.Background(), "boom", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("CallTool(boom) succeeded, want an error")
	}
	if !strings.Contains(output, "tool failed") {
		t.Fatalf("CallTool(boom) output = %q, want the server text", output)
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("CallTool(boom) error = %q, want the server text", err)
	}
}

func TestClientRejectsUnknownServerName(t *testing.T) {
	if _, err := Start(context.Background(), Config{Command: "true"}); err == nil {
		t.Fatal("Start() without a server name succeeded, want an error")
	}
	if _, err := Start(context.Background(), Config{Name: "fake"}); err == nil {
		t.Fatal("Start() without a command succeeded, want an error")
	}
}

func TestClientReportsMissingExecutable(t *testing.T) {
	if _, err := Start(context.Background(), Config{Name: "fake", Command: "sigma-agent-missing-mcp-binary"}); err == nil {
		t.Fatal("Start() with a missing executable succeeded, want an error")
	}
}

func TestToolNameNamespacesAndBounds(t *testing.T) {
	if got := ToolName("web", "web_search"); got != "mcp__web__web_search" {
		t.Fatalf("ToolName(web, web_search) = %q, want mcp__web__web_search", got)
	}
	if got := ToolName("web", "odd name"); !strings.HasPrefix(got, "mcp__web__odd_name_") {
		t.Fatalf("ToolName(web, odd name) = %q, want sanitized name with collision-resistant suffix", got)
	}
	if first, second := ToolName("web", "odd name"), ToolName("web", "odd/name"); first == second {
		t.Fatalf("ToolName() collided after sanitizing different names: %q", first)
	}
	long := ToolName("web", strings.Repeat("x", 200))
	if len(long) > 64 {
		t.Fatalf("ToolName() length = %d, want at most 64", len(long))
	}
	if again := ToolName("web", strings.Repeat("x", 200)); again != long {
		t.Fatalf("ToolName() = %q then %q, want a stable name", long, again)
	}
	if short := ToolName("web", strings.Repeat("x", 100)); short == long {
		t.Fatal("ToolName() must distinguish different tool names")
	}
}

func TestReplaceEnvAppliesOverrides(t *testing.T) {
	got := ReplaceEnv([]string{"PATH=/usr/bin", "A=1", "B=2"}, map[string]string{"A": "9", "C": "3"})
	want := []string{"PATH=/usr/bin", "B=2", "A=9", "C=3"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ReplaceEnv() = %#v, want %#v", got, want)
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	client, err := Start(context.Background(), Config{
		Name:    "fake",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMain"},
		Env:     map[string]string{"SIGMA_FAKE_MCP_SERVER": "1"},
	})
	if err != nil {
		t.Fatalf("Start(fake MCP server) = %v, want no error", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() = %v, want no error", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() = %v, want no error", err)
	}
	if _, err := client.Tools(context.Background()); err == nil {
		t.Fatal("Tools() after Close() succeeded, want an error")
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	client := startFakeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := client.CallTool(ctx, "hang", json.RawMessage(`{}`)); err == nil {
		t.Fatal("CallTool(hang) with an expiring context succeeded, want an error")
	}
}
