package sigmaagent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// MemoryRelevance identifies a reviewed memory entry using its source and a
// human-written distinguishing phrase from the fact text.
type MemoryRelevance struct {
	Source       string `json:"source"`
	FactContains string `json:"fact_contains"`
}

type MemoryRetrievalCase struct {
	ID       string            `json:"id"`
	Query    string            `json:"query"`
	Relevant []MemoryRelevance `json:"relevant"`
}

type MemoryRetrievalMetrics struct {
	Cases     int     `json:"cases"`
	RecallAtK float64 `json:"recall_at_k"`
	MRR       float64 `json:"mrr"`
}

type MemoryRetrievalResult struct {
	ID           string            `json:"id"`
	Query        string            `json:"query"`
	Relevant     []MemoryRelevance `json:"relevant"`
	BM25Top      []MemoryRelevance `json:"bm25_top"`
	VectorTop    []MemoryRelevance `json:"vector_top"`
	HybridTop    []MemoryRelevance `json:"hybrid_top"`
	BM25Recall   float64           `json:"bm25_recall_at_k"`
	VectorRecall float64           `json:"vector_recall_at_k"`
	HybridRecall float64           `json:"hybrid_recall_at_k"`
	BM25MRR      float64           `json:"bm25_mrr"`
	VectorMRR    float64           `json:"vector_mrr"`
	HybridMRR    float64           `json:"hybrid_mrr"`
}

type MemoryRetrievalEvaluation struct {
	CorpusEntries int                     `json:"corpus_entries"`
	K             int                     `json:"k"`
	BM25          MemoryRetrievalMetrics  `json:"bm25"`
	Vector        MemoryRetrievalMetrics  `json:"vector"`
	Hybrid        MemoryRetrievalMetrics  `json:"hybrid_rrf"`
	Results       []MemoryRetrievalResult `json:"results"`
}

// EvaluateMemoryRetrieval compares lexical, vector, and fused rankings over
// the current reviewed memory file. It requires the configured real embedding
// endpoint and Qdrant; it never substitutes fabricated vectors.
func EvaluateMemoryRetrieval(ctx context.Context, root string, cases []MemoryRetrievalCase, k int) (MemoryRetrievalEvaluation, error) {
	if len(cases) == 0 {
		return MemoryRetrievalEvaluation{}, errors.New("memory retrieval dataset is empty")
	}
	if k < 1 {
		k = 5
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return MemoryRetrievalEvaluation{}, err
	}
	repoID := "memory-eval-" + directoryID(absoluteRoot)
	vector := newVectorConfig()
	if vector == nil {
		return MemoryRetrievalEvaluation{}, errors.New("configure a real embedding endpoint before --eval-memory")
	}
	entries, err := loadMemoryEntriesWithRepoID(root, repoID)
	if err != nil {
		return MemoryRetrievalEvaluation{}, err
	}
	if len(entries) == 0 {
		return MemoryRetrievalEvaluation{}, errors.New("no reviewed entries found in .sigma-agent/MEMORY.md")
	}
	for _, item := range cases {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Query) == "" || len(item.Relevant) == 0 {
			return MemoryRetrievalEvaluation{}, fmt.Errorf("case %q needs id, query, and at least one relevant entry", item.ID)
		}
		for _, relevant := range item.Relevant {
			if !memoryRelevanceExists(entries, relevant) {
				return MemoryRetrievalEvaluation{}, fmt.Errorf("case %q relevance label does not match current MEMORY.md: source=%q fact_contains=%q", item.ID, relevant.Source, relevant.FactContains)
			}
		}
	}
	if _, err := vector.indexMemoryForRepo(ctx, root, repoID); err != nil {
		return MemoryRetrievalEvaluation{}, fmt.Errorf("index reviewed memory: %w", err)
	}

	evaluation := MemoryRetrievalEvaluation{CorpusEntries: len(entries), K: k, Results: make([]MemoryRetrievalResult, 0, len(cases))}
	var bm25Recall, vectorRecall, hybridRecall, bm25MRR, vectorMRR, hybridMRR float64
	for _, item := range cases {
		lexical, err := searchMemoryBM25(root, item.Query, k*4)
		if err != nil {
			return MemoryRetrievalEvaluation{}, fmt.Errorf("case %q BM25 search: %w", item.ID, err)
		}
		semantic, err := vector.searchMemoryForRepo(ctx, root, repoID, item.Query, k*4)
		if err != nil {
			return MemoryRetrievalEvaluation{}, fmt.Errorf("case %q vector search: %w", item.ID, err)
		}
		hybrid := rankMemoryHybrid(lexical, semantic, k*4)
		result := MemoryRetrievalResult{ID: item.ID, Query: item.Query, Relevant: item.Relevant}
		result.BM25Top = memoryLabels(lexical, k)
		result.VectorTop = memoryLabels(semantic, k)
		result.HybridTop = memoryLabels(hybrid, k)
		result.BM25Recall, result.BM25MRR = scoreMemoryRanking(lexical, item.Relevant, k)
		result.VectorRecall, result.VectorMRR = scoreMemoryRanking(semantic, item.Relevant, k)
		result.HybridRecall, result.HybridMRR = scoreMemoryRanking(hybrid, item.Relevant, k)
		bm25Recall += result.BM25Recall
		vectorRecall += result.VectorRecall
		hybridRecall += result.HybridRecall
		bm25MRR += result.BM25MRR
		vectorMRR += result.VectorMRR
		hybridMRR += result.HybridMRR
		evaluation.Results = append(evaluation.Results, result)
	}
	denom := float64(len(cases))
	evaluation.BM25 = MemoryRetrievalMetrics{Cases: len(cases), RecallAtK: bm25Recall / denom, MRR: bm25MRR / denom}
	evaluation.Vector = MemoryRetrievalMetrics{Cases: len(cases), RecallAtK: vectorRecall / denom, MRR: vectorMRR / denom}
	evaluation.Hybrid = MemoryRetrievalMetrics{Cases: len(cases), RecallAtK: hybridRecall / denom, MRR: hybridMRR / denom}
	return evaluation, nil
}

