package server

import (
	"bufio"
	"context"
	"crypto/rand"
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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/filelock"
	"github.com/sdaAlbert/sigma-agent/internal/sigmahome"
	"github.com/sdaAlbert/sigma-agent/internal/tracecontext"
)

type Task struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	Prompt      string    `json:"prompt"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
	Traceparent string    `json:"traceparent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func sessionSnapshotPath(root, id string) string {
	current := filepath.Join(sigmahome.Sessions(root), id+".json")
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(root, "var", "sessions", id+".json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

type manager struct {
	mu          sync.RWMutex
	root        string
	storeDir    string
	configPath  string
	commandMode string
	queue       chan string
	tasks       map[string]*Task
	running     map[string]context.CancelFunc
}

func ListenAndServe(addr string, maxConcurrent int, configPath string) error {
	return ListenAndServeWithCommandMode(addr, maxConcurrent, configPath, "")
}

func ListenAndServeWithCommandMode(addr string, maxConcurrent int, configPath, commandMode string) error {
	if maxConcurrent < 1 || maxConcurrent > 32 {
		return errors.New("max-concurrent must be between 1 and 32")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	stateDir := sigmahome.Server(root)
	if _, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
		legacy := filepath.Join(root, "var", "server")
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			stateDir = legacy
		}
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	m := &manager{root: root, storeDir: stateDir, configPath: configPath, commandMode: commandMode, queue: make(chan string, 1024), tasks: map[string]*Task{}, running: map[string]context.CancelFunc{}}
	if err := m.restore(); err != nil {
		return err
	}
	for i := 0; i < maxConcurrent; i++ {
		go m.worker(fmt.Sprintf("worker-%02d", i+1))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", m.submit)
	mux.HandleFunc("GET /tasks/{id}", m.get)
	mux.HandleFunc("GET /tasks/{id}/events", m.events)
	mux.HandleFunc("POST /tasks/{id}/messages", m.message)
	mux.HandleFunc("POST /tasks/{id}/cancel", m.cancel)
	mux.HandleFunc("POST /tasks/{id}/resume", m.resume)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	fmt.Fprintf(os.Stderr, "Sigma Agent task API listening on http://%s (max concurrent: %d)\n", addr, maxConcurrent)
	return server.ListenAndServe()
}

func (m *manager) restore() error {
	entries, err := os.ReadDir(m.storeDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.storeDir, entry.Name()))
		if err != nil {
			return err
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil || !agent.ValidSessionID(task.ID) {
			continue
		}
		if task.Status == "running" {
			task.Status = "interrupted"
			task.Error = "server restarted while task was running; resume to continue from its saved session"
			m.saveLocked(&task)
		}
		m.tasks[task.ID] = &task
		if task.Status == "queued" {
			m.queue <- task.ID
		}
	}
	return nil
}

func (m *manager) submit(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Repo string `json:"repo"`
		Task string `json:"task"`
	}
	if err := decodeJSON(r.Body, &request); err != nil || strings.TrimSpace(request.Repo) == "" || strings.TrimSpace(request.Task) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON body must include repo and task"})
		return
	}
	repo, err := filepath.Abs(request.Repo)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repository path"})
		return
	}
	info, err := os.Stat(repo)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repository path must be an existing directory"})
		return
	}
	id, err := newID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate task id"})
		return
	}
	requestTrace, err := requestSpan(r.Header.Get("traceparent"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate trace context"})
		return
	}
	task := &Task{ID: id, Repo: repo, Prompt: strings.TrimSpace(request.Task), Status: "queued", TraceID: requestTrace.TraceID, Traceparent: requestTrace.Traceparent(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	m.mu.Lock()
	m.tasks[id] = task
	err = m.saveLocked(task)
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not persist task"})
		return
	}
	m.appendTaskEvent(id, requestTrace, "", map[string]any{"type": "task_queued", "at": time.Now().UTC()})
	m.queue <- id
	writeJSON(w, http.StatusAccepted, task)
}

func (m *manager) get(w http.ResponseWriter, r *http.Request) {
	task, ok := m.getTask(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (m *manager) message(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Message string `json:"message"`
		Mode    string `json:"mode"`
	}
	if err := decodeJSON(r.Body, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON body must include message and may include mode"})
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" || len(request.Message) > 64*1024 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message must contain 1 to 65536 bytes"})
		return
	}
	if request.Mode == "" {
		request.Mode = "steer"
	}
	if request.Mode != "steer" && request.Mode != "follow_up" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be steer or follow_up"})
		return
	}
	messageID, err := newID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate message id"})
		return
	}

	m.mu.Lock()
	task := m.tasks[r.PathValue("id")]
	if task == nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Status != "running" && task.Status != "queued" && task.Status != "completed" {
		m.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "messages can be added only to queued, running, or completed tasks"})
		return
	}
	if err := os.MkdirAll(m.taskDir(task.ID), 0o700); err != nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not prepare task message inbox"})
		return
	}
	message := map[string]string{"id": messageID, "mode": request.Mode, "message": request.Message}
	encoded, err := json.Marshal(message)
	if err == nil {
		var file *os.File
		file, err = os.OpenFile(m.messagePath(task.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			_, err = file.Write(append(encoded, '\n'))
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not persist task message"})
		return
	}
	queueTask := task.Status == "completed"
	if queueTask {
		task.Status, task.Error = "queued", ""
		if err := m.saveLocked(task); err != nil {
			task.Status = "completed"
			m.mu.Unlock()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not queue task continuation"})
			return
		}
	}
	copy := *task
	m.mu.Unlock()

	messageTrace, traceErr := childTaskTrace(copy.Traceparent)
	if traceErr != nil {
		messageTrace = tracecontext.Context{}
	}
	m.appendTaskEvent(copy.ID, messageTrace, "", map[string]any{"type": "user_message_queued", "message_id": messageID, "mode": request.Mode, "message": request.Message, "at": time.Now().UTC()})
	if queueTask {
		m.appendTaskEvent(copy.ID, messageTrace, "", map[string]any{"type": "task_queued", "resume": true, "at": time.Now().UTC()})
		m.queue <- copy.ID
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": copy.ID, "message_id": messageID, "mode": request.Mode, "status": copy.Status})
}

func (m *manager) cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if cancel := m.running[id]; cancel != nil {
		cancel()
	} else if task.Status == "queued" {
		cancelTrace, traceErr := childTaskTrace(task.Traceparent)
		if traceErr == nil {
			m.appendTaskEvent(id, cancelTrace, "", map[string]any{"type": "task_cancelled", "at": time.Now().UTC()})
		}
		task.Status = "cancelled"
		task.UpdatedAt = time.Now().UTC()
		_ = m.saveLocked(task)
	}
	copy := *task
	m.mu.Unlock()
	writeJSON(w, http.StatusAccepted, copy)
}

func (m *manager) resume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Status != "interrupted" && task.Status != "cancelled" && task.Status != "failed" {
		m.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "only interrupted, cancelled, or failed tasks can be resumed"})
		return
	}
	if _, err := os.Stat(sessionSnapshotPath(m.root, id)); err != nil {
		m.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no saved agent session is available to resume"})
		return
	}
	task.Status, task.Error, task.UpdatedAt = "queued", "", time.Now().UTC()
	err := m.saveLocked(task)
	copy := *task
	m.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not persist task"})
		return
	}
	resumeTrace, traceErr := childTaskTrace(copy.Traceparent)
	if traceErr == nil {
		m.appendTaskEvent(id, resumeTrace, "", map[string]any{"type": "task_queued", "resume": true, "at": time.Now().UTC()})
	}
	m.queue <- id
	writeJSON(w, http.StatusAccepted, copy)
}

