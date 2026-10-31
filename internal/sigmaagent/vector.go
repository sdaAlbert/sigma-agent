package sigmaagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const vectorCollection = "sigma_agent_code"

type vectorConfig struct {
	qdrantURL string
	embedURL  string
	model     string
	apiKey    string
	provider  string
	client    *http.Client
}

type codeChunk struct {
	ID      string `json:"id"`
	RepoID  string `json:"repo_id"`
	Path    string `json:"path"`
	Start   int    `json:"start_line"`
	End     int    `json:"end_line"`
	Content string `json:"content"`
	Hash    string `json:"sha256"`
}

func newVectorConfig() *vectorConfig {
	if strings.TrimSpace(os.Getenv("SIGMA_AGENT_EMBEDDING_MODEL")) == "" {
		return nil
	}
	provider := strings.TrimSpace(os.Getenv("SIGMA_AGENT_EMBEDDING_PROVIDER"))
	if provider == "" {
		provider = "huggingface"
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SIGMA_AGENT_EMBEDDING_BASE_URL")), "/")
	model := strings.TrimSpace(os.Getenv("SIGMA_AGENT_EMBEDDING_MODEL"))
	key := strings.TrimSpace(os.Getenv("SIGMA_AGENT_EMBEDDING_API_KEY"))
	if key == "" && provider == "openai-compatible" {
		if data, err := os.ReadFile(filepath.Join(".secrets", "embedding-api-key")); err == nil {
			key = strings.TrimSpace(string(data))
		}
	}
	embedURL := ""
	switch provider {
	case "huggingface":
		if key == "" {
			key = strings.TrimSpace(os.Getenv("HF_TOKEN"))
		}
		if base == "" {
			base = "https://router.huggingface.co/hf-inference/models"
		}
		embedURL = base + "/" + model
	case "openai-compatible":
		if base == "" {
			return nil
		}
		embedURL = base + "/embeddings"
	default:
		return nil
	}
	qdrant := strings.TrimRight(strings.TrimSpace(os.Getenv("SIGMA_AGENT_QDRANT_URL")), "/")
	if qdrant == "" {
		qdrant = "http://localhost:6333"
	}
	return &vectorConfig{qdrantURL: qdrant, embedURL: embedURL, model: model, apiKey: key, provider: provider, client: &http.Client{Timeout: 2 * time.Minute}}
}

func (v *vectorConfig) embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	payload := map[string]any{"model": v.model, "input": texts}
	if v.provider == "huggingface" {
		payload = map[string]any{"inputs": texts, "normalize": true, "truncate": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.embedURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.apiKey)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if v.provider == "huggingface" {
		var vectors [][]float32
		if err := json.Unmarshal(data, &vectors); err != nil {
			return nil, fmt.Errorf("decode Hugging Face feature-extraction response: %w", err)
		}
		if len(vectors) != len(texts) {
			return nil, fmt.Errorf("embedding count mismatch: got %d for %d texts", len(vectors), len(texts))
		}
		for _, vector := range vectors {
			if len(vector) == 0 {
				return nil, errors.New("embedding response contains an empty vector")
			}
		}
		return vectors, nil
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d for %d texts", len(result.Data), len(texts))
	}
	vectors := make([][]float32, len(texts))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(texts) || len(item.Embedding) == 0 {
			return nil, errors.New("embedding response contains an invalid index or empty vector")
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

func (v *vectorConfig) request(ctx context.Context, method, endpoint string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, v.qdrantURL+endpoint, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("Qdrant request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Qdrant returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if output != nil {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (v *vectorConfig) ensureCollection(ctx context.Context, dimension int) error {
	var existing map[string]any
	err := v.request(ctx, http.MethodGet, "/collections/"+vectorCollection, nil, &existing)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		return err
	}
	var result map[string]any
	return v.request(ctx, http.MethodPut, "/collections/"+vectorCollection, map[string]any{"vectors": map[string]any{"size": dimension, "distance": "Cosine"}}, &result)
}

func (v *vectorConfig) collectionExists(ctx context.Context) (bool, error) {
	var existing map[string]any
	err := v.request(ctx, http.MethodGet, "/collections/"+vectorCollection, nil, &existing)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "HTTP 404") {
		return false, nil
	}
	return false, err
}

func (v *vectorConfig) repoPointIDs(ctx context.Context, repoID string) (map[string]bool, error) {
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
		if err := v.request(ctx, http.MethodPost, "/collections/"+vectorCollection+"/points/scroll", input, &response); err != nil {
			return nil, err
		}
		for _, point := range response.Result.Points {
			var id string
			if err := json.Unmarshal(point.ID, &id); err != nil {
				return nil, fmt.Errorf("decode Qdrant point ID: %w", err)
			}
			ids[id] = true
		}
		if response.Result.NextPageOffset == nil || len(response.Result.Points) == 0 {
			return ids, nil
		}
		offset = response.Result.NextPageOffset
	}
}

func (v *vectorConfig) deletePoints(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var result map[string]any
	return v.request(ctx, http.MethodPost, "/collections/"+vectorCollection+"/points/delete?wait=true", map[string]any{"points": ids}, &result)
}

func (v *vectorConfig) upsert(ctx context.Context, chunks []codeChunk, vectors [][]float32) error {
	points := make([]map[string]any, 0, len(chunks))
	for i, chunk := range chunks {
		points = append(points, map[string]any{"id": chunk.ID, "vector": vectors[i], "payload": chunk})
	}
	var result map[string]any
	return v.request(ctx, http.MethodPut, "/collections/"+vectorCollection+"/points?wait=true", map[string]any{"points": points}, &result)
}

func (v *vectorConfig) search(ctx context.Context, repoID string, vector []float32, limit int) ([]codeChunk, error) {
	var response struct {
		Result []struct {
			Payload codeChunk `json:"payload"`
		} `json:"result"`
	}
	err := v.request(ctx, http.MethodPost, "/collections/"+vectorCollection+"/points/search", map[string]any{
		"vector": vector, "limit": limit, "with_payload": true, "filter": repoFilter(repoID),
	}, &response)
	if err != nil {
		return nil, err
	}
	chunks := make([]codeChunk, 0, len(response.Result))
	for _, hit := range response.Result {
		chunks = append(chunks, hit.Payload)
	}
	return chunks, nil
}

func repoFilter(repoID string) map[string]any {
	return map[string]any{"must": []any{map[string]any{"key": "repo_id", "match": map[string]any{"value": repoID}}}}
}

func (v *vectorConfig) index(ctx context.Context, root string) (int, error) {
	ws := workspace{root: root}
	files, err := ws.walk(root)
	if err != nil {
		return 0, err
	}
	repoID, err := repositoryID(root)
	if err != nil {
		return 0, err
	}
	chunks := make([]codeChunk, 0)
	for _, relative := range files {
		data, err := readBounded(filepath.Join(root, filepath.FromSlash(relative)), maxFileBytes)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for start := 0; start < len(lines); start += 68 {
			end := start + 80
			if end > len(lines) {
				end = len(lines)
			}
			content := strings.Join(lines[start:end], "\n")
			hash := sha256.Sum256([]byte(content))
			chunks = append(chunks, codeChunk{ID: pointID(repoID, relative, start+1, hex.EncodeToString(hash[:])), RepoID: repoID, Path: relative, Start: start + 1, End: end, Content: content, Hash: hex.EncodeToString(hash[:])})
			if end == len(lines) {
				break
			}
		}
	}
	if len(chunks) == 0 {
		return 0, errors.New("no indexable source chunks found")
	}
	// Qwen3.7 embedding endpoints accept at most 20 input rows per request.
	const batchSize = 20
	var firstVector []float32
	activeIDs := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		activeIDs[chunk.ID] = true
	}
	exists, err := v.collectionExists(ctx)
	if err != nil {
		return 0, err
	}
	existingIDs := make(map[string]bool)
	if exists {
		existingIDs, err = v.repoPointIDs(ctx, repoID)
		if err != nil {
			return 0, err
		}
	}
	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batchChunks := make([]codeChunk, 0, end-start)
		texts := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			if existingIDs[chunks[i].ID] {
				continue
			}
			batchChunks = append(batchChunks, chunks[i])
			texts = append(texts, chunks[i].Path+"\n"+chunks[i].Content)
		}
		if len(batchChunks) == 0 {
			continue
		}
		vectors, err := v.embeddings(ctx, texts)
		if err != nil {
			return 0, err
		}
		if firstVector == nil {
			firstVector = vectors[0]
			if err := v.ensureCollection(ctx, len(firstVector)); err != nil {
				return 0, err
			}
		}
		if err := v.upsert(ctx, batchChunks, vectors); err != nil {
			return 0, err
		}
		for _, chunk := range batchChunks {
			existingIDs[chunk.ID] = true
		}
	}
	// Publish additions and updates before removing stale chunks. If embedding or
	// upserting fails, the previous index remains searchable and a retry can finish.
	staleIDs := make([]string, 0)
	for id := range existingIDs {
		if !activeIDs[id] {
			staleIDs = append(staleIDs, id)
		}
	}
	for start := 0; start < len(staleIDs); start += 256 {
		end := start + 256
		if end > len(staleIDs) {
			end = len(staleIDs)
		}
		if err := v.deletePoints(ctx, staleIDs[start:end]); err != nil {
			return 0, err
		}
	}
	return len(chunks), nil
}

