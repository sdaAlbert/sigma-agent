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
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	"github.com/sdaAlbert/sigma-agent/internal/server"
	"github.com/sdaAlbert/sigma-agent/internal/sigmaagent"
)

func main() {
	serve := flag.Bool("serve", false, "run the HTTP/SSE task service")
	indexMemory := flag.Bool("index-memory", false, "synchronize repository memory into Qdrant")
	evalMemory := flag.Bool("eval-memory", false, "compare BM25, vector, and hybrid retrieval on labeled repository-memory queries")
	memoryDataset := flag.String("memory-dataset", "evals/memory-retrieval-v1.json", "labeled memory retrieval queries")
	memoryTopK := flag.Int("memory-top-k", 5, "number of memory results used for retrieval metrics")
	configPath := flag.String("config", "config.json", "JSON file with tunable runtime limits")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address when --serve is enabled")
	maxConcurrent := flag.Int("max-concurrent", 2, "maximum concurrent agent tasks when --serve is enabled")
	repoArg := flag.String("repo", "", "Git repository to work on (defaults to the current directory in interactive setup)")
	task := flag.String("task", "", "coding task for the agent, or an additional instruction when resuming")
	interactive := flag.Bool("interactive", false, "start a persistent terminal conversation (default when no task or resume is given)")
	jsonOutput := flag.Bool("json", false, "write Agent lifecycle events and the final run result as JSONL to stdout")
	resumeID := flag.String("resume", "", "resume a persisted session by ID; omit --task to continue it")
	githubMCP := flag.Bool("github-mcp", true, "enable read-only GitHub MCP tools when credentials are configured (default true)")
	githubWrite := flag.Bool("github-write", false, "advanced: enable GitHub PR creation at startup; interactive users can use /github-write")
	pushBranch := flag.String("push-branch", "", "advanced: configure a publish branch at startup; interactive users can use /publish")
	allowContainerNetwork := flag.Bool("allow-container-network", false, "advanced: allow container internet access at startup; interactive users can use /network")
	keep := flag.Bool("keep-workspace", true, "keep the session worktree after a successful run")
	maxTurns := flag.Int("max-turns", 0, "override config: maximum assistant turns")
	maxToolCalls := flag.Int("max-tool-calls", 0, "override config: maximum tool calls per task")
	contextTokens := flag.Int("context-tokens", 0, "override config: maximum estimated context tokens")
	contextCompactionTokens := flag.Int("context-compaction-tokens", 0, "override config: proactively compact context above this estimated token count")
	modelOutputTokens := flag.Int("model-output-tokens", 0, "override config: maximum model output tokens")
	sessionID := flag.String("session-id", "", "use this id for a new session")
	eventFile := flag.String("event-file", "", "append agent lifecycle events to this JSONL file")
	messageFile := flag.String("message-file", "", "read durable task messages from this JSONL file")
	flag.Parse()
	if *pushBranch != "" && (*indexMemory || *evalMemory) {
		fmt.Fprintln(os.Stderr, "sigma-agent: --push-branch cannot be combined with memory indexing or evaluation")
		os.Exit(2)
	}
	if *githubWrite && (*pushBranch == "" || !*githubMCP) {
		fmt.Fprintln(os.Stderr, "sigma-agent: --github-write requires --push-branch and --github-mcp")
		os.Exit(2)
	}
	if *githubWrite && !agent.GitHubMCPConfigured() {
		fmt.Fprintln(os.Stderr, "sigma-agent: --github-write requires a configured GitHub token")
		os.Exit(2)
	}
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
		if runtimeConfig.ContextCompactionThresholdTokens > *contextTokens {
			runtimeConfig.ContextCompactionThresholdTokens = *contextTokens
		}
	}
	if *contextCompactionTokens > 0 {
		runtimeConfig.ContextCompactionThresholdTokens = *contextCompactionTokens
	}
	if *modelOutputTokens > 0 {
		runtimeConfig.ModelOutputTokens = *modelOutputTokens
	}
	if err := runtimeConfig.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent: invalid runtime config:", err)
		os.Exit(2)
	}
	if *indexMemory {
		if strings.TrimSpace(*repoArg) == "" {
			fmt.Fprintln(os.Stderr, "sigma-agent: --index-memory requires --repo")
			os.Exit(2)
		}
		memoryCount, err := sigmaagent.IndexRepositoryMemory(*repoArg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		fmt.Printf("Indexed %d repository memory entries into Qdrant.\n", memoryCount)
		return
	}
	if *evalMemory {
		if strings.TrimSpace(*repoArg) == "" {
			fmt.Fprintln(os.Stderr, "sigma-agent: --eval-memory requires --repo")
			os.Exit(2)
		}
		if *memoryTopK < 1 || *memoryTopK > 20 {
			fmt.Fprintln(os.Stderr, "sigma-agent: --memory-top-k must be from 1 to 20")
			os.Exit(2)
		}
		datasetPath := *memoryDataset
		if !filepath.IsAbs(datasetPath) {
			workingDir, err := os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent:", err)
				os.Exit(1)
			}
			datasetPath = filepath.Join(workingDir, datasetPath)
		}
		data, err := os.ReadFile(datasetPath)
		if err == nil {
			var cases []sigmaagent.MemoryRetrievalCase
			err = json.Unmarshal(data, &cases)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
				defer cancel()
				var evaluation sigmaagent.MemoryRetrievalEvaluation
				evaluation, err = sigmaagent.EvaluateMemoryRetrieval(ctx, *repoArg, cases, *memoryTopK)
				if err == nil {
					err = json.NewEncoder(os.Stdout).Encode(evaluation)
				}
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	if *serve {
		if *pushBranch != "" {
			fmt.Fprintln(os.Stderr, "sigma-agent: --push-branch is available only for direct CLI sessions, not --serve")
			os.Exit(2)
		}
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
	if *allowContainerNetwork {
		runtimeConfig.AllowContainerNetwork = true
	}
	var setupInput *bufio.Scanner
	modelSelection := agent.ModelSelection{}
	modelSelectionExplicit := false
	if interactiveMode && strings.TrimSpace(*task) == "" {
		defaultRepo := strings.TrimSpace(*repoArg)
		if defaultRepo == "" {
			defaultRepo, err = os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent:", err)
				os.Exit(1)
			}
		}
		setup, start, input, setupErr := interactiveSetup(interactiveSetupOptions{
			repo: defaultRepo, sessionID: *sessionID, resumeID: *resumeID,
			pushBranch: *pushBranch, githubWrite: *githubWrite,
			allowNetwork: runtimeConfig.AllowContainerNetwork,
			githubMCP:    *githubMCP,
		})
		if setupErr != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", setupErr)
			os.Exit(1)
		}
		if !start {
			return
		}
		setupInput = input
		modelSelection = setup.model
		modelSelectionExplicit = setup.modelExplicit
		*repoArg = setup.repo
		*sessionID = setup.sessionID
		*resumeID = setup.resumeID
		*pushBranch = setup.pushBranch
		*githubWrite = setup.githubWrite
		runtimeConfig.AllowContainerNetwork = setup.allowNetwork
		*task = setup.initialPrompt
	}
	if strings.TrimSpace(*repoArg) == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*repoArg, *task, *keep, runtimeConfig, *resumeID, *githubMCP, *githubWrite, *sessionID, *pushBranch, *eventFile, *messageFile, interactiveMode, *jsonOutput, modelSelection, modelSelectionExplicit, setupInput); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(1)
	}
}

