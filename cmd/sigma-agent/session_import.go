package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	workspaceagent "github.com/sdaAlbert/sigma-agent/internal/workspace"
)

type piImportEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId"`
	Timestamp json.RawMessage `json:"timestamp"`
	CWD       string          `json:"cwd"`
	Provider  string          `json:"provider"`
	ModelID   string          `json:"modelId"`
	Name      string          `json:"name"`
	Message   json.RawMessage `json:"message"`
}

type piImportMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    bool            `json:"isError"`
}

type piImportBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	Content   json.RawMessage `json:"content"`
	Arguments json.RawMessage `json:"arguments"`
	IsError   bool            `json:"isError"`
}

type importedPiSession struct {
	snapshot         agent.SessionSnapshot
	workspace        string
	startingCommit   string
	gitDiffAvailable bool
}

func readPiSessionJSONL(path string, fallbackWorkspace string) (importedPiSession, error) {
	file, err := os.Open(path)
	if err != nil {
		return importedPiSession{}, fmt.Errorf("open Pi session: %w", err)
	}
	defer file.Close()

	var records []piImportEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		var record piImportEntry
		if err := json.Unmarshal(data, &record); err != nil {
			return importedPiSession{}, fmt.Errorf("decode Pi session line %d: %w", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return importedPiSession{}, fmt.Errorf("read Pi session: %w", err)
	}
	if len(records) == 0 || records[0].Type != "session" {
		return importedPiSession{}, errors.New("file is not a Pi JSONL session (the first record must be a session header)")
	}
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return importedPiSession{}, fmt.Errorf("close Pi session: %w", err)
	}

	header := records[0]
	if !agent.ValidSessionID(header.ID) {
		return importedPiSession{}, fmt.Errorf("Pi session ID %q cannot be represented by Sigma", header.ID)
	}
	workspace := strings.TrimSpace(header.CWD)
	if workspace == "" {
		workspace = fallbackWorkspace
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return importedPiSession{}, fmt.Errorf("resolve imported session folder: %w", err)
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		if err != nil {
			return importedPiSession{}, fmt.Errorf("Pi session folder is unavailable (%s): %w", workspace, err)
		}
		return importedPiSession{}, fmt.Errorf("Pi session folder is not a directory: %s", workspace)
	}

	byID := make(map[string]piImportEntry, len(records))
	lastID := ""
	modelID := ""
	name := ""
	for _, record := range records[1:] {
		if record.ID != "" {
			byID[record.ID] = record
			lastID = record.ID
		}
		if record.Type == "model_change" && record.Provider != "" && record.ModelID != "" {
			modelID = record.Provider + "/" + record.ModelID
		}
		if record.Type == "session_info" && record.Name != "" {
			name = record.Name
		}
	}
	if header.ModelID != "" {
		modelID = header.ModelID
	}
	snapshot := agent.SessionSnapshot{Version: 2, ID: header.ID, Name: name, ModelID: modelID}
	for _, record := range records[1:] {
		if record.ID == "" {
			continue
		}
		entry := agent.SessionEntry{ID: record.ID, ParentID: record.ParentID, Type: "imported_metadata"}
		if entry.ParentID != "" {
			if _, found := byID[entry.ParentID]; !found {
				entry.ParentID = ""
			}
		}
		switch record.Type {
		case "message":
			var source piImportMessage
			if err := json.Unmarshal(record.Message, &source); err != nil {
				return importedPiSession{}, fmt.Errorf("decode Pi message %q: %w", record.ID, err)
			}
			message, keep, convertErr := convertPiMessage(source)
			if convertErr != nil {
				return importedPiSession{}, fmt.Errorf("convert Pi message %q: %w", record.ID, convertErr)
			}
			if keep {
				message.ID = record.ID
				entry.Type = "message"
				entry.Message = &message
			}
		case "model_change":
			entry.Type = "model_change"
			if record.Provider != "" && record.ModelID != "" {
				entry.ModelID = record.Provider + "/" + record.ModelID
			}
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	snapshot.Head = lastID

	// Import the active branch only. Walking parent IDs avoids mixing abandoned
	// branches into the conversation that Sigma resumes.
	var branch []piImportEntry
	seen := make(map[string]bool)
	for id := lastID; id != ""; {
		if seen[id] {
			return importedPiSession{}, errors.New("Pi session contains a cyclic parent chain")
		}
		seen[id] = true
		record, exists := byID[id]
		if !exists {
			return importedPiSession{}, fmt.Errorf("Pi session history is missing parent entry %q", id)
		}
		branch = append(branch, record)
		id = record.ParentID
	}
	for left, right := 0, len(branch)-1; left < right; left, right = left+1, right-1 {
		branch[left], branch[right] = branch[right], branch[left]
	}

	for _, record := range branch {
		if record.Type != "message" || len(record.Message) == 0 {
			continue
		}
		var source piImportMessage
		if err := json.Unmarshal(record.Message, &source); err != nil {
			return importedPiSession{}, fmt.Errorf("decode Pi message %q: %w", record.ID, err)
		}
		message, keep, convertErr := convertPiMessage(source)
		if convertErr != nil {
			return importedPiSession{}, fmt.Errorf("convert Pi message %q: %w", record.ID, convertErr)
		}
		if keep {
			message.ID = record.ID
			snapshot.Messages = append(snapshot.Messages, message)
		}
	}
	if len(snapshot.Messages) == 0 {
		return importedPiSession{}, errors.New("Pi session has no importable conversation messages")
	}

	commit := ""
	gitAvailable := workspaceagent.HasGitCommit(workspace)
	if gitAvailable {
		if result, gitErr := gitOutput(workspace, "rev-parse", "HEAD"); gitErr == nil {
			commit = strings.TrimSpace(result)
		} else {
			gitAvailable = false
		}
	}
	return importedPiSession{snapshot: snapshot, workspace: workspace, startingCommit: commit, gitDiffAvailable: gitAvailable}, nil
}

func convertPiMessage(source piImportMessage) (agent.Message, bool, error) {
	role := source.Role
	switch role {
	case "user", "assistant":
	case "toolResult":
		role = "user"
	case "system":
		// The current Sigma system prompt is regenerated from the imported
		// workspace, so Pi's system prompt must not become user conversation.
		return agent.Message{}, false, nil
	default:
		return agent.Message{}, false, nil
	}

	message := agent.Message{Role: role}
	if source.Role == "toolResult" {
		content, err := piContentText(source.Content)
		if err != nil {
			return agent.Message{}, false, err
		}
		message.Content = []agent.ContentBlock{{Type: "tool_result", ToolUseID: source.ToolCallID, Name: source.ToolName, Content: content, IsError: source.IsError}}
		return message, true, nil
	}
	var sourceBlocks []json.RawMessage
	if err := json.Unmarshal(source.Content, &sourceBlocks); err != nil {
		return agent.Message{}, false, fmt.Errorf("decode content blocks: %w", err)
	}
	for _, raw := range sourceBlocks {
		var block piImportBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return agent.Message{}, false, err
		}
		switch block.Type {
		case "text":
			message.Content = append(message.Content, agent.ContentBlock{Type: "text", Text: block.Text})
		case "toolCall":
			input := bytes.TrimSpace(block.Arguments)
			if len(input) == 0 || !json.Valid(input) {
				input = json.RawMessage(`{}`)
			}
			message.Content = append(message.Content, agent.ContentBlock{Type: "tool_use", ID: block.ID, Name: block.Name, Input: append(json.RawMessage(nil), input...)})
		case "image":
			message.Content = append(message.Content, agent.ContentBlock{Type: "text", Text: "[image content omitted during session import]"})
		}
	}
	return message, len(message.Content) > 0, nil
}

func piContentText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("unsupported tool result content: %w", err)
	}
	var output strings.Builder
	for _, rawBlock := range blocks {
		var block piImportBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return "", err
		}
		if block.Type == "text" {
			output.WriteString(block.Text)
		} else if block.Type == "image" {
			output.WriteString("[image content omitted during session import]")
		}
	}
	return output.String(), nil
}

