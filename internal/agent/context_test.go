package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWindowContextManagerCompactsOversizedToolResult(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "Fix the parser regression."}}},
		{Role: "assistant", Content: []ContentBlock{{Type: "tool_use", ID: "call-1", Name: "read_file", Input: json.RawMessage(`{"path":"parser.go"}`)}}},
		{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "call-1", Content: strings.Repeat("source line with useful context\n", 2000)}}},
	}

	prepared, err := (WindowContextManager{MaxTokens: 1200, SummaryTokens: 100}).Prepare("You are a coding agent.", messages)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.EstimatedAfter > 1200 {
		t.Fatalf("estimated context = %d, want <= 1200", prepared.EstimatedAfter)
	}
	if !validTranscriptSuffix(prepared.Messages) {
		t.Fatal("compacted transcript lost tool call/result pairing")
	}
	if !strings.Contains(prepared.Messages[0].Content[0].Text, "Fix the parser regression.") {
		t.Fatal("compaction dropped the user's request")
	}
	if !strings.Contains(prepared.Messages[2].Content[0].Content, "compacted") {
		t.Fatal("tool result should contain a compaction marker")
	}
}
