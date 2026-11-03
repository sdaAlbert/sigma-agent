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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// vectorConfig is used only by repository long-term memory. Source files are
// searched with the workspace tools and are never embedded or stored in Qdrant.
type vectorConfig struct {
	qdrantURL string
	embedURL  string
	model     string
	apiKey    string
	provider  string
	client    *http.Client
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

func repoFilter(repoID string) map[string]any {
	return map[string]any{"must": []any{map[string]any{"key": "repo_id", "match": map[string]any{"value": repoID}}}}
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

// IndexRepositoryMemory synchronizes repository facts into Qdrant.
// The Markdown file remains the source of truth; source code is never indexed.
func IndexRepositoryMemory(root string) (int, error) {
	vector := newVectorConfig()
	if vector == nil {
		return 0, errors.New("set SIGMA_AGENT_EMBEDDING_MODEL to enable memory vector indexing")
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
