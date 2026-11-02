package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	"github.com/sdaAlbert/sigma-agent/internal/server"
	"github.com/sdaAlbert/sigma-agent/internal/sigmaagent"
)

func main() {
	serve := flag.Bool("serve", false, "run the HTTP/SSE task service")
	index := flag.Bool("index", false, "incrementally synchronize repository code chunks into Qdrant")
	indexMemory := flag.Bool("index-memory", false, "synchronize reviewed repository memory into Qdrant without indexing code")
	evalRetrieval := flag.Bool("eval-retrieval", false, "evaluate BM25 and optional hybrid retrieval on a path-labeled SWE-bench dataset")
	retrievalDataset := flag.String("retrieval-dataset", "var/benchmark-data/swebench_bm25_13k_dev.json", "JSON retrieval cases; prepare with scripts/prepare_swebench_retrieval.py")
	retrievalRepoCache := flag.String("retrieval-repo-cache", "var/benchmark-data/repos", "cache for repositories used by --eval-retrieval")
	retrieveQuery := flag.String("retrieve", "", "inspect BM25 or hybrid code retrieval results without calling a model")
	retrieveTopK := flag.Int("top-k", 8, "maximum code chunks returned by --retrieve")
	configPath := flag.String("config", "config.json", "JSON file with tunable runtime limits")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address when --serve is enabled")
	maxConcurrent := flag.Int("max-concurrent", 2, "maximum concurrent agent tasks when --serve is enabled")
	repoArg := flag.String("repo", "", "clean Git repository to work on")
	task := flag.String("task", "", "coding task for the agent, or an additional instruction when resuming")
	interactive := flag.Bool("interactive", false, "start a persistent terminal conversation (default when no task or resume is given)")
	jsonOutput := flag.Bool("json", false, "write Agent lifecycle events and the final run result as JSONL to stdout")
	resumeID := flag.String("resume", "", "resume a persisted session by ID; omit --task to continue it")
	githubMCP := flag.Bool("github-mcp", true, "enable read-only GitHub MCP tools when credentials are configured (default true)")
	githubWrite := flag.Bool("github-write", false, "push the completed task branch and open a pull request; requires --push-branch and --github-mcp")
	pushBranch := flag.String("push-branch", "", "feature branch to push and open a pull request from; required with --github-write")
	baseBranch := flag.String("base-branch", "main", "branch that a pull request is opened against")
	allowContainerNetwork := flag.Bool("allow-container-network", false, "allow outbound network in the test container so package managers can download dependencies")
	keep := flag.Bool("keep-workspace", true, "keep the session worktree after a successful run")
	maxTurns := flag.Int("max-turns", 0, "override config: maximum assistant turns")
	maxToolCalls := flag.Int("max-tool-calls", 0, "override config: maximum tool calls per task")
	contextTokens := flag.Int("context-tokens", 0, "override config: maximum estimated context tokens")
	modelOutputTokens := flag.Int("model-output-tokens", 0, "override config: maximum model output tokens")
	sessionID := flag.String("session-id", "", "use this id for a new session")
	eventFile := flag.String("event-file", "", "append agent lifecycle events to this JSONL file")
	messageFile := flag.String("message-file", "", "read durable task messages from this JSONL file")
	flag.Parse()
	runtimeConfig, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(2)
	}
	if *maxTurns > 0 {
		runtimeConfig.MaxTurns = *maxTurns
	}
	if *maxToolCalls > 0 {
		runtimeConfig.MaxToolCalls = *maxToolCalls
	}
	if *contextTokens > 0 {
		runtimeConfig.MaxContextTokens = *contextTokens
	}
	if *modelOutputTokens > 0 {
		runtimeConfig.ModelOutputTokens = *modelOutputTokens
	}
	if err := runtimeConfig.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent: invalid runtime config:", err)
		os.Exit(2)
	}
	if *allowContainerNetwork {
		runtimeConfig.AllowContainerNetwork = true
	}
	if err := validateGitHubWrite(*githubWrite, *githubMCP, *pushBranch); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(2)
	}
	if *index || *indexMemory {
		if strings.TrimSpace(*repoArg) == "" {
			fmt.Fprintln(os.Stderr, "sigma-agent: --index and --index-memory require --repo")
			os.Exit(2)
		}
		if *index && *indexMemory {
			fmt.Fprintln(os.Stderr, "sigma-agent: choose either --index or --index-memory")
			os.Exit(2)
		}
		if *indexMemory {
			memoryCount, err := sigmaagent.IndexRepositoryMemory(*repoArg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent:", err)
				os.Exit(1)
			}
			fmt.Printf("Indexed %d reviewed memory entries into Qdrant.\n", memoryCount)
			return
		}
		codeCount, memoryCount, err := sigmaagent.IndexRepositoryWithMemory(*repoArg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		fmt.Printf("Indexed %d code chunks and %d reviewed memory entries into Qdrant.\n", codeCount, memoryCount)
		return
	}
	if *evalRetrieval {
		workingDir, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		datasetPath := *retrievalDataset
		if !filepath.IsAbs(datasetPath) {
			datasetPath = filepath.Join(workingDir, datasetPath)
		}
		cases, err := sigmaagent.LoadRetrievalCases(datasetPath)
		if err == nil {
			var evaluation sigmaagent.RetrievalEvaluation
			cachePath := *retrievalRepoCache
			if !filepath.IsAbs(cachePath) {
				cachePath = filepath.Join(workingDir, cachePath)
			}
			evaluation, err = sigmaagent.EvaluateRetrievalDataset(cachePath, cases, 5)
			if err == nil {
				_ = json.NewEncoder(os.Stdout).Encode(evaluation)
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	if *retrieveQuery != "" {
		if strings.TrimSpace(*repoArg) == "" {
			fmt.Fprintln(os.Stderr, "sigma-agent: --retrieve requires --repo")
			os.Exit(2)
		}
		if *retrieveTopK < 1 || *retrieveTopK > 12 {
			fmt.Fprintln(os.Stderr, "sigma-agent: --top-k must be from 1 to 12")
			os.Exit(2)
		}
		results, err := sigmaagent.RetrieveCode(*repoArg, *retrieveQuery, *retrieveTopK, runtimeConfig.MaxRetrievalQueryBytes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		fmt.Println(results)
		return
	}
	if *serve {
		if err := server.ListenAndServe(*addr, *maxConcurrent, *configPath); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	interactiveMode := *interactive || (strings.TrimSpace(*task) == "" && strings.TrimSpace(*resumeID) == "")
	if *jsonOutput && interactiveMode {
		fmt.Fprintln(os.Stderr, "sigma-agent: --json requires a one-shot --task or --resume invocation; interactive mode is not supported")
		os.Exit(2)
	}
	if *githubWrite && interactiveMode {
		fmt.Fprintln(os.Stderr, "sigma-agent: --github-write requires a one-shot --task or --resume run")
		os.Exit(2)
	}
	if strings.TrimSpace(*repoArg) == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*repoArg, *task, *keep, runtimeConfig, *resumeID, *githubMCP, *sessionID, *eventFile, *messageFile, interactiveMode, *jsonOutput, *githubWrite, *pushBranch, *baseBranch); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(1)
	}
}

// validateGitHubWrite checks that an explicitly authorized GitHub write scope
// is complete. GitHub write access is only granted together with a named
// feature branch and with the GitHub MCP server enabled.
func validateGitHubWrite(githubWrite, githubMCP bool, pushBranch string) error {
	if !githubWrite {
		if strings.TrimSpace(pushBranch) != "" {
			return errors.New("--push-branch requires --github-write")
		}
		return nil
	}
	if !githubMCP {
		return errors.New("--github-write requires --github-mcp")
	}
	if strings.TrimSpace(pushBranch) == "" {
		return errors.New("--github-write requires --push-branch")
	}
	return nil
}

func run(repoArg, task string, keep bool, runtimeConfig config.Runtime, resumeID string, githubMCP bool, requestedSessionID, requestedEventFile, requestedMessageFile string, interactive, jsonOutput bool, githubWrite bool, pushBranch, baseBranch string) error {
	if githubWrite && !agent.GitHubMCPConfigured() {
		return errors.New("--github-write requires GITHUB_PERSONAL_ACCESS_TOKEN or .secrets/github-pat")
	}
	repo, err := gitOutput(repoArg, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve Git repository: %w", err)
	}
	repo = strings.TrimSpace(repo)
	workingDir, err := os.Getwd()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(workingDir, "var", "runs")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	sessionsDir := filepath.Join(workingDir, "var", "sessions")
	store, err := agent.NewFileSessionStore(sessionsDir)
	if err != nil {
		return err
	}
	runID, err := newRunID()
	if err != nil {
		return err
	}
	runDir := filepath.Join(baseDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	sessionID := strings.TrimSpace(resumeID)
	newSession := sessionID == ""
	if newSession {
		sessionID = strings.TrimSpace(requestedSessionID)
		if sessionID == "" {
			sessionID = runID
		}
		if !agent.ValidSessionID(sessionID) {
			return errors.New("invalid requested session id")
		}
	} else if !agent.ValidSessionID(sessionID) {
		return errors.New("invalid session id")
	}
	sessionDir := filepath.Join(sessionsDir, sessionID)
	workspace := filepath.Join(sessionDir, "workspace")
	startingCommit := ""
	removeWorkspaceOnSuccess := true
	var runErr error
	keepWorkspaceAfterRun := keep
	worktreeCreated := false
	defer func() {
		if worktreeCreated && removeWorkspaceOnSuccess && !keepWorkspaceAfterRun && runErr == nil {
			_, _ = gitOutput(repo, "worktree", "remove", "--force", workspace)
			_, _ = gitOutput(repo, "worktree", "prune")
		}
	}()
	if newSession {
		status, err := gitOutput(repo, "status", "--porcelain")
		if err != nil {
			return err
		}
		if strings.TrimSpace(status) != "" {
			return errors.New("source repository must have a clean working tree")
		}
		commit, err := gitOutput(repo, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		startingCommit = strings.TrimSpace(commit)
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			return err
		}
		if _, err := gitOutput(repo, "worktree", "add", "--detach", workspace, startingCommit); err != nil {
			return fmt.Errorf("create detached task worktree: %w", err)
		}
		worktreeCreated = true
		metadata, _ := json.MarshalIndent(map[string]string{"repo": repo, "starting_commit": startingCommit}, "", "  ")
		if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), metadata, 0o600); err != nil {
			return err
		}
	} else {
		var metadata struct {
			Repo           string `json:"repo"`
			StartingCommit string `json:"starting_commit"`
		}
		data, err := os.ReadFile(filepath.Join(sessionDir, "metadata.json"))
		if err != nil {
			return fmt.Errorf("read session metadata: %w", err)
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			return fmt.Errorf("decode session metadata: %w", err)
		}
		if filepath.Clean(metadata.Repo) != filepath.Clean(repo) {
			return errors.New("resume repository does not match the session's original repository")
		}
		startingCommit = metadata.StartingCommit
		if _, err := os.Stat(workspace); err != nil {
			return fmt.Errorf("session worktree is unavailable; start a new task or restore the worktree: %w", err)
		}
		worktreeRoot, err := gitOutput(workspace, "rev-parse", "--show-toplevel")
		if err != nil || filepath.Clean(strings.TrimSpace(worktreeRoot)) != filepath.Clean(workspace) {
			return errors.New("session workspace is not the original Git worktree")
		}
	}
	if !newSession {
		worktreeCreated = true
	}
	toolOutputStore, err := agent.NewFileToolOutputStore(filepath.Join(sessionDir, "tool_outputs"))
	if err != nil {
		return err
	}

	tracePath := filepath.Join(runDir, "trace.jsonl")
	if requestedEventFile != "" {
		tracePath = requestedEventFile
		if err := os.MkdirAll(filepath.Dir(tracePath), 0o700); err != nil {
			return err
		}
	}
	traceFile, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer traceFile.Close()
	var traceMu sync.Mutex
	var stdoutMu sync.Mutex
	eventSink := func(event agent.Event) {
		traceMu.Lock()
		_ = json.NewEncoder(traceFile).Encode(event)
		traceMu.Unlock()
		if jsonOutput {
			stdoutMu.Lock()
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"protocol_version": 1, "type": "agent_event", "event": event})
			stdoutMu.Unlock()
		} else if event.Type == "model_text_delta" {
			_, _ = io.WriteString(os.Stdout, event.Text)
		} else if event.Type == "tool_execution_start" {
			fmt.Fprintf(os.Stderr, "\n[tool] %s started\n", event.Tool)
		} else if event.Type == "tool_execution_end" {
			fmt.Fprintf(os.Stderr, "[tool] %s finished (%d ms)\n", event.Tool, event.Duration)
		} else if event.Error != "" {
			fmt.Fprintf(os.Stderr, "\n[%s] %s\n", event.Type, event.Error)
		}
	}

	model, err := agent.NewMiniMaxMessagesModelFromEnv()
	if err != nil {
		return err
	}
	model.MaxRetries = runtimeConfig.MaxRetries
	model.MaxOutputTokens = runtimeConfig.ModelOutputTokens
	model.Client.Timeout = time.Duration(runtimeConfig.ModelTimeoutMS) * time.Millisecond
	tools, err := sigmaagent.BuiltinTools(workspace, runtimeConfig.GoTestImage, runtimeConfig.MaxRetrievalQueryBytes, model, toolOutputStore, runtimeConfig.AllowContainerNetwork)
	if err != nil {
		return err
	}
	tools = append(tools, agent.ReadToolOutputTool(toolOutputStore, runtimeConfig.MaxToolResultBytes))
	projectGuidance, err := sigmaagent.LoadProjectGuidance(workspace)
	if err != nil {
		return fmt.Errorf("load repository skills and memory: %w", err)
	}
	if !runtimeConfig.MemoryEnabled {
		projectGuidance.Memory = ""
		filtered := tools[:0]
		for _, registeredTool := range tools {
			if registeredTool.Definition.Name != "search_memory" {
				filtered = append(filtered, registeredTool)
			}
		}
		tools = filtered
	}
	if !runtimeConfig.SkillsEnabled {
		projectGuidance.Skills = nil
	}
	if len(projectGuidance.Skills) > 0 {
		tools = append(tools, projectGuidance.SkillTool())
	}
	if runtimeConfig.WebSearchEnabled {
		tools = append(tools, sigmaagent.WebSearchTool(model.WebSearch))
	}
	githubMCPEnabled := githubMCP && agent.GitHubMCPConfigured()
	githubWriteOptions := agent.GitHubWriteOptions{}
	var githubClient *agent.GitHubMCP
	if githubWrite {
		repository, err := agent.OriginRepository(repo, "origin")
		if err != nil {
			return fmt.Errorf("resolve origin repository for --github-write: %w", err)
		}
		githubWriteOptions = agent.GitHubWriteOptions{
			Repository: repository,
			HeadBranch: strings.TrimSpace(pushBranch),
			BaseBranch: strings.TrimSpace(baseBranch),
		}
	}
	if githubMCPEnabled {
		client, err := agent.StartGitHubMCPWithWrite(context.Background(), githubWriteOptions)
		if err != nil {
			return err
		}
		defer func() {
			if err := client.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "warning: close GitHub MCP server:", err)
			}
		}()
		githubClient = client
		githubTools, err := client.Tools(context.Background())
		if err != nil {
			return err
		}
		tools = append(tools, githubTools...)
	}
	registry, err := agent.NewToolRegistry(tools)
	if err != nil {
		return err
	}
	var session *agent.Session
	useMemorySearch := false
	for _, registeredTool := range tools {
		if registeredTool.Definition.Name == "search_memory" {
			useMemorySearch = true
			break
		}
	}
	if !useMemorySearch {
		projectGuidance.Memory = ""
	}
	systemPrompt := projectGuidance.SystemPrompt(codingSystemPrompt)
	if useMemorySearch {
		systemPrompt = projectGuidance.SystemPromptWithMemorySearch(codingSystemPrompt)
	}
	if newSession {
		session = &agent.Session{ID: sessionID, System: systemPrompt, Store: store}
	} else {
		snapshot, err := store.Load(sessionID)
		if err != nil {
			return fmt.Errorf("load session %s: %w", sessionID, err)
		}
		session, err = agent.RestoreSession(snapshot, model, registry, store)
		if err != nil {
			return err
		}
		session.System = systemPrompt
	}
	session.Model = model
	session.Tools = registry
	session.ToolOutputs = toolOutputStore
	session.BeforeTool = configuredToolPolicy(runtimeConfig)
	session.Limits = agent.Limits{MaxTurns: runtimeConfig.MaxTurns, MaxToolCalls: runtimeConfig.MaxToolCalls, MaxToolResultBytes: runtimeConfig.MaxToolResultBytes, ToolTimeout: time.Duration(runtimeConfig.ToolTimeoutMS) * time.Millisecond}
	session.Context = agent.WindowContextManager{MaxTokens: runtimeConfig.MaxContextTokens, SummaryTokens: runtimeConfig.SummaryTokens}
	session.OnEvent = eventSink
	ctx, cancel := signalContext()
	defer cancel()
	if requestedMessageFile != "" {
		if _, err := consumeTaskMessages(requestedMessageFile, session); err != nil {
			return fmt.Errorf("read queued task messages: %w", err)
		}
		go watchTaskMessages(ctx, requestedMessageFile, session)
	}
	var result agent.Result
	if interactive {
		var interactiveErr error
		result, interactiveErr = interactiveSession(ctx, session, workspace, task, !newSession)
		if interactiveErr != nil {
			runErr = interactiveErr
		}
	} else if task != "" {
		result, runErr = session.Prompt(ctx, task)
	} else {
		result, runErr = session.Continue(ctx)
	}
	if runErr != nil {
		keepWorkspaceAfterRun = true
	}
	patch, diffErr := sigmaagent.WorkspaceDiff(workspace)
	if diffErr != nil && runErr == nil {
		runErr = fmt.Errorf("collect patch: %w", diffErr)
	}
	if writeErr := os.WriteFile(filepath.Join(runDir, "result.patch"), []byte(patch), 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	transcript, _ := json.MarshalIndent(session.Messages(), "", "  ")
	if writeErr := os.WriteFile(filepath.Join(runDir, "transcript.json"), transcript, 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	metadata, _ := json.MarshalIndent(map[string]any{
		"run_id": runID, "session_id": sessionID, "repo": repo, "starting_commit": startingCommit, "model": model.ModelID(),
		"github_mcp_enabled":        githubMCPEnabled,
		"tool_output_artifacts_dir": filepath.Join(sessionDir, "tool_outputs"),
		"turns":                     result.Turns, "tool_calls": result.ToolCalls, "input_tokens": result.InputTokens,
		"output_tokens": result.OutputTokens, "finished_at": time.Now().UTC(), "error": errorString(runErr),
	}, "", "  ")
	if writeErr := os.WriteFile(filepath.Join(runDir, "run.json"), metadata, 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	if jsonOutput {
		completion := map[string]any{
			"protocol_version": 1, "type": "run_completed", "status": "completed", "run_id": runID, "session_id": sessionID,
			"run_dir": runDir, "patch_path": filepath.Join(runDir, "result.patch"),
			"turns": result.Turns, "tool_calls": result.ToolCalls,
			"input_tokens": result.InputTokens, "output_tokens": result.OutputTokens,
		}
		if runErr != nil {
			completion["type"] = "run_failed"
			completion["status"] = "failed"
			completion["error"] = runErr.Error()
		}
		if err := json.NewEncoder(os.Stdout).Encode(completion); err != nil && runErr == nil {
			runErr = fmt.Errorf("write JSONL completion event: %w", err)
		}
	}

	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "\n\nRun artifacts: %s\nPatch: %s\n", runDir, filepath.Join(runDir, "result.patch"))
		fmt.Fprintf(os.Stderr, "Session ID: %s\nSession snapshot: %s\n", sessionID, filepath.Join(sessionsDir, sessionID+".json"))
		if keep || runErr != nil {
			fmt.Fprintf(os.Stderr, "Task worktree kept at: %s\n", workspace)
		}
	}
	if runErr != nil {
		return runErr
	}
	if githubWrite {
		// The branch push is performed by the runtime, not by the model, so the
		// authorized branch is the only branch that can be published.
		pushOptions := agent.PushBranchOptions{
			Branch:     strings.TrimSpace(pushBranch),
			Remote:     "origin",
			BaseBranch: strings.TrimSpace(baseBranch),
		}
		if err := agent.PushBranch(repo, workspace, pushOptions); err != nil {
			return fmt.Errorf("push feature branch: %w", err)
		}
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Pushed feature branch: %s\n", pushOptions.Branch)
		}
		if githubClient == nil {
			return errors.New("GitHub MCP client is unavailable after the feature branch was pushed")
		}
		title := pullRequestTitle(task, result.Text)
		body := pullRequestBody(task, result.Text)
		pr, err := githubClient.CreatePullRequest(context.Background(), githubWriteOptions, title, body)
		if err != nil {
			return fmt.Errorf("feature branch was pushed but pull request creation failed: %w", err)
		}
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Created pull request: %s\n", pr)
		}
	}
	return nil
}

