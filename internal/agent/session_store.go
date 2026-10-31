package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/fsx"
)

const sessionSchemaVersion = 2
const legacySessionSchemaVersion = 1

var validSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidSessionID(id string) bool { return validSessionID.MatchString(id) }

// SessionSnapshot is the durable boundary between a running process and a later resume.
// PendingTools are persisted before any handler runs so recovery never repeats an
// operation whose outcome may be unknown.
type SessionSnapshot struct {
	Version      int            `json:"version"`
	ID           string         `json:"id"`
	System       string         `json:"system_prompt"`
	Messages     []Message      `json:"messages"`
	Steering     []Message      `json:"steering_queue,omitempty"`
	FollowUps    []Message      `json:"follow_up_queue,omitempty"`
	PendingTools []ContentBlock `json:"pending_tools,omitempty"`
	Entries      []SessionEntry `json:"entries,omitempty"`
	Head         string         `json:"head,omitempty"`
	ModelID      string         `json:"model_id,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// SessionEntry is one immutable point in a session tree. Message and model-change
// entries share the same parent chain so a branch captures both conversation and
// provider history.
type SessionEntry struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Type      string    `json:"type"`
	Message   *Message  `json:"message,omitempty"`
	ModelID   string    `json:"model_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionStore interface {
	Load(id string) (SessionSnapshot, error)
	Save(snapshot SessionSnapshot) error
}

type FileSessionStore struct {
	Directory string
	mu        sync.Mutex
}

func NewFileSessionStore(directory string) (*FileSessionStore, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("restrict session directory permissions: %w", err)
	}
	return &FileSessionStore{Directory: abs}, nil
}

func (s *FileSessionStore) Load(id string) (SessionSnapshot, error) {
	if !ValidSessionID(id) {
		return SessionSnapshot{}, errors.New("invalid session id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Directory, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionSnapshot{}, err
	}
	var snapshot SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SessionSnapshot{}, fmt.Errorf("decode session snapshot: %w", err)
	}
	if (snapshot.Version != sessionSchemaVersion && snapshot.Version != legacySessionSchemaVersion) || snapshot.ID != id {
		return SessionSnapshot{}, fmt.Errorf("unsupported or mismatched session snapshot")
	}
	return snapshot, nil
}

func (s *FileSessionStore) Save(snapshot SessionSnapshot) error {
	if !ValidSessionID(snapshot.ID) {
		return errors.New("invalid session id")
	}
	snapshot.Version = sessionSchemaVersion
	snapshot.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session snapshot: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	temporary, err := os.CreateTemp(s.Directory, snapshot.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create session snapshot temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write session snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("sync session snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close session snapshot: %w", err)
	}
	if err := fsx.ReplaceFile(temporaryPath, filepath.Join(s.Directory, snapshot.ID+".json")); err != nil {
		cleanup()
		return fmt.Errorf("publish session snapshot: %w", err)
	}
	return nil
}

// RestoreSession reconnects runtime dependencies to a persisted conversation. An
// interrupted in-flight tool is converted into an explicit error result: the code
// intentionally avoids replaying an operation that may already have changed state.
func RestoreSession(snapshot SessionSnapshot, model Model, tools *ToolRegistry, store SessionStore) (*Session, error) {
	if (snapshot.Version != sessionSchemaVersion && snapshot.Version != legacySessionSchemaVersion) || !ValidSessionID(snapshot.ID) {
		return nil, errors.New("invalid session snapshot")
	}
	session := &Session{
		ID: snapshot.ID, System: snapshot.System, Model: model, Tools: tools, Store: store,
		messages: cloneMessages(snapshot.Messages), steering: cloneMessages(snapshot.Steering),
		followUps: cloneMessages(snapshot.FollowUps), pendingTools: append([]ContentBlock(nil), snapshot.PendingTools...),
		entries: cloneSessionEntries(snapshot.Entries), head: snapshot.Head, modelID: snapshot.ModelID,
	}
	if snapshot.Version == legacySessionSchemaVersion || len(session.entries) == 0 && len(session.messages) > 0 {
		session.entries = nil
		session.head = ""
		for _, message := range session.messages {
			session.appendEntryLocked(SessionEntry{Type: "message", Message: messagePointer(message)})
		}
	}
	if len(session.entries) > 0 {
		messages, err := session.messagesAtHead(session.head)
		if err != nil {
			return nil, err
		}
		session.messages = messages
	}
	needsSave := snapshot.Version != sessionSchemaVersion
	if len(session.pendingTools) > 0 {
		results := make([]ContentBlock, 0, len(session.pendingTools))
		for _, call := range session.pendingTools {
			results = append(results, toolResult(call.ID, "Previous process stopped while this tool was running. Its side-effect status is unknown; inspect the workspace before deciding whether to retry.", true))
		}
		session.appendMessagesLocked([]Message{{Role: "user", Content: results}})
		session.pendingTools = nil
		needsSave = true
	}
	if needsSave {
		if err := session.persistLocked(); err != nil {
			return nil, fmt.Errorf("save recovered session: %w", err)
		}
	}
	return session, nil
}

