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

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	modelconfig "github.com/sdaAlbert/sigma-agent/internal/model"
	"github.com/sdaAlbert/sigma-agent/internal/server"
	"github.com/sdaAlbert/sigma-agent/internal/sigmaagent"
	"github.com/sdaAlbert/sigma-agent/internal/workflow"
)

func main() {
	serve := flag.Bool("serve", false, "run the HTTP/SSE task service")
	indexMemory := flag.Bool("index-memory", false, "synchronize repository memory into Qdrant")
	configPath := flag.String("config", "config.json", "JSON file with tunable runtime limits")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address when --serve is enabled")
	maxConcurrent := flag.Int("max-concurrent", 2, "maximum concurrent agent tasks when --serve is enabled")
	repoArg := flag.String("repo", "", "workspace folder to work in (defaults to the current directory in interactive setup)")
	task := flag.String("task", "", "coding task text for the agent (not a file path), or an additional instruction when resuming")
	interactive := flag.Bool("interactive", false, "start a persistent terminal conversation (default when no task or resume is given)")
	tui := flag.Bool("tui", false, "start the integrated terminal UI for setup and Agent interaction")
	jsonOutput := flag.Bool("json", false, "write Agent lifecycle events and the final run result as JSONL to stdout")
	resumeID := flag.String("resume", "", "resume a persisted session by ID; omit --task to continue it")
	githubMCP := flag.Bool("github-mcp", true, "enable read-only GitHub MCP tools when credentials are configured (default true)")
	allowContainerNetwork := flag.Bool("allow-container-network", false, "advanced: allow container internet access at startup; interactive users can use /network")
	maxTurns := flag.Int("max-turns", 0, "override config: maximum assistant turns")
	maxToolCalls := flag.Int("max-tool-calls", 0, "override config: maximum tool calls per task")
	contextTokens := flag.Int("context-tokens", 0, "override config: maximum estimated context tokens")
	contextCompactionTokens := flag.Int("context-compaction-tokens", 0, "override config: proactively compact context above this estimated token count")
	modelOutputTokens := flag.Int("model-output-tokens", 0, "override config: maximum model output tokens")
	modelArg := flag.String("model", "", "override saved/session model using provider/model-id syntax")
	showVersion := flag.Bool("version", false, "print the Sigma Agent version and exit")
	sessionID := flag.String("session-id", "", "use this id for a new session")
	eventFile := flag.String("event-file", "", "append agent lifecycle events to this JSONL file")
	messageFile := flag.String("message-file", "", "read durable task messages from this JSONL file")
	installSource := flag.String("install", "", "install a package (npm:<name>, git:<repo>, or a local path) and exit")
	uninstallSource := flag.String("uninstall", "", "remove a package from the registry and exit")
	listInstalled := flag.Bool("list-packages", false, "list the installed packages and exit")
	listMCPServers := flag.Bool("list-mcp", false, "connect to the configured MCP servers, list their tools, and exit")
	localScope := flag.Bool("local", false, "install or remove the package in the project scope instead of the user scope")
	flag.Parse()
	if *showVersion {
		fmt.Printf("sigma-agent v%s\n", sigmaAgentVersion)
		return
	}
	if *installSource != "" && *uninstallSource != "" {
		fmt.Fprintln(os.Stderr, "sigma-agent: --install and --uninstall cannot be combined")
		os.Exit(2)
	}
	if (*installSource != "" || *uninstallSource != "") && *localScope && strings.TrimSpace(*repoArg) == "" {
		fmt.Fprintln(os.Stderr, "sigma-agent: --local requires --repo")
		os.Exit(2)
	}
	if *installSource != "" || *uninstallSource != "" || *listInstalled || *listMCPServers {
		if err := runPackageCommand(*installSource, *uninstallSource, *listInstalled, *listMCPServers, *localScope, *repoArg); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	runtimeConfig, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(2)
	}
	requestedModel := modelconfig.Selection{}
	modelExplicit := false
	if strings.TrimSpace(*modelArg) != "" {
		selection, action, modelErr := parseModelCommand(*modelArg, nil)
		if modelErr != nil || action != "switch" {
			if modelErr == nil {
				modelErr = errors.New("--model requires provider/model-id syntax")
			}
			fmt.Fprintln(os.Stderr, "sigma-agent:", modelErr)
			os.Exit(2)
		}
		requestedModel = selection
		modelExplicit = true
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
		var memoryCount int
		if runtimeConfig.MemoryFile != "" {
			memoryCount, err = sigmaagent.IndexProjectMemoryFile(runtimeConfig.MemoryFile, runtimeConfig.MemoryRepositoryID)
		} else {
			memoryCount, err = sigmaagent.IndexRepositoryMemory(*repoArg)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		fmt.Printf("Indexed %d repository memory entries into Qdrant.\n", memoryCount)
		return
	}
	if *tui && *serve {
		fmt.Fprintln(os.Stderr, "sigma-agent: --tui cannot be combined with --serve")
		os.Exit(2)
	}
	if *serve {
		if err := server.ListenAndServe(*addr, *maxConcurrent, *configPath); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	interactiveMode := *tui || *interactive || (strings.TrimSpace(*task) == "" && strings.TrimSpace(*resumeID) == "")
	if *jsonOutput && interactiveMode {
		fmt.Fprintln(os.Stderr, "sigma-agent: --json requires a one-shot --task or --resume invocation; interactive mode is not supported")
		os.Exit(2)
	}
	if *allowContainerNetwork {
		runtimeConfig.AllowContainerNetwork = true
	}
	if interactiveMode {
		defaultRepo := strings.TrimSpace(*repoArg)
		if defaultRepo == "" {
			defaultRepo, err = os.Getwd()
			if err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent:", err)
				os.Exit(1)
			}
		}
		selection := requestedModel
		if !modelExplicit {
			selection, err = modelconfig.LoadDefaultSelection()
			if err != nil {
				fmt.Fprintln(os.Stderr, "sigma-agent:", err)
				os.Exit(1)
			}
		}
		if selection.Provider == "" {
			selection = modelconfig.Selection{Provider: "minimax", Model: modelconfig.DefaultModel("minimax")}
		}
		setup := interactiveSetupOptions{
			repo: defaultRepo, sessionID: *sessionID, resumeID: *resumeID,
			allowNetwork: runtimeConfig.AllowContainerNetwork,
			githubMCP:    *githubMCP, model: selection, modelExplicit: modelExplicit,
			initialPrompt: *task,
		}
		if err := startTUI(setup, func(options interactiveSetupOptions, bridge *tuiBridge) error {
			return run(options.repo, options.initialPrompt, runtimeConfig, options.resumeID, options.githubMCP, options.sessionID, *eventFile, *messageFile, true, false, options.model, options.modelExplicit, nil, bridge)
		}); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent:", err)
			os.Exit(1)
		}
		return
	}
	if strings.TrimSpace(*repoArg) == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*repoArg, *task, runtimeConfig, *resumeID, *githubMCP, *sessionID, *eventFile, *messageFile, interactiveMode, *jsonOutput, requestedModel, modelExplicit, nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent:", err)
		os.Exit(1)
	}
}

