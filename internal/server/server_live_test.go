package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// This is intentionally an opt-in live test: it launches the real Sigma Agent
// executable, MiniMax API, Git worktrees, and Docker test runner. It has no
// fake model, fake task runner, or mocked HTTP server.
func TestLiveTaskServiceLifecycle(t *testing.T) {
	if os.Getenv("SIGMA_AGENT_LIVE_SERVICE_TEST") != "1" {
		t.Skip("set SIGMA_AGENT_LIVE_SERVICE_TEST=1 to run real MiniMax/Docker service integration")
	}
	if !hasModelCredential() {
		t.Fatal("live service test requires ANTHROPIC_AUTH_TOKEN, ANTHROPIC_API_KEY, or MINIMAX_API_KEY")
	}
	if output, err := exec.Command("docker", "image", "inspect", "golang:1.26", "--format", "{{.Id}}").CombinedOutput(); err != nil {
		t.Fatalf("live service test requires local golang:1.26 Docker image: %v: %s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)
	temp := t.TempDir()
	serviceHome := filepath.Join(temp, "service")
	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(serviceHome, 0o700); err != nil {
		t.Fatal(err)
	}
	createLiveFixtureRepo(t, repo)
	configPath := filepath.Join(serviceHome, "config.json")
	config := `{"max_turns":32,"max_tool_calls":64,"max_context_tokens":64000,"summary_tokens":800,"max_tool_result_bytes":16384,"model_output_tokens":16384,"tool_timeout_ms":60000,"model_timeout_ms":120000,"max_retries":1}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(temp, executableName())
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/sigma-agent")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real Sigma Agent executable: %v\n%s", err, output)
	}

	addr := reserveAddress(t)
	service := startLiveService(t, ctx, binary, serviceHome, configPath, addr, filepath.Join(temp, "service-first.log"))
	defer func() { service.stop() }()
	client := &http.Client{Timeout: 15 * time.Second}
	baseURL := "http://" + addr

	// Two real model-backed tasks share one worker. The second must remain
	// queued while the first is active; the first task is cancelled mid-test.
	taskA := submitLiveTask(t, client, baseURL, repo, `Change Greeting to return "good morning" and update TestGreeting to expect "good morning". Preserve the sleep and unrelated code. Run the Go tests.`)
	streamA := startSSE(t, baseURL, taskA.ID, "")
	waitTaskStatus(t, ctx, client, baseURL, taskA.ID, "running")
	taskB := submitLiveTask(t, client, baseURL, repo, `Change Greeting to return "good evening" and update TestGreeting to expect "good evening". Preserve the sleep and unrelated code. Run the Go tests.`)
	if got := getLiveTask(t, client, baseURL, taskB.ID).Status; got != "queued" {
		t.Fatalf("with max-concurrent=1, second task status = %q while first task is running; want queued", got)
	}
	t.Log("PASS: second real task remains queued while the single worker runs the first task")

	awaitSSETool(t, ctx, streamA, "run_tests", client, baseURL, serviceHome, taskA.ID)
	time.Sleep(time.Second) // Let Docker start the real go test process before cancelling it.
	postTaskAction(t, client, baseURL, taskA.ID, "cancel")
	waitTaskStatus(t, ctx, client, baseURL, taskA.ID, "cancelled")
	streamA.close()
	t.Log("PASS: cancel endpoint stopped an in-flight Agent test run")

	waitTaskStatus(t, ctx, client, baseURL, taskB.ID, "completed")
	assertLiveWorkspace(t, serviceHome, taskB.ID, "good evening")
	assertSuccessfulGoTest(t, serviceHome, taskB.ID)
	t.Log("PASS: queued real task ran after worker became free and changed its isolated worktree")

	postTaskAction(t, client, baseURL, taskA.ID, "resume")
	resumeMessageID := postLiveTaskMessage(t, client, baseURL, taskA.ID, "The earlier run_tests call was cancelled. Re-run the Go tests against the current workspace and fix any failures. Preserve the test sleep and unrelated code.", "steer")
	streamAResume := startSSE(t, baseURL, taskA.ID, "")
	waitTaskStatus(t, ctx, client, baseURL, taskA.ID, "completed")
	resumedEvents := collectSSE(t, ctx, streamAResume)
	assertSSELifecycle(t, resumedEvents, true)
	if !containsMessageEvent(resumedEvents, resumeMessageID, "steer") {
		t.Fatal("resumed task SSE stream did not report its steering message")
	}
	assertLiveWorkspace(t, serviceHome, taskA.ID, "good morning")
	assertSuccessfulGoTest(t, serviceHome, taskA.ID)
	t.Log("PASS: cancelled task resumed from its persisted session and retained workspace")

	// Kill the actual service and its process tree during a real tool run. A new
	// service process must restore the task as interrupted and allow continuation.
	taskC := submitLiveTask(t, client, baseURL, repo, `Change Greeting to return "good night" and update TestGreeting to expect "good night". Preserve the sleep and unrelated code. Run the Go tests.`)
	streamC := startSSE(t, baseURL, taskC.ID, "")
	waitTaskStatus(t, ctx, client, baseURL, taskC.ID, "running")
	awaitSSETool(t, ctx, streamC, "run_tests", client, baseURL, serviceHome, taskC.ID)
	time.Sleep(time.Second)
	streamC.close()
	service.stop()
	service = startLiveService(t, ctx, binary, serviceHome, configPath, addr, filepath.Join(temp, "service-restarted.log"))
	if got := getLiveTask(t, client, baseURL, taskC.ID).Status; got != "interrupted" {
		t.Fatalf("task status after real service process restart = %q, want interrupted", got)
	}
	if _, err := os.Stat(filepath.Join(serviceHome, "var", "sessions", taskC.ID+".json")); err != nil {
		t.Fatalf("interrupted task session snapshot is missing: %v", err)
	}
	t.Log("PASS: process-tree termination and restart restored running task as interrupted with its snapshot")

	postTaskAction(t, client, baseURL, taskC.ID, "resume")
	restartMessageID := postLiveTaskMessage(t, client, baseURL, taskC.ID, "The service was restarted while run_tests was executing. Inspect the interrupted result, re-run the Go tests against the current workspace, and fix any failures.", "steer")
	streamCResume := startSSE(t, baseURL, taskC.ID, "")
	waitTaskStatus(t, ctx, client, baseURL, taskC.ID, "completed")
	eventsC := collectSSE(t, ctx, streamCResume)
	assertSSELifecycle(t, eventsC, false)
	if !containsMessageEvent(eventsC, restartMessageID, "steer") {
		t.Fatal("restarted task SSE stream did not report its steering message")
	}
	assertLiveWorkspace(t, serviceHome, taskC.ID, "good night")
	assertSuccessfulGoTest(t, serviceHome, taskC.ID)
	t.Log("PASS: interrupted task resumed after service restart and passed real Docker Go tests")

	firstID := firstSSEID(t, eventsC)
	replay := collectSSE(t, ctx, startSSE(t, baseURL, taskC.ID, strconv.FormatInt(firstID, 10)))
	if containsQueuedEvent(replay, false) || !containsQueuedEvent(replay, true) || !containsEvent(replay, "task_completed", "") {
		t.Fatal("Last-Event-ID replay did not return only later SSE events through task completion")
	}
	t.Log("PASS: SSE Last-Event-ID reconnect returned subsequent persisted events")
}

func TestLiveTaskMessageInteraction(t *testing.T) {
	if os.Getenv("SIGMA_AGENT_LIVE_SERVICE_TEST") != "1" {
		t.Skip("set SIGMA_AGENT_LIVE_SERVICE_TEST=1 to run real MiniMax/Docker service integration")
	}
	if !hasModelCredential() {
		t.Fatal("live service test requires ANTHROPIC_AUTH_TOKEN, ANTHROPIC_API_KEY, or MINIMAX_API_KEY")
	}
	if output, err := exec.Command("docker", "image", "inspect", "golang:1.26", "--format", "{{.Id}}").CombinedOutput(); err != nil {
		t.Fatalf("live service test requires local golang:1.26 Docker image: %v: %s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	temp := t.TempDir()
	serviceHome := filepath.Join(temp, "service")
	repo := filepath.Join(temp, "repo")
	if err := os.MkdirAll(serviceHome, 0o700); err != nil {
		t.Fatal(err)
	}
	createLiveMessageFixtureRepo(t, repo)
	configPath := filepath.Join(serviceHome, "config.json")
	config := `{"max_turns":32,"max_tool_calls":64,"max_context_tokens":64000,"summary_tokens":800,"max_tool_result_bytes":16384,"model_output_tokens":16384,"tool_timeout_ms":60000,"model_timeout_ms":120000,"max_retries":1}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(temp, executableName())
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/sigma-agent")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real Sigma Agent executable: %v\n%s", err, output)
	}

	addr := reserveAddress(t)
	service := startLiveService(t, ctx, binary, serviceHome, configPath, addr, filepath.Join(temp, "service.log"))
	defer service.stop()
	client := &http.Client{Timeout: 15 * time.Second}
	baseURL := "http://" + addr
	task := submitLiveTask(t, client, baseURL, repo, `Change Greeting to return "good evening" and update TestGreeting to expect "good evening". Preserve unrelated code. Run the Go tests.`)

	initialStream := startSSE(t, baseURL, task.ID, "")
	awaitSSETool(t, ctx, initialStream, "run_tests", client, baseURL, serviceHome, task.ID)
	steerID := postLiveTaskMessage(t, client, baseURL, task.ID, `Also add ExcitedGreeting() string returning "good evening!" and add a test for it.`, "steer")
	waitTaskStatus(t, ctx, client, baseURL, task.ID, "completed")
	initialEvents := collectSSE(t, ctx, initialStream)
	if !containsMessageEvent(initialEvents, steerID, "steer") {
		t.Fatal("SSE stream did not report the accepted steering message")
	}
	workspace := filepath.Join(serviceHome, "var", "sessions", task.ID, "workspace")
	assertLiveFileContains(t, filepath.Join(workspace, "greeting.go"), "ExcitedGreeting")
	assertLiveFileContains(t, filepath.Join(workspace, "greeting_test.go"), "TestExcitedGreeting")
	assertSuccessfulGoTest(t, serviceHome, task.ID)
	t.Log("PASS: real in-flight steering reached the Agent, changed its patch, and passed Docker Go tests")

	followUpID := postLiveTaskMessage(t, client, baseURL, task.ID, `Also add ShoutGreeting() string returning "GOOD EVENING!" and add a test for it.`, "follow_up")
	replay := startSSE(t, baseURL, task.ID, strconv.FormatInt(firstSSEID(t, initialEvents), 10))
	waitTaskStatus(t, ctx, client, baseURL, task.ID, "completed")
	followUpEvents := collectSSE(t, ctx, replay)
	if !containsMessageEvent(followUpEvents, followUpID, "follow_up") {
		t.Fatal("SSE replay did not report the accepted follow-up message")
	}
	assertLiveFileContains(t, filepath.Join(workspace, "greeting.go"), "ShoutGreeting")
	assertLiveFileContains(t, filepath.Join(workspace, "greeting_test.go"), "TestShoutGreeting")
	assertSuccessfulGoTest(t, serviceHome, task.ID)
	t.Log("PASS: completed-session follow-up resumed the same Agent session and SSE replay delivered its events")
}

type liveTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type liveSSEEvent struct {
	ID        int64
	Type      string `json:"type"`
	Tool      string `json:"tool"`
	Resume    bool   `json:"resume"`
	MessageID string `json:"message_id"`
	Mode      string `json:"mode"`
}

type liveSSEStream struct {
	cancel context.CancelFunc
	events chan liveSSEEvent
	done   chan struct{}
	err    chan error
}

type liveServiceProcess struct {
	cmd      *exec.Cmd
	done     chan error
	stopOnce sync.Once
}

func (p *liveServiceProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	p.stopOnce.Do(func() {
		if runtime.GOOS == "windows" {
			_ = exec.Command("taskkill.exe", "/PID", strconv.Itoa(p.cmd.Process.Pid), "/T", "/F").Run()
		} else {
			_ = p.cmd.Process.Kill()
		}
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
		}
	})
}

func hasModelCredential() bool {
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "MINIMAX_API_KEY"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "sigma-agent.exe"
	}
	return "sigma-agent"
}

func createLiveFixtureRepo(t *testing.T, repo string) {
	createLiveFixtureRepoWithSleep(t, repo, 20*time.Second)
}

func createLiveMessageFixtureRepo(t *testing.T, repo string) {
	createLiveFixtureRepoWithSleep(t, repo, 8*time.Second)
}

