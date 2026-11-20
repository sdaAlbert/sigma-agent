package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const priorSessionSearchToolName = "search_prior_sessions"

type priorSessionSearchInput struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

type priorSessionHit struct {
	sessionID string
	updatedAt string
	message   int
	role      string
	text      string
	score     float64
}

type priorSessionDocument struct {
	sessionID string
	updatedAt string
	message   int
	role      string
	text      string
	terms     []string
	frequency map[string]int
}

// PriorSessionSearchTool exposes saved transcripts from the same repository
// through the shared search router. It is deliberately separate from curated
// project memory and only reads sessions with matching repository metadata.
func PriorSessionSearchTool(store *FileSessionStore, repository, currentSessionID string) Tool {
	return Tool{
		Definition: ToolDefinition{
			Name:        priorSessionSearchToolName,
			Description: "Search older saved conversation transcripts for this exact repository. Use for fine-grained facts, decisions, one-off details, or prior task history that may not have entered curated project memory. Results are keyword-ranked excerpts with source session IDs, dates, and message indices; this is not semantic vector search.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2,"maxLength":2000},"top_k":{"type":"integer","minimum":1,"maximum":5}},"required":["query"],"additionalProperties":false}`),
		},
		InternalOnly:  true,
		ExecutionMode: "parallel",
		ReplayPolicy:  ToolReplaySafe,
		Validate: func(raw json.RawMessage) error {
			var input priorSessionSearchInput
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return err
			}
			if len([]rune(strings.TrimSpace(input.Query))) < 2 || len([]rune(input.Query)) > 2000 {
				return errors.New("query must contain 2 to 2000 characters")
			}
			if input.TopK < 0 || input.TopK > 5 {
				return errors.New("top_k must be between 1 and 5")
			}
			return nil
		},
		Handle: func(_ context.Context, raw json.RawMessage) (string, error) {
			if store == nil {
				return "", errors.New("saved session search is unavailable")
			}
			var input priorSessionSearchInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", err
			}
			return store.SearchPriorSessions(repository, currentSessionID, input.Query, input.TopK), nil
		},
	}
}

// SearchPriorSessions scans saved session snapshots belonging to repository,
// ranks matching messages with BM25, and returns a small set of cited excerpts.
// Sessions without repository metadata are excluded rather than searched
// globally, which prevents unrelated projects from leaking into this scope.
func (s *FileSessionStore) SearchPriorSessions(repository, currentSessionID, query string, topK int) string {
	queryTerms := historyQueryTerms(query)
	if len(queryTerms) == 0 {
		return "No searchable terms in query. Try a distinctive phrase, file path, symbol, error text, or tool-call ID."
	}
	if topK <= 0 {
		topK = 3
	}
	if topK > 5 {
		topK = 5
	}

	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		return "Saved session search is unavailable: " + err.Error()
	}
	documents := make([]priorSessionDocument, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		if !ValidSessionID(sessionID) || sessionID == currentSessionID || !s.sessionBelongsToRepository(sessionID, repository) {
			continue
		}
		snapshot, loadErr := s.Load(sessionID)
		if loadErr != nil {
			continue
		}
		for index, message := range snapshot.Messages {
			text, _ := historyMessageText(message)
			if strings.TrimSpace(text) == "" {
				continue
			}
			terms := historyQueryTerms(text)
			frequency := make(map[string]int, len(terms))
			for _, term := range terms {
				frequency[term]++
			}
			documents = append(documents, priorSessionDocument{
				sessionID: sessionID, updatedAt: snapshot.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
				message: index, role: message.Role, text: text, terms: terms, frequency: frequency,
			})
		}
	}
	if len(documents) == 0 {
		return "No saved prior sessions were found for this repository."
	}

	documentFrequency := make(map[string]int)
	totalLength := 0
	for _, document := range documents {
		totalLength += len(document.terms)
		for term := range document.frequency {
			documentFrequency[term]++
		}
	}
	averageLength := float64(totalLength) / float64(len(documents))
	if averageLength == 0 {
		return "No searchable text was found in saved prior sessions for this repository."
	}

	hits := make([]priorSessionHit, 0)
	for _, document := range documents {
		length := float64(len(document.terms))
		score := 0.0
		for _, term := range queryTerms {
			tf := float64(document.frequency[term])
			if tf == 0 {
				continue
			}
			df := float64(documentFrequency[term])
			idf := math.Log(1 + (float64(len(documents))-df+0.5)/(df+0.5))
			score += idf * (tf * 2.2) / (tf + 1.2*(0.25+0.75*length/averageLength))
		}
		if score > 0 {
			hits = append(hits, priorSessionHit{
				sessionID: document.sessionID, updatedAt: document.updatedAt,
				message: document.message, role: document.role, text: document.text, score: score,
			})
		}
	}
	if len(hits) == 0 {
		return "No prior-session messages matched those terms. Try a distinctive phrase, file path, symbol, error text, or tool-call ID."
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].updatedAt != hits[j].updatedAt {
			return hits[i].updatedAt > hits[j].updatedAt
		}
		if hits[i].sessionID != hits[j].sessionID {
			return hits[i].sessionID < hits[j].sessionID
		}
		return hits[i].message > hits[j].message
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Prior-session transcript results for this repository (BM25 lexical ranking; verify against the cited session):\n")
	for _, hit := range hits {
		fmt.Fprintf(&output, "\n[session_id=%s updated_at=%s message_index=%d score=%.4f role=%s]\n%s\n", hit.sessionID, hit.updatedAt, hit.message, hit.score, hit.role, truncateRunes(hit.text, 1800))
	}
	return strings.TrimSpace(output.String())
}

func (s *FileSessionStore) sessionBelongsToRepository(sessionID, repository string) bool {
	metadataPath := filepath.Join(s.Directory, sessionID, "metadata.json")
	info, err := os.Lstat(metadataPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return false
	}
	var metadata struct {
		Repo string `json:"repo"`
	}
	if json.Unmarshal(data, &metadata) != nil || strings.TrimSpace(metadata.Repo) == "" || strings.TrimSpace(repository) == "" {
		return false
	}
	saved := filepath.Clean(metadata.Repo)
	current := filepath.Clean(repository)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(saved, current)
	}
	return saved == current
}
