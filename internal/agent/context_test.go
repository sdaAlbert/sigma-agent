package agent

import (
	"encoding/json"
	"fmt"
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
	if !strings.Contains(prepared.System, "Fix the parser regression.") {
		t.Fatal("compaction dropped the user's request")
	}
	if !strings.Contains(prepared.Messages[2].Content[0].Content, "compacted") {
		t.Fatal("tool result should contain a compaction marker")
	}
}

func TestWindowContextManagerCompactsLongToolLoopAtCompletedCallBoundary(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "Fix the migration serialization bug."}}},
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("call-%d", i)
		messages = append(messages,
			Message{Role: "assistant", Content: []ContentBlock{{
				Type: "tool_use", ID: id, Name: "read_file",
				Input: json.RawMessage(fmt.Sprintf(`{"path":"file-%d.py","context":"%s"}`, i, strings.Repeat("inspect-this-", 12))),
			}}},
			Message{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: id, Content: "file contents inspected"}}},
		)
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
	if !strings.Contains(prepared.System, "Fix the migration serialization bug.") {
		t.Fatal("compaction dropped the original user request from its summary")
	}
	if strings.Contains(prepared.System, "call-39") || !containsToolUse(prepared.Messages, "call-39") {
		t.Fatal("compaction should retain the latest completed tool call")
	}
	if containsToolUse(prepared.Messages, "call-0") {
		t.Fatal("compaction should drop older completed tool calls")
	}
}

func TestWindowContextManagerKeepsEscapedSummaryWithinBudget(t *testing.T) {
	messages := []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: strings.Repeat("<", 1800)}}}}
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("escaped-%d", i)
		messages = append(messages,
			Message{Role: "assistant", Content: []ContentBlock{{Type: "tool_use", ID: id, Name: "read_file", Input: json.RawMessage(`{"path":"django/utils/http.py"}`)}}},
			Message{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: id, Content: "source read"}}},
		)
	}

	const limit = 1200
	prepared, err := (WindowContextManager{MaxTokens: limit, SummaryTokens: 100}).Prepare("You are a coding agent.", messages)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.EstimatedAfter > limit {
		t.Fatalf("estimated context = %d, want <= %d", prepared.EstimatedAfter, limit)
	}
}

func containsToolUse(messages []Message, id string) bool {
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Type == "tool_use" && block.ID == id {
				return true
			}
		}
	}
	return false
}