func run(repoArg, task string, runtimeConfig config.Runtime, resumeID string, githubMCP bool, requestedSessionID, requestedEventFile, requestedMessageFile string, interactive, jsonOutput bool, requestedModel modelconfig.Selection, modelExplicit bool, setupInput *bufio.Scanner, tui *tuiBridge) error {
	repo, err := filepath.Abs(repoArg)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	if info, statErr := os.Stat(repo); statErr != nil || !info.IsDir() {
		if statErr != nil {
			return fmt.Errorf("resolve workspace folder: %w", statErr)
		}
		return errors.New("workspace path must be a directory")
	}
	gitRoot, gitErr := gitOutput(repo, "rev-parse", "--show-toplevel")
	gitDiffAvailable := gitErr == nil && sigmaagent.HasGitCommit(repo)
	if gitErr == nil {
		repo = filepath.Clean(strings.TrimSpace(gitRoot))
	}
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
	workspace := repo
	var restoredSnapshot *agent.SessionSnapshot
	if !newSession {
		snapshot, loadErr := store.Load(sessionID)
		if loadErr != nil {
			return fmt.Errorf("load session %s: %w", sessionID, loadErr)
		}
		restoredSnapshot = &snapshot
	}
	startingCommit := ""
	workspaceMode := "direct"
	var runErr error
	if newSession {
		if gitDiffAvailable {
			commit, commitErr := gitOutput(repo, "rev-parse", "HEAD")
			if commitErr != nil {
				return commitErr
			}
			startingCommit = strings.TrimSpace(commit)
		}
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			return err
		}
		metadata, _ := json.MarshalIndent(map[string]any{
			"repo": repo, "starting_commit": startingCommit,
			"git_diff_available": gitDiffAvailable,
			"workspace_mode":     workspaceMode,
			"model_provider":     activeModelSelection.Provider, "model_id": activeModelSelection.Model,
		}, "", "  ")
		if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), metadata, 0o600); err != nil {
			return err
		}
	} else {
		var metadata struct {
			Repo             string `json:"repo"`
			StartingCommit   string `json:"starting_commit"`
			GitDiffAvailable *bool  `json:"git_diff_available"`
			WorkspaceMode    string `json:"workspace_mode"`
			ModelProvider    string `json:"model_provider"`
			ModelID          string `json:"model_id"`
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
		if metadata.WorkspaceMode != "direct" {
			// Keep support for resuming sessions created before the direct-workspace
			// migration. New sessions always use repo as the active workspace.
			workspaceMode = "isolated"
			workspace = filepath.Join(sessionDir, "workspace")
		}
		if !modelExplicit && metadata.ModelProvider != "" && metadata.ModelID != "" {
			activeModelSelection = modelconfig.Selection{Provider: metadata.ModelProvider, Model: metadata.ModelID}
		}
		if !modelExplicit && restoredSnapshot != nil {
			if providerText, modelID, ok := strings.Cut(restoredSnapshot.ModelID, "/"); ok {
				if provider, valid := modelconfig.NormalizeProvider(providerText); valid && modelID != "" {
					activeModelSelection = modelconfig.Selection{Provider: provider, Model: modelID}
				}
			}
		}
		startingCommit = metadata.StartingCommit
		if metadata.GitDiffAvailable != nil {
			gitDiffAvailable = *metadata.GitDiffAvailable
		} else {
			gitDiffAvailable = startingCommit != ""
		}
		if _, err := os.Stat(workspace); err != nil {
			return fmt.Errorf("session workspace is unavailable; start a new task or restore the workspace: %w", err)
		}
		if gitDiffAvailable {
			worktreeRoot, gitErr := gitOutput(workspace, "rev-parse", "--show-toplevel")
			if gitErr != nil || filepath.Clean(strings.TrimSpace(worktreeRoot)) != filepath.Clean(workspace) {
				return errors.New("session workspace is not the original Git worktree")
			}
		}
	}
	memorySourceHash := ""
	if runtimeConfig.MemoryFile != "" {
		memorySourceHash, err = sigmaagent.CopyProjectMemoryFromFile(runtimeConfig.MemoryFile, workspace, runtimeConfig.MemoryRepositoryID)
		if err != nil {
			return fmt.Errorf("load configured shared memory into active workspace: %w", err)
		}
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
	var workflowHost *workflow.Host
	eventSink := func(event agent.Event) {
		traceMu.Lock()
		_ = json.NewEncoder(traceFile).Encode(event)
		traceMu.Unlock()
		if jsonOutput {
			stdoutMu.Lock()
			protocolEvent := event
			protocolEvent.Args = nil
			protocolEvent.ResultSummary = ""
			protocolEvent.ResultOutput = ""
			protocolEvent.ToolCallID = ""
			protocolEvent.Status = ""
			protocolEvent.CompactionSummary = ""
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"protocol_version": 1, "type": "agent_event", "event": protocolEvent})
			stdoutMu.Unlock()
		} else if tui != nil {
			tui.sendEvent(event)
		} else if event.Type == "model_text_delta" {
			_, _ = io.WriteString(os.Stdout, event.Text)
		} else if event.Type == "tool_execution_start" {
			fmt.Fprintf(os.Stderr, "\n[tool] %s started\n", event.Tool)
		} else if event.Type == "tool_execution_end" {
			fmt.Fprintf(os.Stderr, "[tool] %s finished (%d ms)\n", event.Tool, event.Duration)
		} else if event.Error != "" {
			fmt.Fprintf(os.Stderr, "\n[%s] %s\n", event.Type, event.Error)
		}
		if workflowHost != nil {
			if projected, ok := workflowEvent(event); ok {
				workflowHost.Dispatch(projected)
			}
		}
	}

	model, err := modelconfig.NewConfigured(activeModelSelection)
	if err != nil {
		return err
	}
	configureModelRuntime(model, runtimeConfig)
	backgroundTasks, err := sigmaagent.NewBackgroundCommandManager(workspace, runtimeConfig.CommandExecutionImage, filepath.Join(sessionDir, "background_tasks"), startingCommit, runtimeConfig.AllowContainerNetwork)
	if err != nil {
		return fmt.Errorf("configure background task manager: %w", err)
	}
	tools, err := sigmaagent.BuiltinToolsWithNetworkAndMemoryRepositoryID(workspace, runtimeConfig.MaxRetrievalQueryBytes, model, toolOutputStore, backgroundTasks, runtimeConfig.AllowContainerNetwork, runtimeConfig.MemoryRepositoryID, startingCommit)
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
	commandTools, err := sigmaagent.CommandExecutionToolsWithEnvironmentPreparationAndFilesystemScope(workspace, runtimeConfig.CommandExecutionImage, backgroundTasks, runtimeConfig.AllowContainerNetwork, runtimeConfig.CommandFilesystemScope, startingCommit)
	if err != nil {
		return fmt.Errorf("configure command execution tool: %w", err)
	}
	tools = append(tools, commandTools...)
	tools = append(tools, agent.ReadToolOutputTool(toolOutputStore, runtimeConfig.MaxToolResultBytes))
	packageScopes, err := packageScopes(repo)
	if err != nil {
		return fmt.Errorf("resolve package scopes: %w", err)
	}
	var extensionPaths []string
	if !runtimeConfig.DisableWorkflowExtensions {
		extensionPaths, err = packageExtensions(packageScopes)
		if err != nil {
			return fmt.Errorf("discover workflow extensions: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "sigma-agent: workflow extensions disabled by run config")
	}
	var extensionHandler workflow.Handler = consoleWorkflowHandler{}
	if tui != nil {
		extensionHandler = tui
	}
	workflowHost, closeWorkflow, err := workflow.LoadJavaScript(context.Background(), extensionPaths, extensionHandler)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sigma-agent: warning: workflow extensions are unavailable:", err)
		workflowHost = workflow.NewHost(extensionHandler)
		closeWorkflow = func() error { return nil }
	}
	defer func() {
		if err := closeWorkflow(); err != nil {
			fmt.Fprintln(os.Stderr, "sigma-agent: warning: close workflow extensions:", err)
		}
	}()
	mcpTools, shutdownMCPServers, err := startConfiguredMCPServers(context.Background(), packageScopes, runtimeConfig.DisableConfiguredMCPServers)
	if runtimeConfig.DisableConfiguredMCPServers {
		fmt.Fprintln(os.Stderr, "sigma-agent: configured MCP servers disabled by run config")
	}
	if err != nil {
		// A broken MCP configuration must not take the session down. The
		// failure is reported and the run continues without those tools.
		fmt.Fprintln(os.Stderr, "sigma-agent: warning: MCP servers are unavailable:", err)
		shutdownMCPServers = func() {}
		mcpTools = nil
	}
	defer shutdownMCPServers()
	registerAgentTools(workflowHost, mcpTools)
	contributedSkills, err := packageSkills(packageScopes)
	if err != nil {
		return fmt.Errorf("load package skills: %w", err)
	}
	projectGuidance, err := sigmaagent.LoadProjectGuidanceWithProfile(workspace, contributedSkills, runtimeConfig.ProjectInstructionsFile)
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
		client, err := agent.StartGitHubMCP(context.Background())
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
		registerAgentTools(workflowHost, githubTools)
	}
	tools = append(tools, workflowAgentTools(workflowHost, tools)...)
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
	if workspaceMode == "direct" {
		systemPrompt += "\n\nYou are working directly in the user's selected folder. File changes take effect there immediately. Preserve unrelated existing files and changes. If this folder has Git with a commit, review the diff before finishing; otherwise explain that Git diff is unavailable. Never revert user changes unless the user asks."
	}
	systemPrompt += "\n\nCONTEXT COMPACTION: You may call compact_context by itself when earlier conversation or tool output has become long and a concise handoff can preserve the active task. It makes an additional model request and is lossy, so use it selectively; the recent turns and full session record are retained."
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
	if runtimeConfig.SkillsEnabled {
		session.RequiredSkills = projectGuidance.MatchingSkillNames(task)
	}
	session.ToolOutputs = toolOutputStore
	session.BeforeTool = configuredToolPolicy(runtimeConfig)
	session.Limits = agent.Limits{MaxTurns: runtimeConfig.MaxTurns, MaxToolCalls: runtimeConfig.MaxToolCalls, MaxRepeatedFailedCommands: runtimeConfig.MaxRepeatedFailedCommands, MaxToolResultBytes: runtimeConfig.MaxToolResultBytes, ToolTimeout: time.Duration(runtimeConfig.ToolTimeoutMS) * time.Millisecond}
	session.Context = agent.WindowContextManager{
		MaxTokens:                 runtimeConfig.MaxContextTokens,
		CompactionThresholdTokens: runtimeConfig.EffectiveContextCompactionThresholdTokens(),
		SummaryTokens:             runtimeConfig.SummaryTokens,
	}
	session.OnEvent = eventSink
	workflowHost.Dispatch(workflow.Event{Type: "session_start", SessionID: session.ID})
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
		if tui != nil {
			result, interactiveErr = interactiveTUISession(ctx, session, workspace, task, !newSession, startingCommit, runtimeConfig, projectGuidance, workflowHost, tui)
		} else {
			var scanners []*bufio.Scanner
			if setupInput != nil {
				scanners = append(scanners, setupInput)
			}
			result, interactiveErr = interactiveSession(ctx, session, workspace, task, !newSession, startingCommit, runtimeConfig, projectGuidance, workflowHost, scanners...)
		}
		if interactiveErr != nil {
			runErr = interactiveErr
		}
	} else if task != "" {
		result, runErr = session.Prompt(ctx, task)
	} else {
		result, runErr = session.Continue(ctx)
	}
	workflowHost.Dispatch(workflow.Event{Type: "session_end", SessionID: session.ID, Error: errorText(runErr)})
	patch := ""
	var diffErr error
	if gitDiffAvailable {
		patch, diffErr = sigmaagent.WorkspaceDiff(workspace, startingCommit)
	}
	if result.VerificationStatus == "no_changes" && strings.TrimSpace(patch) != "" {
		// A resumed session can start this invocation with a candidate patch
		// already present. The current invocation's turn history alone cannot
		// establish that those changes were verified.
		result.VerificationStatus = "incomplete"
	}
	if diffErr != nil && runErr == nil {
		runErr = fmt.Errorf("collect patch: %w", diffErr)
	}
	patchSaved := false
	if writeErr := os.WriteFile(filepath.Join(runDir, "result.patch"), []byte(patch), 0o600); writeErr != nil {
		if runErr == nil {
			runErr = writeErr
		}
	} else {
		patchSaved = true
	}
	transcript, _ := json.MarshalIndent(session.Messages(), "", "  ")
	if writeErr := os.WriteFile(filepath.Join(runDir, "transcript.json"), transcript, 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	memoryWrite := sigmaagent.MemoryWriteReport{Status: "disabled"}
	if runtimeConfig.MemoryEnabled && runtimeConfig.MemoryAutoWrite {
		switch {
		case !gitDiffAvailable:
			memoryWrite.Status = "skipped_non_git_workspace"
		case interactive:
			memoryWrite.Status = "skipped_interactive"
		case runErr != nil:
			memoryWrite.Status = "skipped_unsuccessful_run"
		case result.VerificationStatus == "incomplete" || result.VerificationStatus == "test_failed":
			memoryWrite.Status = "skipped_unverified_run"
		default:
			memoryWrite = sigmaagent.CaptureSuccessfulRunMemoryWithProfile(context.Background(), model, repo, runtimeConfig.MemoryFile, runtimeConfig.MemoryRepositoryID, runID, task, patch, session.Messages())
			event := agent.Event{At: time.Now().UTC(), Type: "memory_write_completed", Session: sessionID}
			if memoryWrite.Warning != "" {
				event.Error = memoryWrite.Warning
			}
			eventSink(event)
		}
	}
	metadata, _ := json.MarshalIndent(map[string]any{
		"run_id": runID, "session_id": sessionID, "repo": repo, "starting_commit": startingCommit, "model": session.Model.ModelID(), "push_branch": "",
		"verification_status":   result.VerificationStatus,
		"git_diff_available":    gitDiffAvailable,
		"candidate_patch_saved": patchSaved,
		"final_diff_collected":  diffErr == nil,
		"workspace_mode":        workspaceMode,
		"github_write_enabled":  false, "allow_container_network": runtimeConfig.AllowContainerNetwork,
		"command_filesystem_scope":        runtimeConfig.CommandFilesystemScope,
		"github_mcp_enabled":              githubMCPEnabled,
		"configured_mcp_servers_disabled": runtimeConfig.DisableConfiguredMCPServers,
		"workflow_extensions_disabled":    runtimeConfig.DisableWorkflowExtensions,
		"model_reasoning_effort":          runtimeConfig.ModelReasoningEffort,
		"model_output_tokens":             runtimeConfig.ModelOutputTokens,
		"project_instructions_file":       projectGuidance.ExternalInstructionsPath,
		"project_instructions_sha256":     projectGuidance.ExternalInstructionsSHA256,
		"memory_file":                     runtimeConfig.MemoryFile,
		"memory_repository_id":            runtimeConfig.MemoryRepositoryID,
		"memory_source_sha256_at_start":   memorySourceHash,
		"tool_output_artifacts_dir":       filepath.Join(sessionDir, "tool_outputs"),
		"memory_write":                    memoryWrite,
		"turns":                           result.Turns, "tool_calls": result.ToolCalls, "input_tokens": result.InputTokens,
		"output_tokens": result.OutputTokens, "cache_read_input_tokens": result.CacheReadInputTokens,
		"cache_miss_input_tokens":     result.CacheMissInputTokens,
		"cache_creation_input_tokens": result.CacheCreationInputTokens, "context_compactions": result.ContextCompactions,
		"context_compaction_input_tokens":     result.ContextCompactionInputTokens,
		"context_compaction_output_tokens":    result.ContextCompactionOutputTokens,
		"max_context_tokens":                  runtimeConfig.MaxContextTokens,
		"context_compaction_threshold_tokens": runtimeConfig.EffectiveContextCompactionThresholdTokens(),
		"summary_tokens":                      runtimeConfig.SummaryTokens,
		"max_turns":                           runtimeConfig.MaxTurns,
		"max_tool_calls":                      runtimeConfig.MaxToolCalls,
		"max_repeated_failed_commands":        runtimeConfig.MaxRepeatedFailedCommands,
		"finished_at":                         time.Now().UTC(), "error": errorString(runErr),
	}, "", "  ")
	if writeErr := os.WriteFile(filepath.Join(runDir, "run.json"), metadata, 0o600); writeErr != nil && runErr == nil {
		runErr = writeErr
	}
	if jsonOutput {
		completion := map[string]any{
			"protocol_version": 1, "type": "run_completed", "status": "completed", "run_id": runID, "session_id": sessionID,
			"run_dir": runDir, "patch_path": filepath.Join(runDir, "result.patch"), "push_branch": "",
			"verification_status":   result.VerificationStatus,
			"candidate_patch_saved": patchSaved,
			"final_diff_collected":  diffErr == nil,
			"turns":                 result.Turns, "tool_calls": result.ToolCalls,
			"input_tokens": result.InputTokens, "output_tokens": result.OutputTokens,
			"cache_read_input_tokens":          result.CacheReadInputTokens,
			"cache_miss_input_tokens":          result.CacheMissInputTokens,
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
		fmt.Fprintf(os.Stderr, "Workspace: %s\n", workspace)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func defaultModelSelection() (modelconfig.Selection, error) {
	selection, err := modelconfig.LoadDefaultSelection()
	if err != nil {
		return modelconfig.Selection{}, err
	}
	if selection.Provider == "" {
		modelID := strings.TrimSpace(os.Getenv("SIGMA_AGENT_MODEL"))
		if modelID == "" {
			modelID = modelconfig.DefaultModel("minimax")
		}
		selection = modelconfig.Selection{Provider: "minimax", Model: modelID}
	}
	return selection, nil
}

func parseModelCommand(value string, current agent.Model) (modelconfig.Selection, string, error) {
	currentSelection := modelconfig.Selection{Provider: "minimax", Model: modelconfig.DefaultModel("minimax")}
	if current != nil {
		modelID := current.ModelID()
		if providerText, id, ok := strings.Cut(modelID, "/"); ok {
			if provider, valid := modelconfig.NormalizeProvider(providerText); valid && id != "" {
				currentSelection = modelconfig.Selection{Provider: provider, Model: id}
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
	provider, valid := modelconfig.NormalizeProvider(providerText)
	if !valid || !ok || strings.TrimSpace(modelID) == "" {
		return modelconfig.Selection{}, "", errors.New("usage: /model <provider>/<model-id>, /model list, or /model save")
	}
	hasKey, err := modelconfig.ProviderHasAPIKey(provider)
	if err != nil {
		return modelconfig.Selection{}, "", err
	}
	if !hasKey {
		return modelconfig.Selection{}, "", fmt.Errorf("no %s API key is configured; use /login to choose a credential", provider)
	}
	return modelconfig.Selection{Provider: provider, Model: strings.TrimSpace(modelID)}, "switch", nil
}

func interactiveSession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool, startingCommit string, runtimeConfig config.Runtime, projectGuidance sigmaagent.ProjectGuidance, extensions *workflow.Host, inputScanners ...*bufio.Scanner) (agent.Result, error) {
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
	commandNames := []string{"/help", "/model", "/diff", "/tree [entry-id]", "/skill:<name> <task>", "/abort", "/exit"}
	for _, command := range extensions.Commands() {
		commandNames = append(commandNames, "/"+command.Name)
	}
	fmt.Fprintf(os.Stderr, "\nSigma Agent interactive session %s\nCommands: %s\nWhile the Agent is running, enter text to steer its next turn.\n", session.ID, strings.Join(commandNames, ", "))
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
			if strings.HasPrefix(line, "/") {
				name, args, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
				if handled, commandErr := extensions.RunCommand(ctx, name, strings.TrimSpace(args)); handled {
					if commandErr != nil {
						fmt.Fprintf(os.Stderr, "[extension command error] %v\n", commandErr)
					}
					continue
				}
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
				fmt.Fprintf(os.Stderr, "Commands: %s. Enter text to start a task; while a turn is active, plain text or /steer <text> queues guidance.\n", strings.Join(commandNames, ", "))
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
			case line == "/tree" || strings.HasPrefix(line, "/tree "):
				entryID := strings.TrimSpace(strings.TrimPrefix(line, "/tree"))
				if activeDone != nil {
					fmt.Fprintln(os.Stderr, "Wait for the current turn to finish before changing session history.")
				} else if entryID == "" {
					printSessionTree(session)
				} else if err := session.BranchFrom(entryID); err != nil {
					fmt.Fprintf(os.Stderr, "Could not continue from that history entry: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "Continued from %s. Previous history remains available in /tree.\n", entryID)
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
					if err := modelconfig.SaveDefaultSelection(selection); err != nil {
						fmt.Fprintf(os.Stderr, "Could not save default model: %v\n", err)
					} else {
						fmt.Fprintf(os.Stderr, "Saved default model %s/%s.\n", selection.Provider, selection.Model)
					}
				case "switch":
					model, err := modelconfig.NewConfigured(selection)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						continue
					}
					configureModelRuntime(model, runtimeConfig)
					if err := session.SetModel(model); err != nil {
						fmt.Fprintf(os.Stderr, "Could not switch model: %v\n", err)
						continue
					}
					fmt.Fprintf(os.Stderr, "Switched to %s/%s.\n", selection.Provider, selection.Model)
				}
			case line == "/diff":
				if !sigmaagent.HasGitCommit(workspace) {
					fmt.Fprintln(os.Stderr, "Git diff is unavailable in this folder")
				} else {
					diff, err := gitOutput(workspace, "diff", "--stat", startingCommit)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
					} else if strings.TrimSpace(diff) == "" {
						fmt.Fprintln(os.Stderr, "No changes yet.")
					} else {
						fmt.Fprint(os.Stdout, diff)
					}
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
	total.CacheMissInputTokens += current.CacheMissInputTokens
	total.CacheCreationInputTokens += current.CacheCreationInputTokens
	total.ContextCompactions += current.ContextCompactions
	total.ContextCompactionInputTokens += current.ContextCompactionInputTokens
	total.ContextCompactionOutputTokens += current.ContextCompactionOutputTokens
}

func printSessionTree(session *agent.Session) {
	items := sessionTreeItems(session)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "This session has no conversation entries yet.")
		return
	}
	fmt.Fprintln(os.Stderr, "Session history (use /tree <entry-id> to continue from an entry):")
	for _, item := range items {
		fmt.Fprintln(os.Stderr, item.label)
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

func configureModelRuntime(model agent.Model, runtimeConfig config.Runtime) {
	if configurable, ok := model.(interface {
		ConfigureRuntime(int, int, time.Duration)
	}); ok {
		configurable.ConfigureRuntime(runtimeConfig.MaxRetries, runtimeConfig.ModelOutputTokens, time.Duration(runtimeConfig.ModelTimeoutMS)*time.Millisecond)
	}
	if configurable, ok := model.(interface{ ConfigureReasoningEffort(string) }); ok {
		configurable.ConfigureReasoningEffort(runtimeConfig.ModelReasoningEffort)
	}
}
