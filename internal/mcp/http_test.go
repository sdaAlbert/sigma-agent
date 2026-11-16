package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamableHTTPFallsBackAndUsesNegotiatedVersion(t *testing.T) {
	const negotiatedVersion = "2025-06-18"
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Errorf("DELETE session ID = %q", got)
			}
			if got := r.Header.Get("MCP-Protocol-Version"); got != negotiatedVersion {
				t.Errorf("DELETE protocol version = %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		methods = append(methods, request.Method)
		switch request.Method {
		case "server/discover":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
		case "initialize":
			if got := r.Header.Get("MCP-Protocol-Version"); got != "" {
				t.Errorf("initialize version header = %q, want absent", got)
			}
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": negotiatedVersion, "capabilities": map[string]any{}}})
		case "notifications/initialized":
			checkLegacyHeaders(t, r, negotiatedVersion)
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			checkLegacyHeaders(t, r, negotiatedVersion)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"tools": []any{}}})
		default:
			t.Errorf("unexpected method %q", request.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client, err := Start(nil, Config{Name: "legacy", URL: server.URL})
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if _, err := client.Tools(context.Background()); err != nil {
		t.Fatalf("Tools() = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	want := []string{"server/discover", "initialize", "notifications/initialized", "tools/list"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %v, want %v", methods, want)
		}
	}
}

func checkLegacyHeaders(t *testing.T, r *http.Request, version string) {
	t.Helper()
	if got := r.Header.Get("MCP-Protocol-Version"); got != version {
		t.Errorf("MCP-Protocol-Version = %q, want %q", got, version)
	}
	if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
		t.Errorf("Mcp-Session-Id = %q, want session-1", got)
	}
}