func (m *manager) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := m.getTask(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming is not supported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	lastID := int64(0)
	if value := r.Header.Get("Last-Event-ID"); value != "" {
		_, _ = fmt.Sscan(value, &lastID)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		lines, next, err := m.readEvents(id, lastID)
		if err != nil {
			return
		}
		lineOffset := lastID
		for _, line := range lines {
			lineOffset += int64(len(line)) + 1
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", lineOffset, line)
		}
		if len(lines) > 0 {
			lastID = next
			flusher.Flush()
		}
		if !m.isActive(id) {
			// Drain already-persisted events before closing the stream.
			if len(lines) == 0 {
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *manager) worker(workerID string) {
	for id := range m.queue {
		lock, busy, err := m.tryWorkspaceLock(id)
		if busy {
			m.queue <- id
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err != nil {
			m.failQueuedTask(id, err)
			continue
		}
		m.runTask(id, lock, workerID)
	}
}

func (m *manager) tryWorkspaceLock(id string) (*filelock.Lock, bool, error) {
	m.mu.RLock()
	task := m.tasks[id]
	if task == nil || task.Status != "queued" {
		m.mu.RUnlock()
		return nil, false, nil
	}
	repo := task.Repo
	m.mu.RUnlock()
	path, err := workspaceLockPath(repo)
	if err != nil {
		return nil, false, err
	}
	lock, err := filelock.Acquire(path, false)
	if errors.Is(err, filelock.ErrLocked) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return lock, false, nil
}

func workspaceLockPath(repo string) (string, error) {
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		canonical, err = filepath.Abs(repo)
	}
	if err != nil {
		return "", fmt.Errorf("resolve task workspace for locking: %w", err)
	}
	canonical = filepath.Clean(canonical)
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate Sigma workspace lock directory: %w", err)
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(cacheDir, "sigma-agent", "workspace-locks", hex.EncodeToString(digest[:])+".lock"), nil
}

func (m *manager) failQueuedTask(id string, cause error) {
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil || task.Status != "queued" {
		m.mu.Unlock()
		return
	}
	task.Status, task.Error, task.UpdatedAt = "failed", cause.Error(), time.Now().UTC()
	_ = m.saveLocked(task)
	copy := *task
	m.mu.Unlock()
	failedTrace, traceErr := childTaskTrace(copy.Traceparent)
	if traceErr == nil {
		m.appendTaskEvent(id, failedTrace, "", map[string]any{"type": "task_failed", "error": cause.Error(), "at": time.Now().UTC()})
	}
}

func (m *manager) runTask(id string, workspaceLock *filelock.Lock, workerID string) {
	if workspaceLock != nil {
		defer workspaceLock.Close()
	}
	m.mu.Lock()
	task := m.tasks[id]
	if task == nil || task.Status != "queued" {
		m.mu.Unlock()
		return
	}
	copy := *task
	parentTrace, traceErr := tracecontext.ParseTraceparent(copy.Traceparent)
	if traceErr != nil {
		parentTrace, traceErr = tracecontext.New()
		if traceErr != nil {
			m.mu.Unlock()
			m.failQueuedTask(id, traceErr)
			return
		}
		copy.Traceparent = parentTrace.Traceparent()
		task.Traceparent = copy.Traceparent
	}
	copy.TraceID = parentTrace.TraceID
	task.TraceID = parentTrace.TraceID
	workerTrace, traceErr := tracecontext.Child(parentTrace)
	if traceErr != nil {
		m.mu.Unlock()
		m.failQueuedTask(id, traceErr)
		return
	}
	resume := task.CreatedAt.Before(task.UpdatedAt) && task.Status == "queued" && fileExists(sessionSnapshotPath(m.root, id))
	task.Status, task.Error, task.UpdatedAt = "running", "", time.Now().UTC()
	_ = m.saveLocked(task)
	ctx, cancel := context.WithCancel(tracecontext.WithContext(context.Background(), workerTrace))
	m.running[id] = cancel
	m.mu.Unlock()
	workerStarted := time.Now()
	m.appendTaskEvent(id, workerTrace, workerID, map[string]any{"type": "task_started", "queue_duration_ms": workerStarted.Sub(copy.CreatedAt).Milliseconds(), "at": workerStarted})

	executable, err := os.Executable()
	if err == nil {
		args := []string{"--config", m.configPath, "--repo", copy.Repo, "--session-id", id, "--event-file", m.eventPath(id), "--message-file", m.messagePath(id), "--traceparent", workerTrace.Traceparent(), "--task-id", id, "--worker-id", workerID}
		if m.commandMode != "" {
			args = append(args, "--command-execution-mode", m.commandMode)
		}
		if resume {
			args = append(args, "--resume", id)
		} else {
			args = append(args, "--task", copy.Prompt)
		}
		var logFile *os.File
		logFile, err = os.OpenFile(filepath.Join(m.taskDir(id), "agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			cmd := exec.CommandContext(ctx, executable, args...)
			cmd.Stdout, cmd.Stderr = logFile, logFile
			err = cmd.Run()
			_ = logFile.Close()
		}
	}
	status := "completed"
	errText := ""
	requeue := false
	if err == nil {
	} else if ctx.Err() != nil {
		status, errText = "cancelled", "task cancelled"
	} else {
		status, errText = "failed", err.Error()
	}
	m.mu.Lock()
	delete(m.running, id)
	cancel()
	task = m.tasks[id]
	if err == nil && m.hasPendingMessagesLocked(id) {
		status, errText, requeue = "queued", "", true
	}
	task.Status, task.Error = status, errText
	task.UpdatedAt = time.Now().UTC()
	_ = m.saveLocked(task)
	m.mu.Unlock()
	m.appendTaskEvent(id, workerTrace, workerID, map[string]any{"type": "task_" + status, "duration_ms": time.Since(workerStarted).Milliseconds(), "error": errText, "at": time.Now().UTC()})
	if requeue {
		m.appendTaskEvent(id, workerTrace, workerID, map[string]any{"type": "task_queued", "resume": true, "at": time.Now().UTC()})
		m.queue <- id
	}
}

func (m *manager) readEvents(id string, offset int64) ([]string, int64, error) {
	file, err := os.Open(m.eventPath(id))
	if os.IsNotExist(err) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	reader := bufio.NewReader(file)
	var lines []string
	next := offset
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		next += int64(len(line))
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines, next, nil
}

func (m *manager) appendEvent(id string, event any) {
	if err := os.MkdirAll(m.taskDir(id), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(m.eventPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_ = json.NewEncoder(file).Encode(event)
	_ = file.Close()
}

func (m *manager) appendTaskEvent(id string, trace tracecontext.Context, workerID string, event map[string]any) {
	event["task_id"] = id
	if trace.TraceID != "" {
		event["trace_id"] = trace.TraceID
	}
	if trace.SpanID != "" {
		event["span_id"] = trace.SpanID
	}
	if trace.ParentSpanID != "" {
		event["parent_span_id"] = trace.ParentSpanID
	}
	if workerID != "" {
		event["worker_id"] = workerID
	}
	if _, exists := event["at"]; !exists {
		event["at"] = time.Now().UTC()
	}
	m.appendEvent(id, event)
}

func requestSpan(traceparent string) (tracecontext.Context, error) {
	traceparent = strings.TrimSpace(traceparent)
	if traceparent == "" {
		return tracecontext.New()
	}
	parent, err := tracecontext.ParseTraceparent(traceparent)
	if err != nil {
		// Invalid external context starts a fresh trace instead of poisoning the
		// task's persisted propagation state.
		return tracecontext.New()
	}
	return tracecontext.Child(parent)
}

func childTaskTrace(traceparent string) (tracecontext.Context, error) {
	parent, err := tracecontext.ParseTraceparent(traceparent)
	if err != nil {
		parent, err = tracecontext.New()
		if err != nil {
			return tracecontext.Context{}, err
		}
	}
	return tracecontext.Child(parent)
}

func (m *manager) saveLocked(task *Task) error {
	task.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.storeDir, task.ID+".json"), data, 0o600)
}

func (m *manager) getTask(id string) (Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task := m.tasks[id]
	if task == nil {
		return Task{}, false
	}
	return *task, true
}

func (m *manager) isActive(id string) bool {
	task, ok := m.getTask(id)
	return ok && (task.Status == "queued" || task.Status == "running")
}

func (m *manager) taskDir(id string) string   { return filepath.Join(m.storeDir, id) }
func (m *manager) eventPath(id string) string { return filepath.Join(m.taskDir(id), "events.jsonl") }
func (m *manager) messagePath(id string) string {
	return filepath.Join(m.taskDir(id), "messages.jsonl")
}
func (m *manager) messageOffsetPath(id string) string {
	return m.messagePath(id) + ".offset"
}

func (m *manager) hasPendingMessagesLocked(id string) bool {
	info, err := os.Stat(m.messagePath(id))
	if err == nil {
		var offset int64
		if data, readErr := os.ReadFile(m.messageOffsetPath(id)); readErr == nil {
			_, _ = fmt.Sscan(strings.TrimSpace(string(data)), &offset)
		}
		if info.Size() > offset {
			return true
		}
	}
	data, err := os.ReadFile(sessionSnapshotPath(m.root, id))
	if err != nil {
		return false
	}
	var snapshot agent.SessionSnapshot
	if json.Unmarshal(data, &snapshot) != nil {
		return false
	}
	return len(snapshot.Steering) > 0 || len(snapshot.FollowUps) > 0
}

func decodeJSON(body io.ReadCloser, target any) error {
	defer body.Close()
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]), nil
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
