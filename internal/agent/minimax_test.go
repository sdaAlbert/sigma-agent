package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMiniMaxReadsProjectLocalAPIKey(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "MINIMAX_API_KEY"} {
		t.Setenv(name, "")
	}
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".secrets", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".secrets", "minimax-api-key"), []byte(" local-test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	model, err := NewMiniMaxMessagesModelFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if model.APIKey != "local-test-key" {
		t.Fatal("model did not load and trim its project-local API key")
	}
}

func TestMiniMaxEnvironmentAPIKeyTakesPrecedence(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "environment-key")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("MINIMAX_API_KEY", "")
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".secrets", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".secrets", "minimax-api-key"), []byte("file-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	model, err := NewMiniMaxMessagesModelFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if model.APIKey != "environment-key" {
		t.Fatal("environment API key should take precedence over the local key file")
	}
}

func TestMiniMaxDoesNotRetryExhaustedBalance(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"message":"insufficient balance"}`)
	}))
	defer server.Close()

	model := &MiniMaxMessagesModel{Model: "test", BaseURL: server.URL, APIKey: "test", Client: server.Client(), MaxRetries: 4}
	_, err := model.Stream(context.Background(), "system", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "quota or balance is exhausted") {
		t.Fatalf("Stream() error = %v, want an exhausted-balance error", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1 for permanent quota error", got)
	}
}

func TestMiniMaxRetriesTransientRateLimit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"message":"rate limit exceeded"}`)
	}))
	defer server.Close()

	model := &MiniMaxMessagesModel{Model: "test", BaseURL: server.URL, APIKey: "test", Client: server.Client(), MaxRetries: 2}
	_, err := model.Stream(context.Background(), "system", nil, nil, nil)
	if err == nil {
		t.Fatal("Stream() succeeded, want rate-limit error")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("request count = %d, want initial request plus 2 retries", got)
	}
}