func createLiveFixtureRepoWithSleep(t *testing.T, repo string, delay time.Duration) {
	t.Helper()
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":           "module example.com/sigmaagent-e2e\n\ngo 1.26\n",
		"greeting.go":      "package sample\n\nfunc Greeting() string { return \"hello\" }\n",
		"greeting_test.go": fmt.Sprintf("package sample\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestGreeting(t *testing.T) {\n\ttime.Sleep(%d * time.Second)\n\tif got := Greeting(); got != \"hello\" { t.Fatalf(\"Greeting() = %%q, want hello\", got) }\n}\n", int64(delay/time.Second)),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "sigmaagent-e2e@example.invalid")
	runGit("config", "user.name", "Sigma Agent E2E")
	runGit("add", ".")
	runGit("commit", "-qm", "Create real service test fixture")
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func startLiveService(t *testing.T, ctx context.Context, binary, home, configPath, addr, logPath string) *liveServiceProcess {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "--config", configPath, "--addr", addr, "--serve", "--max-concurrent", "1")
	cmd.Dir = home
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	_ = logFile.Close()
	process := &liveServiceProcess{cmd: cmd, done: make(chan error, 1)}
	go func() { process.done <- cmd.Wait() }()
	t.Cleanup(process.stop)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-process.done:
			data, _ := os.ReadFile(logPath)
			t.Fatalf("Sigma Agent service exited before becoming ready: %v\n%s", err, data)
		default:
		}
		response, err := client.Get("http://" + addr + "/tasks/not-a-real-task")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				return process
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for service: %v", ctx.Err())
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, _ := os.ReadFile(logPath)
	t.Fatalf("Sigma Agent service did not become ready\n%s", data)
	return nil
}