func (s *Session) snapshotLocked() SessionSnapshot {
	return SessionSnapshot{
		Version: sessionSchemaVersion, ID: s.ID, System: s.System,
		Messages: cloneMessages(s.messages), Steering: cloneMessages(s.steering),
		FollowUps: cloneMessages(s.followUps), PendingTools: append([]ContentBlock(nil), s.pendingTools...),
		Entries: cloneSessionEntries(s.entries), Head: s.head, ModelID: s.modelID,
		UpdatedAt: time.Now().UTC(),
	}
}

func (s *Session) appendMessagesLocked(messages []Message) {
	for _, message := range messages {
		s.messages = append(s.messages, message)
		s.appendEntryLocked(SessionEntry{Type: "message", Message: messagePointer(message)})
	}
}

func (s *Session) appendEntryLocked(entry SessionEntry) {
	entry.ID = fmt.Sprintf("entry-%d", len(s.entries)+1)
	entry.ParentID = s.head
	entry.CreatedAt = time.Now().UTC()
	s.entries = append(s.entries, entry)
	s.head = entry.ID
}

func (s *Session) messagesAtHead(head string) ([]Message, error) {
	byID := make(map[string]SessionEntry, len(s.entries))
	for _, entry := range s.entries {
		if entry.ID == "" || byID[entry.ID].ID != "" {
			return nil, errors.New("session tree contains an empty or duplicate entry id")
		}
		byID[entry.ID] = entry
	}
	var chain []SessionEntry
	seen := make(map[string]bool)
	for current := head; current != ""; {
		entry, ok := byID[current]
		if !ok || seen[current] {
			return nil, errors.New("session tree head has a missing parent or cycle")
		}
		seen[current] = true
		chain = append(chain, entry)
		current = entry.ParentID
	}
	messages := make([]Message, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].Type == "message" && chain[i].Message != nil {
			messages = append(messages, *chain[i].Message)
		}
	}
	return cloneMessages(messages), nil
}

func messagePointer(message Message) *Message {
	cloned := cloneMessages([]Message{message})[0]
	return &cloned
}

func cloneSessionEntries(entries []SessionEntry) []SessionEntry {
	cloned := make([]SessionEntry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		if entry.Message != nil {
			cloned[i].Message = messagePointer(*entry.Message)
		}
	}
	return cloned
}

func (s *Session) persistLocked() error {
	if s.Store == nil {
		return nil
	}
	return s.Store.Save(s.snapshotLocked())
}

// SetModel records provider/model changes in the session tree. The runtime only
// changes models at a settled turn boundary, so switching is rejected mid-run.
func (s *Session) SetModel(model Model) error {
	if model == nil || strings.TrimSpace(model.ModelID()) == "" {
		return errors.New("session model must have an id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("cannot change model while the session is running")
	}
	modelID := model.ModelID()
	if s.modelID == modelID {
		s.Model = model
		return nil
	}
	previousModel, previousModelID, previousHeadID := s.Model, s.modelID, s.head
	s.Model, s.modelID = model, modelID
	s.appendEntryLocked(SessionEntry{Type: "model_change", ModelID: modelID})
	if err := s.persistLocked(); err != nil {
		s.entries = s.entries[:len(s.entries)-1]
		s.head = previousHeadID
		s.Model, s.modelID = previousModel, previousModelID
		return fmt.Errorf("persist model change: %w", err)
	}
	return nil
}

// BranchFrom moves the active head to an existing tree entry. All prior entries
// remain available; subsequent prompts create a new child branch.
func (s *Session) BranchFrom(entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("cannot branch while the session is running")
	}
	if len(s.pendingTools) > 0 {
		return errors.New("cannot branch while tool calls are pending")
	}
	entryFound := false
	for _, entry := range s.entries {
		if entry.ID == entryID {
			entryFound = true
			break
		}
	}
	if !entryFound {
		return fmt.Errorf("session entry %q was not found", entryID)
	}
	messages, err := s.messagesAtHead(entryID)
	if err != nil {
		return err
	}
	previousMessages, previousHead := s.messages, s.head
	previousSteering, previousFollowUps := s.steering, s.followUps
	previousModelID := s.modelID
	s.messages, s.head = messages, entryID
	s.steering, s.followUps = nil, nil
	for current := entryID; current != ""; {
		entry := s.entryByID(current)
		if entry.Type == "model_change" {
			s.modelID = entry.ModelID
			break
		}
		current = entry.ParentID
	}
	if err := s.persistLocked(); err != nil {
		s.messages, s.head = previousMessages, previousHead
		s.steering, s.followUps = previousSteering, previousFollowUps
		s.modelID = previousModelID
		return fmt.Errorf("persist session branch: %w", err)
	}
	return nil
}

func (s *Session) Entries() []SessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSessionEntries(s.entries)
}

func (s *Session) entryByID(id string) SessionEntry {
	for _, entry := range s.entries {
		if entry.ID == id {
			return entry
		}
	}
	return SessionEntry{}
}

func cloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = Message{Role: message.Role, Content: append([]ContentBlock(nil), message.Content...)}
	}
	return cloned
}