func memoryRelevanceExists(entries []MemoryEntry, relevance MemoryRelevance) bool {
	if strings.TrimSpace(relevance.Source) == "" || strings.TrimSpace(relevance.FactContains) == "" {
		return false
	}
	for _, entry := range entries {
		if entry.Source == relevance.Source && strings.Contains(entry.Text, relevance.FactContains) {
			return true
		}
	}
	return false
}

func memoryLabels(hits []memorySearchHit, k int) []MemoryRelevance {
	if k < 0 {
		k = 0
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	labels := make([]MemoryRelevance, 0, len(hits))
	for _, hit := range hits {
		labels = append(labels, MemoryRelevance{Source: hit.Entry.Source, FactContains: hit.Entry.Text})
	}
	return labels
}

func scoreMemoryRanking(hits []memorySearchHit, relevant []MemoryRelevance, k int) (float64, float64) {
	if len(relevant) == 0 || k < 1 {
		return 0, 0
	}
	wanted := make(map[string]bool, len(relevant))
	for _, label := range relevant {
		wanted[memoryRelevanceKey(label)] = true
	}
	found := make(map[string]bool)
	firstRank := 0
	for i, hit := range hits {
		if i >= k {
			break
		}
		for _, label := range relevant {
			key := memoryRelevanceKey(label)
			if wanted[key] && hit.Entry.Source == label.Source && strings.Contains(hit.Entry.Text, label.FactContains) {
				found[key] = true
				if firstRank == 0 {
					firstRank = i + 1
				}
			}
		}
	}
	return float64(len(found)) / float64(len(wanted)), reciprocalRank(firstRank)
}

func memoryRelevanceKey(relevance MemoryRelevance) string {
	return relevance.Source + "\n" + relevance.FactContains
}

func reciprocalRank(rank int) float64 {
	if rank < 1 {
		return 0
	}
	return 1 / float64(rank)
}