func run(repoArg, task string, keep bool, runtimeConfig config.Runtime, resumeID string, githubMCP, githubWrite bool, requestedSessionID, requestedPushBranch, requestedEventFile, requestedMessageFile string, interactive, jsonOutput bool, requestedModel agent.ModelSelection, modelExplicit bool, setupInput ...*bufio.Scanner) error {
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
	activeModelSelection := requestedModel
	if activeModelSelection.Provider == "" {
		activeModelSelection, err = defaultModelSelection()
		if err != nil {
			return err
		}
	}
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
	var restoredSnapshot *agent.SessionSnapshot
	if !newSession {
		snapshot, loadErr := store.Load(sessionID)
		if loadErr != nil {
			return fmt.Errorf("load session %s: %w", sessionID, loadErr)
		}
		restoredSnapshot = &snapshot
	}
	startingCommit := ""
	activePushBranch := ""
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
		status, err := gitOutput(repo, "status", "--porcelain=v1", "--untracked-files=all")
		if err != nil {
			return err
		}
		if !onlyProjectMemoryDirty(status) {
			return errors.New("source repository must be clean except for .sigma-agent/MEMORY.md")
		}
		if requestedPushBranch != "" {
			if err := validateNewPushBranch(repo, requestedPushBranch); err != nil {
				return err
			}
			activePushBranch = requestedPushBranch
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
		if activePushBranch != "" {
			if _, err := gitOutput(workspace, "checkout", "-b", activePushBranch); err != nil {
				return fmt.Errorf("create publish branch %q in task worktree: %w", activePushBranch, err)
			}
		}
		if err := sigmaagent.CopyProjectMemory(repo, workspace); err != nil {
			return fmt.Errorf("copy project memory into task worktree: %w", err)
		}
		metadata, _ := json.MarshalIndent(map[string]string{
			"repo": repo, "starting_commit": startingCommit, "push_branch": activePushBranch,
			"model_provider": activeModelSelection.Provider, "model_id": activeModelSelection.Model,
		}, "", "  ")
		if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), metadata, 0o600); err != nil {
			return err
		}
	} else {
		var metadata struct {
			Repo           string `json:"repo"`
			StartingCommit string `json:"starting_commit"`
			PushBranch     string `json:"push_branch"`
			ModelProvider  string `json:"model_provider"`
			ModelID        string `json:"model_id"`
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
		if !modelExplicit && metadata.ModelProvider != "" && metadata.ModelID != "" {
			activeModelSelection = agent.ModelSelection{Provider: metadata.ModelProvider, Model: metadata.ModelID}
		}
		if !modelExplicit && restoredSnapshot != nil {
			if providerText, modelID, ok := strings.Cut(restoredSnapshot.ModelID, "/"); ok {
				if provider, valid := agent.NormalizeModelProvider(providerText); valid && modelID != "" {
					activeModelSelection = agent.ModelSelection{Provider: provider, Model: modelID}
				}
			}
		}
		if requestedPushBranch != "" && requestedPushBranch != metadata.PushBranch {
			return errors.New("--push-branch must match the branch configured when this session was created")
		}
		if metadata.PushBranch != "" {
			currentBranch, err := gitOutput(workspace, "branch", "--show-current")
			if err != nil || strings.TrimSpace(currentBranch) != metadata.PushBranch {
				return errors.New("saved publish branch does not match the session worktree")
			}
			if requestedPushBranch != "" {
				activePushBranch = metadata.PushBranch
			}
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

	model, err := agent.NewConfiguredModel(activeModelSelection)
	if err != nil {
		return err
	}
	if configurable, ok := model.(interface {
		ConfigureRuntime(int, int, time.Duration)
	}); ok {
		configurable.ConfigureRuntime(runtimeConfig.MaxRetries, runtimeConfig.ModelOutputTokens, time.Duration(runtimeConfig.ModelTimeoutMS)*time.Millisecond)
	}
	backgroundTasks, err := sigmaagent.NewBackgroundCommandManager(workspace, runtimeConfig.CommandExecutionImage, filepath.Join(sessionDir, "background_tasks"), startingCommit, runtimeConfig.AllowContainerNetwork)
	if err != nil {
		return fmt.Errorf("configure background task manager: %w", err)
	}
	tools, err := sigmaagent.BuiltinToolsWithNetwork(workspace, runtimeConfig.GoTestImage, runtimeConfig.MaxRetrievalQueryBytes, model, toolOutputStore, backgroundTasks, runtimeConfig.AllowContainerNetwork, startingCommit)
	if err != nil {
		return err
	}
	tools = append(tools, agent.CompactContextTool())
	if len(runtimeConfig.CommandTools) > 0 {
		plugins := make([]agent.CommandPlugin, 0, len(runtimeConfig.CommandTools))
		for _, plugin := range runtimeConfig.CommandTools {
			plugins = append(plugins, agent.CommandPlugin{
				Name: plugin.Name, Description: plugin.Description, InputSchema: plugin.InputSchema,
				Command: plugin.Command, Args: append([]string(nil), plugin.Args...), ModifiesWorkspace: plugin.ModifiesWorkspace,
			})
		}
		tools = append(tools, agent.CommandPluginTools(workspace, plugins)...)
	}
	commandTools, err := sigmaagent.CommandExecutionToolsWithEnvironmentPreparation(workspace, runtimeConfig.CommandExecutionImage, backgroundTasks, runtimeConfig.AllowContainerNetwork, startingCommit)
	if err != nil {
		return fmt.Errorf("configure command execution tool: %w", err)
	}
	tools = append(tools, commandTools...)
	if activePushBranch != "" {
		tools = append(tools, sigmaagent.GitPublishToolsWithBackground(workspace, activePushBranch, backgroundTasks)...)
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
		tools = append(tools, projectGuidance.SkillTool(), projectGuidance.SkillResourceTool())
	}
	if runtimeConfig.WebSearchEnabled {
		searchProvider, ok := model.(interface {
			WebSearch(context.Context, string) (string, error)
		})
		if !ok {
			return fmt.Errorf("web_search is only supported with the MiniMax provider; current model is %s/%s", activeModelSelection.Provider, activeModelSelection.Model)
		}
		tools = append(tools, sigmaagent.WebSearchTool(searchProvider.WebSearch))
	}
	githubMCPEnabled := githubMCP && agent.GitHubMCPConfigured()
	if githubMCPEnabled {
		githubOptions := agent.GitHubMCPOptions{}
		if githubWrite {
			remoteURL, remoteErr := gitOutput(repo, "remote", "get-url", "origin")
			if remoteErr != nil {
				return fmt.Errorf("resolve origin for pull request creation: %w", remoteErr)
			}
			owner, repository, parseErr := parseGitHubRepository(remoteURL)
			if parseErr != nil {
				return parseErr
			}
			githubOptions = agent.GitHubMCPOptions{AllowCreatePullRequest: true, Owner: owner, Repository: repository, HeadBranch: activePushBranch}
		}
		client, err := agent.StartGitHubMCP(context.Background(), githubOptions)
		if err != nil {
			return err
		}
		defer func() {
			if err := client.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "warning: close GitHub MCP server:", err)
			}
		}()
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
	systemPrompt += "\n\nCONTEXT COMPACTION: You may call compact_context by itself when earlier conversation or tool output has become long and a concise handoff can preserve the active task. It makes an additional model request and is lossy, so use it selectively; the recent turns and full session record are retained."
	if activePushBranch != "" {
		systemPrompt += fmt.Sprintf("\n\nGit publishing is explicitly enabled for branch %q only. Review the diff and run relevant tests before committing. Commit and push only when the user explicitly requested it. Use git_commit before git_push; git_push can only target this branch on origin and cannot force-push or create a pull request.", activePushBranch)
	}
	if githubWrite {
		systemPrompt += "\n\nGitHub pull request creation is explicitly enabled only for this repository and the configured feature branch. Create a pull request only when the user explicitly asks, after the branch is pushed and verification is complete. Do not merge it."
	}
	if newSession {
		session = &agent.Session{ID: sessionID, System: systemPrompt, Store: store}
	} else {
		session, err = agent.RestoreSession(*restoredSnapshot, model, registry, store)
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
	session.Context = agent.WindowContextManager{
		MaxTokens:                 runtimeConfig.MaxContextTokens,
		CompactionThresholdTokens: runtimeConfig.EffectiveContextCompactionThresholdTokens(),
		SummaryTokens:             runtimeConfig.SummaryTokens,
	}
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
		result, interactiveErr = interactiveSession(ctx, session, workspace, task, !newSession, startingCommit, runtimeConfig, projectGuidance, setupInput...)
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
	patch, diffErr := sigmaagent.WorkspaceDiff(workspace, startingCommit)
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
	memoryWrite := sigmaagent.MemoryWriteReport{Status: "disabled"}
	if runtimeConfig.MemoryEnabled && runtimeConfig.MemoryAutoWrite {
		switch {
		case interactive:
			memoryWrite.Status = "skipped_interactive"
		case runErr != nil:
			memoryWrite.Status = "skipped_unsuccessful_run"
		default:
			memoryWrite = sigmaagent.CaptureSuccessfulRunMemory(context.Background(), model, repo, runID, task, patch, session.Messages())
			event := agent.Event{At: time.Now().UTC(), Type: "memory_write_completed", Session: sessionID}
			if memoryWrite.Warning != "" {
				event.Error = memoryWrite.Warning
			}
			eventSink(event)
		}
	}
	metadata, _ := json.MarshalIndent(map[string]any{
		"run_id": runID, "session_id": sessionID, "repo": repo, "starting_commit": startingCommit, "model": session.Model.ModelID(), "push_branch": activePushBranch,
		"github_write_enabled": githubWrite, "allow_container_network": runtimeConfig.AllowContainerNetwork,
		"github_mcp_enabled":        githubMCPEnabled,
		"tool_output_artifacts_dir": filepath.Join(sessionDir, "tool_outputs"),
		"memory_write":              memoryWrite,
		"turns":                     result.Turns, "tool_calls": result.ToolCalls, "input_tokens": result.InputTokens,
		"output_tokens": result.OutputTokens, "cache_read_input_tokens": result.CacheReadInputTokens,
		"cache_creation_input_tokens": result.CacheCreationInputTokens, "context_compactions": result.ContextCompactions,
		"context_compaction_input_tokens":     result.ContextCompactionInputTokens,
		"context_compaction_output_tokens":    result.ContextCompactionOutputTokens,
		"max_context_tokens":                  runtimeConfig.MaxContextTokens,
		"context_compaction_threshold_tokens": runtimeConfig.EffectiveContextCompactionThresholdTokens(),
		"summary_tokens":                      runtimeConfig.SummaryTokens,
		"finished_at":                         time.Now().UTC(), "error": errorString(runErr),
	}, "", "  ")
	if writeErr := os.WriteFile(filepath.Join(runDir, "run.json"), metadata, 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	if jsonOutput {
		completion := map[string]any{
			"protocol_version": 1, "type": "run_completed", "status": "completed", "run_id": runID, "session_id": sessionID,
			"run_dir": runDir, "patch_path": filepath.Join(runDir, "result.patch"), "push_branch": activePushBranch,
			"turns": result.Turns, "tool_calls": result.ToolCalls,
			"input_tokens": result.InputTokens, "output_tokens": result.OutputTokens,
			"cache_read_input_tokens":          result.CacheReadInputTokens,
			"cache_creation_input_tokens":      result.CacheCreationInputTokens,
			"context_compactions":              result.ContextCompactions,
			"context_compaction_input_tokens":  result.ContextCompactionInputTokens,
			"context_compaction_output_tokens": result.ContextCompactionOutputTokens,
			"memory_write":                     memoryWrite,
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
		if activePushBranch != "" {
			fmt.Fprintf(os.Stderr, "Publish branch: %s (origin)\n", activePushBranch)
		}
		if keep || runErr != nil {
			fmt.Fprintf(os.Stderr, "Task worktree kept at: %s\n", workspace)
		}
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func defaultModelSelection() (agent.ModelSelection, error) {
	selection, err := agent.LoadDefaultModelSelection()
	if err != nil {
		return agent.ModelSelection{}, err
	}
	if selection.Provider == "" {
		modelID := strings.TrimSpace(os.Getenv("SIGMA_AGENT_MODEL"))
		if modelID == "" {
			modelID = agent.DefaultModelForProvider("minimax")
		}
		selection = agent.ModelSelection{Provider: "minimax", Model: modelID}
	}
	return selection, nil
}

func parseModelCommand(value string, current agent.Model) (agent.ModelSelection, string, error) {
	currentSelection := agent.ModelSelection{Provider: "minimax", Model: agent.DefaultModelForProvider("minimax")}
	if current != nil {
		modelID := current.ModelID()
		if providerText, id, ok := strings.Cut(modelID, "/"); ok {
			if provider, valid := agent.NormalizeModelProvider(providerText); valid && id != "" {
				currentSelection = agent.ModelSelection{Provider: provider, Model: id}
			}
		} else if modelID != "" {
			currentSelection.Model = modelID
		}
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "list" {
		return currentSelection, "show", nil
	}
	if value == "save" {
		return currentSelection, "save", nil
	}
	providerText, modelID, ok := strings.Cut(value, "/")
	provider, valid := agent.NormalizeModelProvider(providerText)
	if !valid || !ok || strings.TrimSpace(modelID) == "" {
		return agent.ModelSelection{}, "", errors.New("usage: /model <provider>/<model-id>, /model list, or /model save")
	}
	hasKey, err := agent.ModelProviderHasAPIKey(provider)
	if err != nil {
		return agent.ModelSelection{}, "", err
	}
	if !hasKey {
		return agent.ModelSelection{}, "", fmt.Errorf("no %s API key is configured; restart the interactive session and use /login %s", provider, provider)
	}
	return agent.ModelSelection{Provider: provider, Model: strings.TrimSpace(modelID)}, "switch", nil
}

func onlyProjectMemoryDirty(status string) bool {
	for _, line := range strings.Split(strings.TrimSpace(status), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 || strings.TrimSpace(line[:2]) == "" {
			return false
		}
		path := strings.TrimSpace(line[3:])
		if path != filepath.ToSlash(filepath.Join(".sigma-agent", "MEMORY.md")) {
			return false
		}
	}
	return true
}

func interactiveSession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool, startingCommit string, runtimeConfig config.Runtime, projectGuidance sigmaagent.ProjectGuidance, inputScanners ...*bufio.Scanner) (agent.Result, error) {
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
	fmt.Fprintf(os.Stderr, "\nSigma Agent interactive session %s\nCommands: /help, /model, /diff, /tree, /branch <entry-id>, /skill:<name> <task>, /abort, /exit\nWhile the Agent is running, enter text to steer its next turn.\n", session.ID)
	input := make(chan inputEvent, 1)
	var scanner *bufio.Scanner
	if len(inputScanners) > 0 {
		scanner = inputScanners[0]
	}
	if scanner == nil {
		scanner = bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
	}
	go func() {
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
				fmt.Fprintln(os.Stderr, "Enter a coding instruction. Use /skill:<name> <task> to explicitly invoke a Skill. During an active turn, plain text or /steer <text> queues guidance for the next safe turn. /followup <text> runs after the current task settles. /model shows or switches models between turns; /diff shows the patch; /tree lists conversation branches; /branch <entry-id> moves to an earlier point and starts a new branch; /abort stops the active turn; /exit saves and ends the session.")
			case strings.HasPrefix(line, "/skill:"):
				invocation := strings.TrimPrefix(line, "/skill:")
				name, task, ok := strings.Cut(invocation, " ")
				if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(task) == "" {
					fmt.Fprintln(os.Stderr, "Usage: /skill:<name> <task>")
					continue
				}
				prompt, err := projectGuidance.ExplicitSkillPrompt(strings.TrimSpace(name), task)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Could not invoke Skill: %v\n", err)
					continue
				}
				if activeDone != nil {
					if err := session.Steer(prompt); err != nil {
						fmt.Fprintf(os.Stderr, "Could not queue Skill task: %v\n", err)
					} else {
						fmt.Fprintln(os.Stderr, "Skill task queued for the next safe turn.")
					}
				} else {
					startRun(prompt, false)
				}
			case line == "/tree":
				printSessionTree(session)
			case line == "/branch" || strings.HasPrefix(line, "/branch "):
				entryID := strings.TrimSpace(strings.TrimPrefix(line, "/branch"))
				if activeDone != nil {
					fmt.Fprintln(os.Stderr, "Wait for the current turn to finish before branching.")
				} else if entryID == "" {
					fmt.Fprintln(os.Stderr, "Usage: /branch <entry-id> (see /tree)")
				} else if err := session.BranchFrom(entryID); err != nil {
					fmt.Fprintf(os.Stderr, "Could not branch session: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "Moved to entry %s. Enter a new instruction to continue on this branch.\n", entryID)
				}
			case line == "/model" || strings.HasPrefix(line, "/model "):
				if activeDone != nil {
					fmt.Fprintln(os.Stderr, "Wait for the current turn to finish before changing models.")
					continue
				}
				selection, action, err := parseModelCommand(strings.TrimSpace(strings.TrimPrefix(line, "/model")), session.Model)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					continue
				}
				switch action {
				case "show":
					printModelOptions(selection)
				case "save":
					if err := agent.SaveDefaultModelSelection(selection); err != nil {
						fmt.Fprintf(os.Stderr, "Could not save default model: %v\n", err)
					} else {
						fmt.Fprintf(os.Stderr, "Saved default model %s/%s.\n", selection.Provider, selection.Model)
					}
				case "switch":
					model, err := agent.NewConfiguredModel(selection)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						continue
					}
					if configurable, ok := model.(interface {
						ConfigureRuntime(int, int, time.Duration)
					}); ok {
						configurable.ConfigureRuntime(runtimeConfig.MaxRetries, runtimeConfig.ModelOutputTokens, time.Duration(runtimeConfig.ModelTimeoutMS)*time.Millisecond)
					}
					if err := session.SetModel(model); err != nil {
						fmt.Fprintf(os.Stderr, "Could not switch model: %v\n", err)
						continue
					}
					fmt.Fprintf(os.Stderr, "Switched to %s/%s.\n", selection.Provider, selection.Model)
				}
			case line == "/diff":
				diff, err := gitOutput(workspace, "diff", "--stat", startingCommit)
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
	total.CacheReadInputTokens += current.CacheReadInputTokens
	total.CacheCreationInputTokens += current.CacheCreationInputTokens
	total.ContextCompactions += current.ContextCompactions
	total.ContextCompactionInputTokens += current.ContextCompactionInputTokens
	total.ContextCompactionOutputTokens += current.ContextCompactionOutputTokens
}

func printSessionTree(session *agent.Session) {
	entries := session.Entries()
	head := session.HeadID()
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "This session has no conversation entries yet.")
		return
	}
	fmt.Fprintln(os.Stderr, "Session history (use the full entry ID with /branch):")
	for _, entry := range entries {
		marker := " "
		if entry.ID == head {
			marker = "*"
		}
		detail := entry.Type
		if entry.Type == "model_change" {
			detail += ": " + entry.ModelID
		} else if entry.Message != nil {
			for _, block := range entry.Message.Content {
				if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
					detail += ": " + truncateText(strings.TrimSpace(block.Text), 88)
					break
				}
			}
		}
		fmt.Fprintf(os.Stderr, "%s %s  %s  %s\n", marker, entry.ID, entry.CreatedAt.Local().Format("2006-01-02 15:04:05"), detail)
	}
}

func truncateText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
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

func validateNewPushBranch(repo, branch string) error {
	if strings.TrimSpace(branch) != branch || branch == "" || strings.HasPrefix(branch, "-") {
		return errors.New("--push-branch must be a non-empty Git branch name")
	}
	switch strings.ToLower(branch) {
	case "main", "master", "develop", "trunk":
		return fmt.Errorf("refusing to publish directly to protected branch name %q", branch)
	}
	checked, err := gitOutput(repo, "check-ref-format", "--branch", branch)
	if err != nil || strings.TrimSpace(checked) != branch {
		return fmt.Errorf("invalid --push-branch %q", branch)
	}
	if _, err := gitOutput(repo, "remote", "get-url", "origin"); err != nil {
		return errors.New("--push-branch requires a Git remote named origin")
	}
	if defaultBranch, err := gitOutput(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		defaultBranch = strings.TrimPrefix(strings.TrimSpace(defaultBranch), "origin/")
		if branch == defaultBranch {
			return fmt.Errorf("refusing to publish directly to origin's default branch %q", branch)
		}
	}
	if _, err := gitOutput(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fmt.Errorf("local branch %q already exists; choose a new feature branch", branch)
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return fmt.Errorf("check local branch %q: %w", branch, err)
		}
	}
	remoteRef, err := gitOutput(repo, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("cannot confirm whether origin branch %q is available; check local Git credentials and network access", branch)
	}
	if strings.TrimSpace(remoteRef) != "" {
		return fmt.Errorf("remote branch %q already exists; choose a new feature branch", branch)
	}
	return nil
}

func parseGitHubRepository(remote string) (string, string, error) {
	remote = strings.TrimSpace(remote)
	path := ""
	if strings.HasPrefix(remote, "git@github.com:") {
		path = strings.TrimPrefix(remote, "git@github.com:")
	} else {
		parsed, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
			return "", "", errors.New("--github-write currently supports an origin remote hosted on github.com")
		}
		path = strings.TrimPrefix(parsed.Path, "/")
	}
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot read owner/repository from origin URL %q", remote)
	}
	return parts[0], parts[1], nil
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
