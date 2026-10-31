package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxExternalToolOutputBytes = 32 << 20
	maxToolOutputSessionBytes  = 128 << 20
	maxToolOutputReadBytes     = 12 << 10
)

var toolOutputIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type ToolOutputRef struct {
	ID        string
	SizeBytes int
	SHA256    string
}

// ToolOutputStore keeps large tool results outside the transcript and exposes
// bounded range reads through a tool registered for the same session.
type ToolOutputStore interface {
	Save(output string) (ToolOutputRef, error)
	ReadRange(id string, offset int64, maxBytes int) (content string, nextOffset int64, size int64, err error)
}

type FileToolOutputStore struct {
	directory string
	mu        sync.Mutex
}

func NewFileToolOutputStore(directory string) (*FileToolOutputStore, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create tool output directory: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("restrict tool output directory permissions: %w", err)
	}
	return &FileToolOutputStore{directory: absolute}, nil
}

func (s *FileToolOutputStore) Save(output string) (ToolOutputRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := []byte(output)
	if len(data) > maxExternalToolOutputBytes {
		return ToolOutputRef{}, fmt.Errorf("tool output is %d bytes; artifact limit is %d", len(data), maxExternalToolOutputBytes)
	}
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return ToolOutputRef{}, fmt.Errorf("inspect tool output quota: %w", err)
	}
	var used int64
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".txt" || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return ToolOutputRef{}, fmt.Errorf("inspect tool output artifact: %w", err)
		}
		used += info.Size()
	}
	if used+int64(len(data)) > maxToolOutputSessionBytes {
		return ToolOutputRef{}, fmt.Errorf("session tool output quota of %d bytes would be exceeded", maxToolOutputSessionBytes)
	}
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return ToolOutputRef{}, fmt.Errorf("generate tool output ID: %w", err)
	}
	id := hex.EncodeToString(randomID[:])
	temporary, err := os.CreateTemp(s.directory, id+"-*.tmp")
	if err != nil {
		return ToolOutputRef{}, fmt.Errorf("create tool output artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return ToolOutputRef{}, err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		cleanup()
		return ToolOutputRef{}, fmt.Errorf("write tool output artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return ToolOutputRef{}, fmt.Errorf("sync tool output artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return ToolOutputRef{}, fmt.Errorf("close tool output artifact: %w", err)
	}
	path := filepath.Join(s.directory, id+".txt")
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return ToolOutputRef{}, fmt.Errorf("publish tool output artifact: %w", err)
	}
	sum := sha256.Sum256(data)
	return ToolOutputRef{ID: id, SizeBytes: len(data), SHA256: hex.EncodeToString(sum[:])}, nil
}

func (s *FileToolOutputStore) ReadRange(id string, offset int64, maxBytes int) (string, int64, int64, error) {
	if !toolOutputIDPattern.MatchString(id) {
		return "", 0, 0, errors.New("invalid tool output artifact ID")
	}
	if maxBytes < 1 || maxBytes > maxToolOutputReadBytes {
		return "", 0, 0, fmt.Errorf("max_bytes must be from 1 to %d", maxToolOutputReadBytes)
	}
	path := filepath.Join(s.directory, id+".txt")
	file, err := os.Open(path)
	if err != nil {
		return "", 0, 0, fmt.Errorf("open tool output artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, 0, fmt.Errorf("stat tool output artifact: %w", err)
	}
	size := info.Size()
	if offset < 0 || offset > size {
		return "", 0, size, fmt.Errorf("offset must be between 0 and %d", size)
	}
	if offset > 0 && offset < size {
		var one [1]byte
		for offset < size {
			if _, err := file.ReadAt(one[:], offset); err != nil {
				return "", 0, size, err
			}
			if one[0]&0xC0 != 0x80 {
				break
			}
			offset++
		}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", 0, size, err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes+utf8.UTFMax)))
	if err != nil {
		return "", 0, size, fmt.Errorf("read tool output artifact: %w", err)
	}
	length := len(data)
	if length > maxBytes {
		length = maxBytes
		for length > 0 && !utf8.Valid(data[:length]) {
			length--
		}
	}
	content := strings.ToValidUTF8(string(data[:length]), "�")
	nextOffset := offset + int64(length)
	return content, nextOffset, size, nil
}

func ReadToolOutputTool(store ToolOutputStore, inlineLimit int) Tool {
	readLimit := inlineLimit - 512
	if readLimit > maxToolOutputReadBytes {
		readLimit = maxToolOutputReadBytes
	}
	if readLimit < 256 {
		readLimit = 256
	}
	return Tool{Definition: ToolDefinition{
		Name:        "read_tool_output",
		Description: fmt.Sprintf("Read a bounded byte range from a large tool result saved as a session artifact. Use the artifact_id returned in the tool result; continue from next_offset to read later sections. Each content chunk is at most %d bytes.", readLimit),
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"artifact_id":{"type":"string","pattern":"^[a-f0-9]{32}$"},"offset":{"type":"integer","minimum":0},"max_bytes":{"type":"integer","minimum":1,"maximum":%d}},"required":["artifact_id"],"additionalProperties":false}`, readLimit)),
	}, ExecutionMode: "parallel", Handle: func(_ context.Context, raw json.RawMessage) (string, error) {
		var args struct {
			ArtifactID string `json:"artifact_id"`
			Offset     int64  `json:"offset"`
			MaxBytes   int    `json:"max_bytes"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("decode tool output read arguments: %w", err)
		}
		if args.Offset < 0 {
			return "", errors.New("offset must not be negative")
		}
		if args.MaxBytes == 0 {
			args.MaxBytes = readLimit
		}
		content, nextOffset, size, err := store.ReadRange(args.ArtifactID, args.Offset, args.MaxBytes)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("artifact_id=%s | requested_offset=%d | next_offset=%d | size_bytes=%d\n%s", args.ArtifactID, args.Offset, nextOffset, size, content), nil
	}}
}

func externalizedToolOutput(ref ToolOutputRef, output string, inlineLimit int) string {
	previewBytes := inlineLimit / 8
	if previewBytes > 1024 {
		previewBytes = 1024
	}
	if previewBytes < 128 {
		previewBytes = 128
	}
	data := []byte(output)
	headEnd := utf8PrefixLength(data, previewBytes)
	tailStart := len(data) - previewBytes
	for tailStart < len(data) && !utf8.RuneStart(data[tailStart]) {
		tailStart++
	}
	preview := string(data[:headEnd])
	if tailStart > headEnd {
		preview += "\n[… middle stored in artifact …]\n" + string(data[tailStart:])
	}
	return fmt.Sprintf("Large tool result saved to session artifact. artifact_id=%s | size_bytes=%d | sha256=%s. Use read_tool_output with this artifact_id and offset to inspect the complete result.\n--- preview ---\n%s", ref.ID, ref.SizeBytes, ref.SHA256, preview)
}

func utf8PrefixLength(data []byte, limit int) int {
	if len(data) < limit {
		limit = len(data)
	}
	for limit > 0 && !utf8.Valid(data[:limit]) {
		limit--
	}
	return limit
}

func truncateToolOutput(output string, limit int) string {
	if limit < 1 {
		return "[tool output omitted]"
	}
	data := []byte(output)
	if len(data) <= limit {
		return output
	}
	limit = utf8PrefixLength(data, limit)
	return string(data[:limit]) + "\n[tool output truncated]"
}