func submitLiveTask(t *testing.T, client *http.Client, baseURL, repo, prompt string) liveTask {
	t.Helper()
	body, err := json.Marshal(map[string]string{"repo": repo, "task": prompt})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(baseURL+"/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var task liveTask
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		t.Fatalf("decode submitted task: %v", err)
	}
	if response.StatusCode != http.StatusAccepted || task.ID == "" {
		t.Fatalf("submit task returned HTTP %d: %+v", response.StatusCode, task)
	}
	return task
}

func getLiveTask(t *testing.T, client *http.Client, baseURL, id string) liveTask {
	t.Helper()
	response, err := client.Get(baseURL + "/tasks/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var task liveTask
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		t.Fatalf("decode task %s: %v", id, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get task %s returned HTTP %d: %+v", id, response.StatusCode, task)
	}
	return task
}

func postTaskAction(t *testing.T, client *http.Client, baseURL, id, action string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/tasks/"+id+"/"+action, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s returned HTTP %d: %s", action, response.StatusCode, body)
	}
}

func postLiveTaskMessage(t *testing.T, client *http.Client, baseURL, id, message, mode string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"message": message, "mode": mode})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(baseURL+"/tasks/"+id+"/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post task message: %v", err)
	}
	defer response.Body.Close()
	var result struct {
		MessageID string `json:"message_id"`
		Mode      string `json:"mode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode task message response: %v", err)
	}
	if response.StatusCode != http.StatusAccepted || result.MessageID == "" || result.Mode != mode {
		t.Fatalf("POST task message returned HTTP %d: %+v", response.StatusCode, result)
	}
	return result.MessageID
}

func waitTaskStatus(t *testing.T, ctx context.Context, client *http.Client, baseURL, id, expected string) liveTask {
	t.Helper()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		task := getLiveTask(t, client, baseURL, id)
		if task.Status == expected {
			return task
		}
		if task.Status == "failed" {
			t.Fatalf("task %s failed while waiting for %s: %s", id, expected, task.Error)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task %s remained %q while waiting for %q: %v", id, task.Status, expected, ctx.Err())
		case <-ticker.C:
		}
	}
}

func startSSE(t *testing.T, baseURL, taskID, lastEventID string) *liveSSEStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/tasks/"+taskID+"/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		cancel()
		t.Fatalf("open task SSE stream: %v", err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		_ = response.Body.Close()
		cancel()
		t.Fatalf("task SSE returned HTTP %d with content type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	stream := &liveSSEStream{cancel: cancel, events: make(chan liveSSEEvent, 1024), done: make(chan struct{}), err: make(chan error, 1)}
	go func() {
		defer close(stream.done)
		defer close(stream.events)
		defer response.Body.Close()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 16*1024), 2*1024*1024)
		var eventID int64
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				eventID, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
			case strings.HasPrefix(line, "data: "):
				var event liveSSEEvent
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
					stream.err <- fmt.Errorf("decode SSE data: %w", err)
					return
				}
				event.ID = eventID
				select {
				case stream.events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			select {
			case stream.err <- err:
			default:
			}
		}
	}()
	return stream
}

func (s *liveSSEStream) close() {
	if s == nil {
		return
	}
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
}

func awaitSSETool(t *testing.T, ctx context.Context, stream *liveSSEStream, tool string, client *http.Client, baseURL, serviceHome, taskID string) {
	t.Helper()
	var observed []string
	for {
		select {
		case event, ok := <-stream.events:
			if !ok {
				task := getLiveTask(t, client, baseURL, taskID)
				logPath := filepath.Join(serviceHome, "var", "server", taskID, "agent.log")
				logTail := "<agent log missing>"
				if data, err := os.ReadFile(logPath); err == nil {
					logTail = string(data)
					if len(logTail) > 4000 {
						logTail = logTail[len(logTail)-4000:]
					}
				}
				select {
				case err := <-stream.err:
					t.Fatalf("SSE stream failed before %s started: %v; task=%+v; observed=%v; agent log tail:\n%s", tool, err, task, observed, logTail)
				default:
				}
				t.Fatalf("SSE stream ended before tool %s started; task=%+v; observed=%v; agent log tail:\n%s", tool, task, observed, logTail)
			}
			observed = append(observed, event.Type+func() string {
				if event.Tool != "" {
					return ":" + event.Tool
				}
				return ""
			}())
			if event.Type == "tool_execution_start" && event.Tool == tool {
				return
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for SSE tool %s: %v", tool, ctx.Err())
		}
	}
}

func collectSSE(t *testing.T, ctx context.Context, stream *liveSSEStream) []liveSSEEvent {
	t.Helper()
	defer stream.close()
	var events []liveSSEEvent
	for {
		select {
		case event, ok := <-stream.events:
			if !ok {
				select {
				case err := <-stream.err:
					t.Fatalf("SSE stream error: %v", err)
				default:
				}
				return events
			}
			events = append(events, event)
		case <-ctx.Done():
			t.Fatalf("SSE stream did not finish: %v", ctx.Err())
		}
	}
}

func assertSSELifecycle(t *testing.T, events []liveSSEEvent, expectCancel bool) {
	t.Helper()
	if expectCancel && !containsEvent(events, "task_cancelled", "") {
		t.Fatal("SSE replay is missing task_cancelled")
	}
	if !containsEvent(events, "task_completed", "") || !containsEvent(events, "tool_execution_start", "run_tests") {
		t.Fatal("SSE replay is missing real Agent tool-start or terminal completion events")
	}
	if firstSSEID(t, events) <= 0 {
		t.Fatal("SSE events did not include replay IDs")
	}
}

func containsQueuedEvent(events []liveSSEEvent, resumed bool) bool {
	for _, event := range events {
		if event.Type == "task_queued" && event.Resume == resumed {
			return true
		}
	}
	return false
}

func containsEvent(events []liveSSEEvent, eventType, tool string) bool {
	for _, event := range events {
		if event.Type == eventType && (tool == "" || event.Tool == tool) {
			return true
		}
	}
	return false
}

func containsMessageEvent(events []liveSSEEvent, messageID, mode string) bool {
	for _, event := range events {
		if event.Type == "user_message_queued" && event.MessageID == messageID && event.Mode == mode {
			return true
		}
	}
	return false
}

func firstSSEID(t *testing.T, events []liveSSEEvent) int64 {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("SSE stream returned no events")
	}
	return events[0].ID
}

func assertLiveWorkspace(t *testing.T, serviceHome, taskID, want string) {
	t.Helper()
	path := filepath.Join(serviceHome, "var", "sessions", taskID, "workspace", "greeting.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read completed task workspace: %v", err)
	}
	if !bytes.Contains(content, []byte(want)) {
		t.Fatalf("workspace source does not contain requested result %q:\n%s", want, content)
	}
}

func assertLiveFileContains(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read task workspace file %s: %v", path, err)
	}
	if !bytes.Contains(content, []byte(want)) {
		t.Fatalf("workspace file %s does not contain %q:\n%s", path, want, content)
	}
}

func assertSuccessfulGoTest(t *testing.T, serviceHome, taskID string) {
	t.Helper()
	path := filepath.Join(serviceHome, "var", "sessions", taskID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted session: %v", err)
	}
	var snapshot struct {
		Messages []struct {
			Content []struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode persisted session: %v", err)
	}
	for _, message := range snapshot.Messages {
		for _, block := range message.Content {
			if block.Type != "tool_result" {
				continue
			}
			var result struct {
				Profile string `json:"profile"`
				Error   string `json:"error"`
				Output  string `json:"output"`
			}
			if json.Unmarshal([]byte(block.Content), &result) == nil && result.Profile == "go-test" && result.Error == "" && strings.Contains(result.Output, "ok") && strings.Contains(result.Output, "example.com/sigmaagent-e2e") {
				return
			}
		}
	}
	t.Fatalf("session %s has no successful real Docker go-test tool result", taskID)
}
