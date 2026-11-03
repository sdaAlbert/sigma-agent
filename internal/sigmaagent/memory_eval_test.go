package sigmaagent

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedInMemoryRetrievalLabelsMatchFixture(t *testing.T) {
	root := filepath.Join("..", "..", "evals", "fixtures", "memory-retrieval-v1")
	entries, err := LoadMemoryEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	isolatedEntries, err := loadMemoryEntriesWithRepoID(root, "memory-eval-test-namespace")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || len(isolatedEntries) == 0 {
		t.Fatal("expected fixture facts")
	}
	if entries[0].RepoID == isolatedEntries[0].RepoID || entries[0].ID == isolatedEntries[0].ID {
		t.Fatal("evaluation namespace must not reuse the current Git repository identity or point ID")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "evals", "memory-retrieval-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []MemoryRetrievalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, item := range cases {
		for _, relevant := range item.Relevant {
			if !memoryRelevanceExists(entries, relevant) {
				t.Errorf("case %q has no matching reviewed fact for %q / %q", item.ID, relevant.Source, relevant.FactContains)
			}
		}
	}
}

func TestScoreMemoryRankingUsesHumanLabeledFactMatches(t *testing.T) {
	queryRelevant := []MemoryRelevance{
		{Source: "docs/testing.md", FactContains: "no external egress"},
		{Source: "docs/architecture.md", FactContains: "memory is repository-scoped"},
	}
	hits := []memorySearchHit{
		{Entry: MemoryEntry{Source: "docs/other.md", Text: "unrelated"}},
		{Entry: MemoryEntry{Source: "docs/testing.md", Text: "The test runner uses Docker and has no external egress."}},
	}
	recall, mrr := scoreMemoryRanking(hits, queryRelevant, 5)
	if math.Abs(recall-0.5) > 1e-9 || math.Abs(mrr-0.5) > 1e-9 {
		t.Fatalf("scores = (recall %.3f, mrr %.3f), want (0.5, 0.5)", recall, mrr)
	}
}

func TestRankMemoryHybridFusesIndependentRanks(t *testing.T) {
	a := MemoryEntry{ID: "a", Source: "a.md", Line: 1}
	b := MemoryEntry{ID: "b", Source: "b.md", Line: 2}
	c := MemoryEntry{ID: "c", Source: "c.md", Line: 3}
	got := rankMemoryHybrid(
		[]memorySearchHit{{Entry: a}, {Entry: b}},
		[]memorySearchHit{{Entry: c}, {Entry: b}},
		3,
	)
	if len(got) != 3 || got[0].Entry.ID != "b" {
		t.Fatalf("hybrid rank = %#v, want shared entry b first", got)
	}
}

func TestMemoryRelevanceRequiresSourceAndFactText(t *testing.T) {
	entries := []MemoryEntry{{Source: "testing.md", Text: "no external egress"}}
	if !memoryRelevanceExists(entries, MemoryRelevance{Source: "testing.md", FactContains: "egress"}) {
		t.Fatal("expected label to match source and fact substring")
	}
	if memoryRelevanceExists(entries, MemoryRelevance{Source: "other.md", FactContains: "egress"}) {
		t.Fatal("label from a different source must not match")
	}
}