func (v *vectorConfig) retrieve(ctx context.Context, root, query string, topK int) ([]codeChunk, error) {
	repoID, err := repositoryID(root)
	if err != nil {
		return nil, err
	}
	queryVectors, err := v.embeddings(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return v.search(ctx, repoID, queryVectors[0], topK*4)
}

func repositoryID(root string) (string, error) {
	command := exec.Command("git", "rev-parse", "--git-common-dir")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		absolute, absErr := filepath.Abs(root)
		if absErr != nil {
			return "", fmt.Errorf("resolve repository identity: %w", absErr)
		}
		return directoryID(absolute), nil
	}
	common := strings.TrimSpace(string(output))
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return "", err
	}
	return directoryID(filepath.Clean(common)), nil
}

func directoryID(directory string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(directory)))
	return hex.EncodeToString(sum[:16])
}

func pointID(repoID, path string, start int, contentHash string) string {
	sum := sha256.Sum256([]byte(repoID + ":" + path + ":" + fmt.Sprint(start) + ":" + contentHash))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:])
}

func (w workspace) retrieveHybrid(ctx context.Context, query string, topK int, vector *vectorConfig) ([]scoredChunk, error) {
	files, err := w.walk(w.root)
	if err != nil {
		return nil, err
	}
	currentChunks := collectChunks(w, files)
	chunks := rankChunks(currentChunks, query)
	vectorChunks, err := vector.retrieve(ctx, w.root, query, topK)
	if err != nil {
		return nil, err
	}
	currentByKey := make(map[string]scoredChunk, len(currentChunks))
	for _, chunk := range currentChunks {
		currentByKey[chunk.path+":"+fmt.Sprint(chunk.start)] = chunk
	}
	// Reciprocal rank fusion combines lexical rank and vector rank without pretending
	// that BM25 scores and cosine similarities share a comparable numeric scale.
	scores := make(map[string]float64, len(chunks)+len(vectorChunks))
	for rank, chunk := range chunks {
		if chunk.score <= 0 {
			continue
		}
		key := chunk.path + ":" + fmt.Sprint(chunk.start)
		scores[key] += 1.0 / float64(60+rank+1)
	}
	for rank, chunk := range vectorChunks {
		key := filepath.ToSlash(chunk.Path) + ":" + fmt.Sprint(chunk.Start)
		current, ok := currentByKey[key]
		if !ok {
			continue
		}
		contentHash := sha256.Sum256([]byte(current.content))
		if hex.EncodeToString(contentHash[:]) != chunk.Hash {
			continue
		}
		scores[key] += 1.0 / float64(60+rank+1)
	}
	byKey := make(map[string]scoredChunk, len(currentChunks))
	for _, chunk := range currentChunks {
		byKey[chunk.path+":"+fmt.Sprint(chunk.start)] = chunk
	}
	results := make([]scoredChunk, 0, len(scores))
	for key, score := range scores {
		if chunk, ok := byKey[key]; ok {
			chunk.score = score
			results = append(results, chunk)
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func qdrantURLValid(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// IndexRepository synchronizes code and reviewed memory indexes for a Git repository.
func IndexRepository(root string) (int, error) {
	codeChunks, _, err := IndexRepositoryWithMemory(root)
	return codeChunks, err
}

// IndexRepositoryMemory synchronizes only the repository's reviewed memory
// entries. It avoids embedding code chunks when only MEMORY.md has changed.
func IndexRepositoryMemory(root string) (int, error) {
	vector := newVectorConfig()
	if vector == nil {
		return 0, errors.New("set SIGMA_AGENT_EMBEDDING_MODEL to enable vector indexing")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return 0, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return vector.indexMemory(ctx, filepath.Clean(absolute))
}

// IndexRepositoryWithMemory returns the number of synchronized code chunks and
// human-reviewed memory entries.
func IndexRepositoryWithMemory(root string) (int, int, error) {
	vector := newVectorConfig()
	if vector == nil {
		return 0, 0, errors.New("set SIGMA_AGENT_EMBEDDING_MODEL to enable vector indexing")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return 0, 0, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	absolute = filepath.Clean(absolute)
	codeChunks, err := vector.index(ctx, absolute)
	if err != nil {
		return 0, 0, err
	}
	memoryEntries, err := vector.indexMemory(ctx, absolute)
	if err != nil {
		return codeChunks, 0, err
	}
	return codeChunks, memoryEntries, nil
}
