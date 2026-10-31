package sigmaagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This integration test uses a real Qdrant instance. Its local embedding
// endpoint returns deterministic vectors to exercise storage, exact repo
// filtering, and stale-entry synchronization without claiming semantic quality.
func TestMemoryIndexAndSearchIsRepositoryScoped(t *testing.T) {
	qdrantURL := strings.TrimRight(os.Getenv("SIGMA_AGENT_TEST_QDRANT_URL"), "/")
	if qdrantURL == "" {
		t.Skip("set SIGMA_AGENT_TEST_QDRANT_URL to a disposable Qdrant instance")
	}

	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vectors := make([][]float32, len(request.Inputs))
		for i := range request.Inputs {
			// Identical vectors intentionally make repo_id filtering the only
			// reason a neighboring repository's memory cannot appear.
			vectors[i] = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(vectors)
	}))
	defer embedServer.Close()

	v := &vectorConfig{
		qdrantURL: qdrantURL,
		embedURL:  embedServer.URL,
		model:     "test-deterministic",
		provider:  "huggingface",
		client:    &http.Client{Timeout: 5 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = v.request(ctx, http.MethodDelete, "/collections/"+memoryCollection, nil, nil)
	t.Cleanup(func() {
		_ = v.request(context.Background(), http.MethodDelete, "/collections/"+memoryCollection, nil, nil)
	})

	repoA := filepath.Join(t.TempDir(), "repo-a")
	repoB := filepath.Join(t.TempDir(), "repo-b")
	writeFixture(t, filepath.Join(repoA, ".sigma-agent", "MEMORY.md"), "# Memory\n\n- 2026-10-30 | source: docs/queue.md | Queue recovery is tested after process restart.\n")
	writeFixture(t, filepath.Join(repoB, ".sigma-agent", "MEMORY.md"), "# Memory\n\n- 2026-10-30 | source: docs/billing.md | Billing retries use an idempotency key.\n")

	if count, err := v.indexMemory(ctx, repoA); err != nil || count != 1 {
		t.Fatalf("index repo A = (%d, %v), want (1, nil)", count, err)
	}
	if count, err := v.indexMemory(ctx, repoB); err != nil || count != 1 {
		t.Fatalf("index repo B = (%d, %v), want (1, nil)", count, err)
	}
	hits, err := v.searchMemory(ctx, repoA, "How is queue recovery tested?", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Entry.Text, "Queue recovery") {
		t.Fatalf("repo A search returned cross-repository or unexpected entries: %#v", hits)
	}
	toolResult, err := workspace{root: repoA}.searchMemoryTool(v, defaultMaxRetrievalQueryBytes).Handle(ctx, json.RawMessage(`{"query":"How is queue recovery tested?"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolResult, "source=docs/queue.md") || !strings.Contains(toolResult, "reviewed=2026-10-30") {
		t.Fatalf("search_memory tool omitted provenance: %s", toolResult)
	}

	writeFixture(t, filepath.Join(repoA, ".sigma-agent", "MEMORY.md"), "# Memory\n\n- 2026-10-30 | source: docs/queue.md | Queue recovery now uses a durable checkpoint.\n")
	hits, err = v.searchMemory(ctx, repoA, "How is queue recovery tested?", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("stale indexed memory should not be returned: %#v", hits)
	}
	if count, err := v.indexMemory(ctx, repoA); err != nil || count != 1 {
		t.Fatalf("reindex updated memory = (%d, %v), want (1, nil)", count, err)
	}
	hits, err = v.searchMemory(ctx, repoA, "How is queue recovery tested?", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Entry.Text, "durable checkpoint") {
		t.Fatalf("updated memory not searchable after reindex: %#v", hits)
	}
}