func saveImportedPiSession(imported importedPiSession, sessionRoot string) error {
	store, err := agent.NewFileSessionStore(filepath.Join(sessionRoot, "var", "sessions"))
	if err != nil {
		return err
	}
	if err := store.Save(imported.snapshot); err != nil {
		return fmt.Errorf("save imported session snapshot: %w", err)
	}
	sessionDir := filepath.Join(store.Directory, imported.snapshot.ID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return fmt.Errorf("create imported session metadata folder: %w", err)
	}
	metadata, err := json.MarshalIndent(map[string]any{
		"repo": imported.workspace, "starting_commit": imported.startingCommit,
		"git_diff_available": imported.gitDiffAvailable, "workspace_mode": "direct",
	}, "", "  ")
	if err != nil {
		return err
	}
	metadataPath := filepath.Join(sessionDir, "metadata.json")
	if err := os.WriteFile(metadataPath, metadata, 0o600); err != nil {
		return fmt.Errorf("write imported session metadata: %w", err)
	}
	return nil
}

func parseSessionImportPath(argument string) (string, error) {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return "", errors.New("usage: /import <pi-session.jsonl>")
	}
	if strings.HasPrefix(argument, "\"") {
		path, err := strconv.Unquote(argument)
		if err != nil {
			return "", fmt.Errorf("invalid quoted import path: %w", err)
		}
		return path, nil
	}
	if strings.HasPrefix(argument, "'") && strings.HasSuffix(argument, "'") && len(argument) >= 2 {
		return argument[1 : len(argument)-1], nil
	}
	return argument, nil
}
