package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const sessionHistorySearchToolName = "search_session_history"
const sessionSearchToolName = "search"

type sessionHistorySearchInput struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

func SessionHistorySearchToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        sessionHistorySearchToolName,
		Description: "Search the full original transcript of this session, including messages omitted from the current compacted model context. Use when an earlier fact, exact wording, decision rationale, tool result, or cross-turn relationship is missing or uncertain. Results include source message indices and tool-call IDs; verify exact details from the returned excerpts.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2,"maxLength":2000},"top_k":{"type":"integer","minimum":1,"maximum":5}},"required":["query"],"additionalProperties":false}`),
	}
}

// WithSessionHistorySearchTool keeps the transcript-search schema in a stable
// position and replaces any external tool that happens to use its reserved name.
func WithSessionHistorySearchTool(definitions []ToolDefinition) []ToolDefinition {
	result := append([]ToolDefinition(nil), definitions...)
	for index := range result {
		if result[index].Name == sessionHistorySearchToolName {
			result[index] = SessionHistorySearchToolDefinition()
			return result
		}
	}
	return append(result, SessionHistorySearchToolDefinition())
}

func SessionSearchToolDefinition(scopes []string) ToolDefinition {
	if len(scopes) == 0 {
		scopes = []string{"session_history"}
	}
	encodedScopes, _ := json.Marshal(scopes)
	return ToolDefinition{
		Name:        sessionSearchToolName,
		Description: "Search a selected Sigma index. Choose scope=tools to find and activate optional tool schemas for the next turn; skills for Skill metadata; memory for curated durable repository facts; prior_sessions for older saved transcripts from this repository; or session_history for earlier transcript messages in this conversation. Results are capped by the selected route. This tool searches indexes, not workspace source code; use run_command with rg for that.",
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"scope":{"type":"string","enum":%s},"query":{"type":"string","minLength":2,"maxLength":2000}},"required":["scope","query"],"additionalProperties":false}`, encodedScopes)),
	}
}

func WithSessionSearchTool(definitions []ToolDefinition, scopes []string) []ToolDefinition {
	result := append([]ToolDefinition(nil), definitions...)
	for index := range result {
		if result[index].Name == sessionSearchToolName {
			result[index] = SessionSearchToolDefinition(scopes)
			return result
		}
	}
	return append(result, SessionSearchToolDefinition(scopes))
}

func (s *Session) sessionHistorySearchTool() Tool {
	definition := SessionHistorySearchToolDefinition()
	return Tool{
		Definition:    definition,
		ExecutionMode: "serialized",
		Validate: func(raw json.RawMessage) error {
			var input sessionHistorySearchInput
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return err
			}
			if len([]rune(strings.TrimSpace(input.Query))) < 2 {
				return fmt.Errorf("query must contain at least two characters")
			}
			if input.TopK < 0 || input.TopK > 5 {
				return fmt.Errorf("top_k must be between 1 and 5")
			}
			return nil
		},
		Handle: func(_ context.Context, raw json.RawMessage) (string, error) {
			var input sessionHistorySearchInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", err
			}
			return s.searchSessionHistory(input.Query, input.TopK), nil
		},
	}
}

type sessionSearchInput struct {
	Scope string `json:"scope"`
	Query string `json:"query"`
}

func (s *Session) sessionSearchTool() Tool {
	definition := SessionSearchToolDefinition(s.Tools.SearchScopes())
	return Tool{
		Definition:    definition,
		ExecutionMode: "parallel",
		ReplayPolicy:  ToolReplaySafe,
		Validate: func(raw json.RawMessage) error {
			var input sessionSearchInput
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				return fmt.Errorf("decode search arguments: %w", err)
			}
			if len([]rune(strings.TrimSpace(input.Query))) < 2 {
				return errors.New("query must contain at least two characters")
			}
			if len([]rune(input.Query)) > 2000 {
				return errors.New("query must contain at most 2000 characters")
			}
			for _, scope := range s.Tools.SearchScopes() {
				if input.Scope == scope {
					return nil
				}
			}
			return fmt.Errorf("scope %q is not available; choose one of %s", input.Scope, strings.Join(s.Tools.SearchScopes(), ", "))
		},
		Handle: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var input sessionSearchInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", err
			}
			switch input.Scope {
			case "tools":
				activated := s.Tools.activateSearch(input.Query, 4)
				if len(activated) == 0 {
					return "No matching tools found. Try a more specific capability or continue with the currently available tools.", nil
				}
				return "Available tools for the next model turn (matching tools are active): " + strings.Join(activated, ", "), nil
			case "session_history":
				return s.searchSessionHistory(input.Query, 3), nil
			case "skills", "memory", "prior_sessions":
				name := map[string]string{"skills": "search_skills", "memory": "search_memory", "prior_sessions": "search_prior_sessions"}[input.Scope]
				route, ok := s.Tools.byName[name]
				if !ok || !route.InternalOnly {
					return "", fmt.Errorf("search scope %q is unavailable", input.Scope)
				}
				arguments, err := json.Marshal(struct {
					Query string `json:"query"`
				}{input.Query})
				if err != nil {
					return "", err
				}
				if route.Validate != nil {
					if err := route.Validate(arguments); err != nil {
						return "", err
					}
				}
				return route.Handle(ctx, arguments)
			default:
				return "", fmt.Errorf("unsupported search scope %q", input.Scope)
			}
		},
	}
}

func sessionSearchPolicyToolName(raw json.RawMessage) string {
	var input sessionSearchInput
	if json.Unmarshal(raw, &input) != nil {
		return sessionSearchToolName
	}
	switch input.Scope {
	case "memory":
		return "search_memory"
	case "skills":
		return "search_skills"
	default:
		return sessionSearchToolName
	}
}

type historyHit struct {
	index int
	score int
	text  string
	ids   []string
}

func (s *Session) searchSessionHistory(query string, topK int) string {
	terms := historyQueryTerms(query)
	if len(terms) == 0 {
		return "No searchable terms in query. Try exact words, file paths, error text, or tool-call IDs."
	}
	if topK <= 0 {
		topK = 3
	}
	if topK > 5 {
		topK = 5
	}
	s.mu.Lock()
	messages := cloneMessages(s.messages)
	sessionID := s.ID
	s.mu.Unlock()

	hits := make([]historyHit, 0)
	for index, message := range messages {
		text, ids := historyMessageText(message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		lower := strings.ToLower(text)
		score := 0
		for _, term := range terms {
			count := strings.Count(lower, strings.ToLower(term))
			if count > 3 {
				count = 3
			}
			score += count
		}
		if score > 0 {
			hits = append(hits, historyHit{index: index, score: score, text: text, ids: ids})
		}
	}
	if len(hits) == 0 {
		return "No transcript messages matched those terms. Retry with a distinctive file path, symbol, error phrase, command, or tool-call ID. The original session remains intact."
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].index > hits[j].index
		}
		return hits[i].score > hits[j].score
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}

	var output strings.Builder
	fmt.Fprintf(&output, "Transcript search results for session %s (lexical ranking; not proof of semantic completeness):\n", sessionID)
	seen := make(map[int]bool)
	for _, hit := range hits {
		if seen[hit.index] {
			continue
		}
		seen[hit.index] = true
		fmt.Fprintf(&output, "\n[matched message_index=%d score=%d] %s\n%s\n", hit.index, hit.score, messages[hit.index].Role, truncateRunes(hit.text, 1800))
		// Nearby messages often contain the request, explanation, or decision
		// that gives this exact tool evidence its meaning. Include one message
		// on either side, while keeping the original indices explicit.
		for _, neighbor := range []int{hit.index - 1, hit.index + 1} {
			if neighbor < 0 || neighbor >= len(messages) || seen[neighbor] {
				continue
			}
			text, _ := historyMessageText(messages[neighbor])
			if strings.TrimSpace(text) == "" {
				continue
			}
			fmt.Fprintf(&output, "[neighbor message_index=%d relative_to=%d] %s\n", neighbor, hit.index, truncateRunes(text, 1000))
			seen[neighbor] = true
		}
		for _, callID := range hit.ids {
			for otherIndex, other := range messages {
				if otherIndex == hit.index || seen[otherIndex] {
					continue
				}
				if messageHasToolCallID(other, callID) {
					text, _ := historyMessageText(other)
					fmt.Fprintf(&output, "[linked message_index=%d tool_call_id=%s] %s\n", otherIndex, callID, truncateRunes(text, 1000))
					seen[otherIndex] = true
				}
			}
		}
	}
	return strings.TrimSpace(output.String())
}

func historyMessageText(message Message) (string, []string) {
	var parts []string
	var ids []string
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_use":
			ids = append(ids, block.ID)
			parts = append(parts, fmt.Sprintf("tool_call id=%s name=%s args=%s", block.ID, block.Name, string(block.Input)))
		case "tool_result":
			ids = append(ids, block.ToolUseID)
			status := "ok"
			if block.IsError {
				status = "error"
			}
			parts = append(parts, fmt.Sprintf("tool_result call_id=%s status=%s %s", block.ToolUseID, status, block.Content))
		}
	}
	return strings.Join(parts, "\n"), ids
}

func messageHasToolCallID(message Message, callID string) bool {
	for _, block := range message.Content {
		if block.Type == "tool_use" && block.ID == callID || block.Type == "tool_result" && block.ToolUseID == callID {
			return true
		}
	}
	return false
}

func historyQueryTerms(query string) []string {
	var terms []string
	var word strings.Builder
	flush := func() {
		if word.Len() >= 2 {
			terms = append(terms, strings.ToLower(word.String()))
		}
		word.Reset()
	}
	var han []rune
	for _, r := range strings.ToLower(query) {
		if unicode.In(r, unicode.Han) {
			flush()
			han = append(han, r)
		} else {
			if len(han) > 0 {
				terms = appendHanBigrams(terms, han)
				han = han[:0]
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/' {
				word.WriteRune(r)
			} else {
				flush()
			}
		}
	}
	flush()
	if len(han) > 0 {
		terms = appendHanBigrams(terms, han)
	}
	seen := make(map[string]bool)
	unique := terms[:0]
	for _, term := range terms {
		if term != "" && !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}
	return unique
}

func appendHanBigrams(terms []string, runes []rune) []string {
	if len(runes) == 1 {
		return append(terms, string(runes))
	}
	for i := 0; i+1 < len(runes); i++ {
		terms = append(terms, string(runes[i:i+2]))
	}
	return terms
}