func pullRequestTitle(task, summary string) string {
	text := strings.TrimSpace(task)
	if text == "" {
		text = strings.TrimSpace(summary)
	}
	text = strings.Join(strings.Fields(strings.SplitN(text, "\n", 2)[0]), " ")
	if text == "" {
		text = "Apply Sigma Agent changes"
	}
	if !strings.HasPrefix(strings.ToLower(text), "sigma agent:") {
		text = "Sigma Agent: " + text
	}
	return truncateUTF8(text, 256)
}

func pullRequestBody(task, summary string) string {
	const maxSection = 24000
	truncate := func(value string) string {
		if len(value) > maxSection {
			return truncateUTF8(value, maxSection) + "\n\n[truncated]"
		}
		return value
	}
	return "## Task\n\n" + truncate(strings.TrimSpace(task)) + "\n\n## Sigma Agent summary\n\n" + truncate(strings.TrimSpace(summary)) + "\n\nCreated by Sigma Agent. Review the changes and run the repository's checks before merging."
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	var result strings.Builder
	result.Grow(maxBytes)
	for _, r := range value {
		if result.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}

func interactiveSession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool) (agent.Result, error) {
	type inputEvent struct {
		line string
		err  error
		eof  bool
	}
	type runOutcome struct {
		result agent.Result
		err    error
	}

	var total agent.Result
	fmt.Fprintf(os.Stderr, "\nSigma Agent interactive session %s\nCommands: /help, /diff, /abort, /exit\nWhile the Agent is running, enter text to steer its next turn.\n", session.ID)
	input := make(chan inputEvent, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			select {
			case input <- inputEvent{line: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case input <- inputEvent{err: scanner.Err(), eof: true}:
		case <-ctx.Done():
		}
	}()

	var activeDone chan runOutcome
	exitWhenDone := false
	pendingPrompt := ""
	startRun := func(prompt string, continueSession bool) {
		activeDone = make(chan runOutcome, 1)
		go func(done chan runOutcome) {
			var outcome runOutcome
			if continueSession {
				outcome.result, outcome.err = session.Continue(ctx)
			} else {
				outcome.result, outcome.err = session.Prompt(ctx, prompt)
			}
			done <- outcome
		}(activeDone)
	}
	if resume {
		messages := session.Messages()
		canContinue := len(messages) > 0 && messages[len(messages)-1].Role != "assistant"
		if canContinue {
			startRun("", true)
			pendingPrompt = strings.TrimSpace(initialPrompt)
		} else if strings.TrimSpace(initialPrompt) != "" {
			startRun(strings.TrimSpace(initialPrompt), false)
		} else {
			fmt.Fprintln(os.Stderr, "Restored a completed session; enter a new instruction to continue it.")
		}
	} else if strings.TrimSpace(initialPrompt) != "" {
		startRun(strings.TrimSpace(initialPrompt), false)
	}

	for {
		inputCh := (<-chan inputEvent)(input)
		if exitWhenDone {
			inputCh = nil
		}
		if activeDone == nil {
			fmt.Fprint(os.Stderr, "\nYou> ")
		} else {
			fmt.Fprint(os.Stderr, "\nSteer> ")
		}
		select {
		case <-ctx.Done():
			if activeDone != nil {
				<-activeDone
			}
			fmt.Fprintln(os.Stderr, "\nSession interrupted; conversation state was saved.")
			return total, nil
		case outcome := <-activeDone:
			activeDone = nil
			addResult(&total, outcome.result)
			fmt.Fprintln(os.Stderr)
			if outcome.err != nil && !errors.Is(outcome.err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "[agent error] %v\n", outcome.err)
			}
			if exitWhenDone {
				fmt.Fprintln(os.Stderr, "Session saved.")
				return total, nil
			}
			if pendingPrompt != "" {
				prompt := pendingPrompt
				pendingPrompt = ""
				startRun(prompt, false)
			}
		case item := <-inputCh:
			if item.eof {
				if item.err != nil {
					return total, fmt.Errorf("read terminal input: %w", item.err)
				}
				if activeDone != nil {
					exitWhenDone = true
					fmt.Fprintln(os.Stderr, "\nEnd of input; waiting for the current turn to finish.")
					continue
				}
				fmt.Fprintln(os.Stderr, "\nEnd of input; session saved.")
				return total, nil
			}
			line := strings.TrimSpace(item.line)
			if line == "" {
				continue
			}
			switch {
			case line == "/exit" || line == "/quit":
				if activeDone != nil {
					exitWhenDone = true
					session.Abort()
					fmt.Fprintln(os.Stderr, "\nStopping the active turn and saving the session...")
					continue
				}
				return total, nil
			case line == "/abort":
				if activeDone == nil {
					fmt.Fprintln(os.Stderr, "No active Agent turn.")
				} else {
					session.Abort()
					fmt.Fprintln(os.Stderr, "\nAbort requested; session will remain open.")
				}
			case line == "/help":
				fmt.Fprintln(os.Stderr, "Enter a coding instruction. During an active turn, plain text or /steer <text> queues guidance for the next safe turn. /followup <text> runs after the current task settles. /diff shows the patch; /abort stops the active turn; /exit saves and ends the session.")
			case line == "/diff":
				diff, err := gitOutput(workspace, "diff", "--stat", "HEAD")
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else if strings.TrimSpace(diff) == "" {
					fmt.Fprintln(os.Stderr, "No changes yet.")
				} else {
					fmt.Fprint(os.Stdout, diff)
				}
			case strings.HasPrefix(line, "/steer "):
				message := strings.TrimSpace(strings.TrimPrefix(line, "/steer "))
				if message == "" {
					fmt.Fprintln(os.Stderr, "Usage: /steer <guidance>")
				} else if activeDone == nil {
					fmt.Fprintln(os.Stderr, "Use plain text to start a new turn; /steer is for an active Agent turn.")
				} else if err := session.Steer(message); err != nil {
					fmt.Fprintf(os.Stderr, "Could not queue steering message: %v\n", err)
				} else {
					fmt.Fprintln(os.Stderr, "Steering queued for the next safe turn.")
				}
			case strings.HasPrefix(line, "/followup "):
				message := strings.TrimSpace(strings.TrimPrefix(line, "/followup "))
				if message == "" {
					fmt.Fprintln(os.Stderr, "Usage: /followup <instruction>")
				} else if activeDone == nil {
					fmt.Fprintln(os.Stderr, "/followup is available while the Agent is running.")
				} else if err := session.FollowUp(message); err != nil {
					fmt.Fprintf(os.Stderr, "Could not queue follow-up: %v\n", err)
				} else {
					fmt.Fprintln(os.Stderr, "Follow-up queued for after the current task.")
				}
			default:
				if activeDone != nil {
					if err := session.Steer(line); err != nil {
						fmt.Fprintf(os.Stderr, "Could not queue steering message: %v\n", err)
					} else {
						fmt.Fprintln(os.Stderr, "Steering queued for the next safe turn.")
					}
				} else {
					startRun(line, false)
				}
			}
		}
	}
}

