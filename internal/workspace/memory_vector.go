package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

const memoryCollection = "sigma_agent_memory"

type memorySearchHit struct {
	Entry MemoryEntry
	Score float64
}

// searchMemoryBM25 searches the reviewed source file directly. It keeps
// repository facts useful even when no embedding provider or Qdrant is set up.
func searchMemoryBM25(root, query string, topK int, repositoryIDs ...string) ([]memorySearchHit, error) {
	var entries []MemoryEntry
	var err error
	if len(repositoryIDs) > 0 && strings.TrimSpace(repositoryIDs[0]) != "" {
		entries, err = loadMemoryEntriesWithRepoID(root, repositoryIDs[0])
	} else {
		entries, err = LoadMemoryEntries(root)
	}
	if err != nil {
		return nil, err
	}
	queryTerms := uniqueMemoryTerms(memoryTokens(query))
	if len(entries) == 0 || len(queryTerms) == 0 || topK < 1 {
		return nil, nil
	}

	docTerms := make([][]string, len(entries))
	docFreq := make([]map[string]int, len(entries))
	docFrequency := make(map[string]int)
	totalLength := 0
	for i, entry := range entries {
		docTerms[i] = memoryTokens(entry.Kind + " " + entry.Source + " " + entry.Text)
		totalLength += len(docTerms[i])
		docFreq[i] = make(map[string]int)
		for _, term := range docTerms[i] {
			docFreq[i][term]++
		}
		for term := range docFreq[i] {
			docFrequency[term]++
		}
	}
	averageLength := float64(totalLength) / float64(len(entries))
	if averageLength == 0 {
		return nil, nil
	}

	hits := make([]memorySearchHit, 0, len(entries))
	for i, entry := range entries {
		length := float64(len(docTerms[i]))
		score := 0.0
		for _, term := range queryTerms {
			tf := float64(docFreq[i][term])
			if tf == 0 {
				continue
			}
			df := float64(docFrequency[term])
			idf := math.Log(1 + (float64(len(entries))-df+0.5)/(df+0.5))
			score += idf * (tf * 2.2) / (tf + 1.2*(0.25+0.75*length/averageLength))
		}
		if score > 0 {
			hits = append(hits, memorySearchHit{Entry: entry, Score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			if hits[i].Entry.Source == hits[j].Entry.Source {
				return hits[i].Entry.Line < hits[j].Entry.Line
			}
			return hits[i].Entry.Source < hits[j].Entry.Source
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

func memoryTokens(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var word strings.Builder
	var han []rune
	flushWord := func() {
		if word.Len() > 1 {
			tokens = append(tokens, word.String())
		}
		word.Reset()
	}
	flushHan := func() {
		if len(han) == 1 {
			tokens = append(tokens, string(han[0]))
		} else {
			for i := 0; i+1 < len(han); i++ {
				tokens = append(tokens, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			flushHan()
			word.WriteRune(r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return tokens
}

func uniqueMemoryTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	unique := make([]string, 0, len(terms))
	for _, term := range terms {
		if !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}
	return unique
}

// rankMemoryHybrid combines lexical and semantic candidate ranks with
// reciprocal-rank fusion. Scores from BM25 and cosine similarity are not
// directly comparable, so only each result's rank contributes.
func rankMemoryHybrid(lexical, semantic []memorySearchHit, topK int) []memorySearchHit {
	scores := make(map[string]float64, len(lexical)+len(semantic))
	entries := make(map[string]MemoryEntry, len(lexical)+len(semantic))
	for rank, hit := range lexical {
		if hit.Entry.ID == "" {
			continue
		}
		entries[hit.Entry.ID] = hit.Entry
		scores[hit.Entry.ID] += 1 / float64(60+rank+1)
	}
	for rank, hit := range semantic {
		if hit.Entry.ID == "" {
			continue
		}
		entries[hit.Entry.ID] = hit.Entry
		scores[hit.Entry.ID] += 1 / float64(60+rank+1)
	}
	ranked := make([]memorySearchHit, 0, len(scores))
	for id, score := range scores {
		ranked = append(ranked, memorySearchHit{Entry: entries[id], Score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			if ranked[i].Entry.Source == ranked[j].Entry.Source {
				return ranked[i].Entry.Line < ranked[j].Entry.Line
			}
			return ranked[i].Entry.Source < ranked[j].Entry.Source
		}
		return ranked[i].Score > ranked[j].Score
	})
	if topK > 0 && len(ranked) > topK {
		ranked = ranked[:topK]
	}
	return ranked
}

// indexMemory syncs reviewed Markdown facts into a separate derived Qdrant
// collection. MEMORY.md remains the source of truth.
func (v *vectorConfig) indexMemory(ctx context.Context, root string) (int, error) {
	repoID, err := repositoryID(root)
	if err != nil {
		return 0, err
	}
	return v.indexMemoryForRepo(ctx, root, repoID)
}

func (v *vectorConfig) indexMemoryForRepo(ctx context.Context, root, repoID string) (int, error) {
	entries, err := loadMemoryEntriesWithRepoID(root, repoID)
	if err != nil {
		return 0, err
	}
	return v.indexMemoryEntries(ctx, repoID, entries)
}

func (v *vectorConfig) indexMemoryEntries(ctx context.Context, repoID string, entries []MemoryEntry) (int, error) {
	exists, err := v.memoryCollectionExists(ctx)
	if err != nil {
		return 0, err
	}
	if !exists && len(entries) == 0 {
		return 0, nil
	}
	existingIDs := make(map[string]bool)
	if exists {
		existingIDs, err = v.memoryPointIDs(ctx, repoID)
		if err != nil {
			return 0, err
		}
	}
	activeIDs := make(map[string]bool, len(entries))
	var newEntries []MemoryEntry
	var texts []string
	for _, entry := range entries {
		activeIDs[entry.ID] = true
		if existingIDs[entry.ID] {
			continue
		}
		newEntries = append(newEntries, entry)
		texts = append(texts, entry.Kind+"\n"+entry.Source+"\n"+entry.Text)
	}
	if len(newEntries) > 0 {
		vectors, err := v.embeddings(ctx, texts)
		if err != nil {
			return 0, err
		}
		if len(vectors) == 0 || len(vectors[0]) == 0 {
			return 0, errors.New("memory embedding endpoint returned an empty vector")
		}
		if err := v.ensureNamedCollection(ctx, memoryCollection, len(vectors[0])); err != nil {
			return 0, err
		}
		if err := v.ensureMemoryRepositoryIndex(ctx); err != nil {
			return 0, err
		}
		if err := v.upsertMemory(ctx, newEntries, vectors); err != nil {
			return 0, err
		}
	}
	staleIDs := make([]string, 0)
	for id := range existingIDs {
		if !activeIDs[id] {
			staleIDs = append(staleIDs, id)
		}
	}
	sort.Strings(staleIDs)
	if err := v.deleteMemoryPoints(ctx, staleIDs); err != nil {
		return 0, err
	}
	return len(entries), nil
}

func (v *vectorConfig) ensureMemoryRepositoryIndex(ctx context.Context) error {
	var result map[string]any
	return v.request(ctx, http.MethodPut, "/collections/"+memoryCollection+"/index?wait=true", map[string]any{
		"field_name": "repo_id", "field_schema": "keyword",
	}, &result)
}

func (v *vectorConfig) searchMemory(ctx context.Context, root, query string, topK int) ([]memorySearchHit, error) {
	repoID, err := repositoryID(root)
	if err != nil {
		return nil, err
	}
	return v.searchMemoryForRepo(ctx, root, repoID, query, topK)
}

func (v *vectorConfig) searchMemoryForRepo(ctx context.Context, root, repoID, query string, topK int) ([]memorySearchHit, error) {
	entries, err := loadMemoryEntriesWithRepoID(root, repoID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("no entries found in .sigma-agent/MEMORY.md")
	}
	queryVectors, err := v.embeddings(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(queryVectors) != 1 || len(queryVectors[0]) == 0 {
		return nil, errors.New("memory query embedding endpoint returned an empty vector")
	}
	indexed, err := v.searchMemoryVectors(ctx, repoID, queryVectors[0], topK)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return nil, errors.New("memory vector index is missing; run Sigma Agent --index-memory for this repository first")
		}
		return nil, err
	}
	current := make(map[string]MemoryEntry, len(entries))
	for _, entry := range entries {
		current[entry.ID] = entry
	}
	hits := make([]memorySearchHit, 0, len(indexed))
	for _, hit := range indexed {
		entry, ok := current[hit.Entry.ID]
		if !ok || entry.ContentHash != hit.Entry.ContentHash {
			continue
		}
		hits = append(hits, memorySearchHit{Entry: entry, Score: hit.Score})
	}
	return hits, nil
}

func (v *vectorConfig) memoryCollectionExists(ctx context.Context) (bool, error) {
	var existing map[string]any
	err := v.request(ctx, http.MethodGet, "/collections/"+memoryCollection, nil, &existing)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "HTTP 404") {
		return false, nil
	}
	return false, err
}

func (v *vectorConfig) ensureNamedCollection(ctx context.Context, collection string, dimension int) error {
	var existing map[string]any
	err := v.request(ctx, http.MethodGet, "/collections/"+collection, nil, &existing)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		return err
	}
	var result map[string]any
	return v.request(ctx, http.MethodPut, "/collections/"+collection, map[string]any{"vectors": map[string]any{"size": dimension, "distance": "Cosine"}}, &result)
}

func (v *vectorConfig) memoryPointIDs(ctx context.Context, repoID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	var offset any
	for {
		input := map[string]any{"filter": repoFilter(repoID), "limit": 256, "with_payload": false, "with_vector": false}
		if offset != nil {
			input["offset"] = offset
		}
		var response struct {
			Result struct {
				Points []struct {
					ID json.RawMessage `json:"id"`
				} `json:"points"`
				NextPageOffset any `json:"next_page_offset"`
			} `json:"result"`
		}
		if err := v.request(ctx, http.MethodPost, "/collections/"+memoryCollection+"/points/scroll", input, &response); err != nil {
			return nil, err
		}
		for _, point := range response.Result.Points {
			var id string
			if err := json.Unmarshal(point.ID, &id); err != nil {
				return nil, fmt.Errorf("decode memory Qdrant point ID: %w", err)
			}
			ids[id] = true
		}
		if response.Result.NextPageOffset == nil || len(response.Result.Points) == 0 {
			return ids, nil
		}
		offset = response.Result.NextPageOffset
	}
}

func (v *vectorConfig) upsertMemory(ctx context.Context, entries []MemoryEntry, vectors [][]float32) error {
	if len(entries) != len(vectors) {
		return errors.New("memory entry and embedding counts do not match")
	}
	points := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		points = append(points, map[string]any{"id": entry.ID, "vector": vectors[i], "payload": entry})
	}
	var result map[string]any
	return v.request(ctx, http.MethodPut, "/collections/"+memoryCollection+"/points?wait=true", map[string]any{"points": points}, &result)
}

func (v *vectorConfig) deleteMemoryPoints(ctx context.Context, ids []string) error {
	for start := 0; start < len(ids); start += 256 {
		end := start + 256
		if end > len(ids) {
			end = len(ids)
		}
		var result map[string]any
		if err := v.request(ctx, http.MethodPost, "/collections/"+memoryCollection+"/points/delete?wait=true", map[string]any{"points": ids[start:end]}, &result); err != nil {
			return err
		}
	}
	return nil
}

func (v *vectorConfig) searchMemoryVectors(ctx context.Context, repoID string, vector []float32, limit int) ([]memorySearchHit, error) {
	var response struct {
		Result []struct {
			Score   float64     `json:"score"`
			Payload MemoryEntry `json:"payload"`
		} `json:"result"`
	}
	input := map[string]any{
		"vector": vector, "limit": limit, "with_payload": true,
		"filter": repoFilter(repoID),
	}
	if err := v.request(ctx, http.MethodPost, "/collections/"+memoryCollection+"/points/search", input, &response); err != nil {
		return nil, err
	}
	hits := make([]memorySearchHit, 0, len(response.Result))
	for _, result := range response.Result {
		hits = append(hits, memorySearchHit{Entry: result.Payload, Score: result.Score})
	}
	return hits, nil
}
