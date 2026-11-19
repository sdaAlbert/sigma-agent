package agent

import (
	"context"
	"errors"
)

// compactAutomatically summarizes only the old prefix when the configured soft
// context limit is crossed. The raw suffix and canonical transcript stay intact.
func (s *Session) compactAutomatically(ctx context.Context, turn int, window WindowContextManager) (*contextCompaction, int, error) {
	if s.Model == nil {
		return nil, 0, nil
	}
	threshold := window.CompactionThresholdTokens
	if threshold <= 0 || threshold > window.MaxTokens {
		threshold = window.MaxTokens
		if threshold <= 0 || threshold >= 30000 {
			threshold = 30000
		}
	}
	summaryBudget := window.SummaryTokens
	if summaryBudget <= 0 {
		summaryBudget = 800
	}
	evidenceBudget := contextEvidenceTokens

	system, messages := s.modelContext(s.System)
	s.mu.Lock()
	if len(s.pendingTools) > 0 {
		s.mu.Unlock()
		return nil, 0, errors.New("automatic context compaction cannot run with pending tool calls")
	}
	baseBoundary := s.compactionBoundary
	if baseBoundary < 0 || baseBoundary > len(s.messages) {
		baseBoundary = 0
	}
	previousSummary := s.compactionSummary
	previousEvidence := s.compactionEvidence
	systemWithoutCheckpoint := s.System
	s.mu.Unlock()

	toolTokens := max(0, window.ToolTokens)
	before := estimateTextTokens(system) + estimateMessageTokens(messages) + toolTokens
	if before <= threshold || len(messages) < 2 {
		return nil, before, nil
	}
	// Keep the largest recent suffix that leaves room for the base prompt, model
	// checkpoint, evidence index, and a small serialization margin.
	recentBudget := threshold - estimateTextTokens(systemWithoutCheckpoint) - toolTokens - summaryBudget - evidenceBudget - 256
	if recentBudget < 128 {
		return nil, before, nil
	}
	keepRecent := window.KeepRecentTokens
	if keepRecent <= 0 {
		keepRecent = 20000
	}
	if keepRecent < recentBudget {
		recentBudget = keepRecent
	}
	cut, ok := selectRecentSuffixBoundary(messages, recentBudget)
	if !ok || cut <= 0 {
		return nil, before, nil
	}
	oldPrefix := messages[:cut]
	compaction, err := s.generateContextSummary(ctx, turn, oldPrefix, baseBoundary, previousSummary, previousEvidence, "", "automatic_context_compaction", summaryBudget, cut, s.Model, s.Context)
	if err != nil {
		return nil, before, err
	}
	newBoundary := baseBoundary + cut
	if err := s.persistAutomaticCompaction(compaction.Summary, compaction.Evidence, newBoundary); err != nil {
		return nil, before, err
	}
	compaction.EstimatedTokensBefore = before
	return &compaction, before, nil
}

func selectRecentSuffixBoundary(messages []Message, tokenBudget int) (int, bool) {
	if tokenBudget <= 0 || len(messages) < 2 {
		return 0, false
	}
	merged := mergeAdjacentMessages(messages)
	// The merge is only for finding an efficient message boundary. Recover the
	// corresponding index in the original slice by walking the merged lengths.
	originalStart := 0
	for _, candidate := range merged {
		if originalStart > 0 && isConversationBoundary(candidate.Role) && validTranscriptSuffix(messages[originalStart:]) {
			if estimateMessageTokens(messages[originalStart:]) <= tokenBudget {
				return originalStart, true
			}
		}
		originalStart += countMergedSourceMessages(messages[originalStart:], candidate.Role)
	}
	return 0, false
}

func countMergedSourceMessages(messages []Message, role string) int {
	count := 0
	for _, message := range messages {
		if message.Role != role {
			break
		}
		count++
	}
	if count == 0 {
		return 1
	}
	return count
}