func addResult(total *agent.Result, current agent.Result) {
	total.Text += current.Text
	total.Turns += current.Turns
	total.ToolCalls += current.ToolCalls
	total.InputTokens += current.InputTokens
	total.OutputTokens += current.OutputTokens
}

func configuredToolPolicy(runtimeConfig config.Runtime) agent.ToolPolicy {
	return func(ctx context.Context, tool agent.Tool, input json.RawMessage) error {
		permission := runtimeConfig.DefaultToolPermission
		if override, ok := runtimeConfig.ToolPermissions[tool.Definition.Name]; ok {
			permission = override
		}
		if permission == "deny" {
			return fmt.Errorf("tool %q is denied by the configured permission policy", tool.Definition.Name)
		}
		return nil
	}
}

func gitOutput(directory string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func newRunID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(bytes[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:]), nil
}

type taskMessage struct {
	ID      string `json:"id"`
	Mode    string `json:"mode"`
	Message string `json:"message"`
}

func watchTaskMessages(ctx context.Context, path string, session *agent.Session) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := consumeTaskMessages(path, session); err != nil {
				fmt.Fprintln(os.Stderr, "warning: read queued task message:", err)
			}
		}
	}
}

func consumeTaskMessages(path string, session *agent.Session) (int64, error) {
	offsetPath := path + ".offset"
	var offset int64
	if data, err := os.ReadFile(offsetPath); err == nil {
		parsed, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if parseErr == nil && parsed >= 0 {
			offset = parsed
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return offset, nil
	}
	if err != nil {
		return offset, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return offset, readErr
			}
			return offset, nil
		}
		var message taskMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			return offset, fmt.Errorf("decode task message at byte %d: %w", offset, err)
		}
		if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.Message) == "" {
			return offset, fmt.Errorf("task message at byte %d is missing an id or message", offset)
		}
		var queueErr error
		switch message.Mode {
		case "steer":
			queueErr = session.SteerWithID(message.ID, message.Message)
		case "follow_up":
			queueErr = session.FollowUpWithID(message.ID, message.Message)
		default:
			return offset, fmt.Errorf("task message at byte %d has unsupported mode %q", offset, message.Mode)
		}
		if queueErr != nil {
			return offset, queueErr
		}
		previousOffset := offset
		offset += int64(len(line))
		if err := os.WriteFile(offsetPath, []byte(strconv.FormatInt(offset, 10)), 0o600); err != nil {
			return previousOffset, err
		}
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
