package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	modelconfig "github.com/sdaAlbert/sigma-agent/internal/model"
	"github.com/sdaAlbert/sigma-agent/internal/sigmaagent"
	"github.com/sdaAlbert/sigma-agent/internal/workflow"
)

type tuiBridge struct {
	program *tea.Program
	inputs  chan string
}

func (b *tuiBridge) send(msg tea.Msg) {
	if b != nil && b.program != nil {
		b.program.Send(msg)
	}
}

func (b *tuiBridge) sendEvent(event agent.Event)            { b.send(tuiAgentEventMsg(event)) }
func (b *tuiBridge) notice(message string)                  { b.send(tuiNoticeMsg(safeTerminalText(message))) }
func (b *tuiBridge) showSessionTree(items []tuiOverlayItem) { b.send(tuiTreeItemsMsg(items)) }

func (b *tuiBridge) Notify(message string) { b.notice(message) }

func (b *tuiBridge) Confirm(ctx context.Context, message string) (bool, error) {
	response := make(chan bool, 1)
	b.send(tuiConfirmMsg{message: safeTerminalText(message), response: response})
	select {
	case confirmed := <-response:
		return confirmed, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

type tuiAgentEventMsg agent.Event
type tuiNoticeMsg string
type tuiConfirmMsg struct {
	message  string
	response chan bool
}
type tuiCommandsMsg []workflow.Command
type tuiTreeItemsMsg []tuiOverlayItem
type tuiReadyMsg struct {
	sessionID, workspace string
	modelID              string
	contextLimit         int
	messages             []agent.Message
}
type tuiModelSelectionMsg modelconfig.Selection
type tuiRunDoneMsg struct{ err error }
type tuiStartMsg struct{}
type tuiStartupInfoMsg struct {
	repo string
	info tuiStartupInfo
}

type tuiOverlayItem struct {
	label string
	value string
}

type tuiToolOutput struct {
	tool, args, output, summary string
	status                      string
	line                        int
}

type tuiStartupInfo struct {
	repoIsGit  bool
	repoDirty  bool
	contexts   []string
	skills     []string
	extensions []string
}

type tuiCompletion struct {
	line, start, end int
	items            []tuiOverlayItem
}

type tuiRunner func(interactiveSetupOptions, *tuiBridge) error

type tuiModel struct {
	input              textarea.Model
	transcript         viewport.Model
	bridge             *tuiBridge
	setup              interactiveSetupOptions
	runner             tuiRunner
	phase              string
	secretFor          string
	lines              []string
	width              int
	height             int
	active             bool
	turnRunning        bool
	quitting           bool
	status             string
	overlay            string
	overlayTitle       string
	overlayItems       []tuiOverlayItem
	overlayIndex       int
	completion         *tuiCompletion
	history            []string
	historyIndex       int
	historyDraft       string
	expandOutputs      bool
	toolOutputs        map[string]*tuiToolOutput
	assistantText      string
	assistantLine      int
	modelStyle         lipgloss.Style
	mutedStyle         lipgloss.Style
	brandStyle         lipgloss.Style
	frameStyle         lipgloss.Style
	hintStyle          lipgloss.Style
	statusBarStyle     lipgloss.Style
	confirmResult      chan bool
	commands           []workflow.Command
	startupInfo        tuiStartupInfo
	startupLoading     bool
	showStartupDetails bool
	sessionID          string
	contextLimit       int
	inputTokens        int
	outputTokens       int
	cacheReadTokens    int
	cacheWriteTokens   int
	latestContext      int
	cacheHitRate       float64
}

func startTUI(setup interactiveSetupOptions, runner tuiRunner) error {
	input := textarea.New()
	input.Placeholder = "Ask Sigma or enter a /command"
	input.Prompt = "› "
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.SetWidth(80)
	input.CharLimit = 64 * 1024
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line"))
	input.SetValue(setup.initialPrompt)
	input.Focus()
	if root, err := gitOutput(setup.repo, "rev-parse", "--show-toplevel"); err == nil {
		setup.repo = strings.TrimSpace(root)
	} else if absolute, absErr := filepath.Abs(setup.repo); absErr == nil {
		setup.repo = absolute
	}
	transcript := viewport.New()
	transcript.SetWidth(80)
	transcript.SetHeight(18)
	app := &tuiModel{
		input: input, transcript: transcript, setup: setup, runner: runner, phase: "setup",
		assistantLine:  -1,
		historyIndex:   -1,
		toolOutputs:    make(map[string]*tuiToolOutput),
		lines:          nil,
		modelStyle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		mutedStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		brandStyle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		frameStyle:     lipgloss.NewStyle().BorderStyle(lipgloss.Border{Top: "─", Bottom: "─"}).BorderTop(true).BorderBottom(true).BorderForeground(lipgloss.Color("#ff4d4d")).Padding(0, 1),
		hintStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		statusBarStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236")),
	}
	app.startupLoading = true
	app.lines = app.startupLines()
	app.syncInputHeight()
	app.refreshTranscript(false)
	bridge := &tuiBridge{inputs: make(chan string, 32)}
	app.bridge = bridge
	program := tea.NewProgram(app)
	bridge.program = program
	_, err := program.Run()
	return err
}

func inspectTUIStartupInfo(repo string) tuiStartupInfo {
	info := tuiStartupInfo{}
	info.repoIsGit = sigmaagent.IsGitRepository(repo)
	if info.repoIsGit {
		status, err := gitOutput(repo, "status", "--porcelain=v1", "--untracked-files=all")
		if err == nil {
			info.repoDirty = strings.TrimSpace(status) != ""
		}
	}
	scopes, err := packageScopes(repo)
	if err != nil {
		return info
	}
	contributedSkills, _ := packageSkills(scopes)
	guidance, err := sigmaagent.LoadProjectGuidanceWithProfile(repo, contributedSkills, "")
	if err == nil {
		if strings.TrimSpace(guidance.Instructions) != "" {
			info.contexts = append(info.contexts, "AGENTS.md")
		}
		if strings.TrimSpace(guidance.Memory) != "" {
			info.contexts = append(info.contexts, filepath.Join(".sigma-agent", "MEMORY.md"))
		}
		if guidance.ExternalInstructionsPath != "" {
			info.contexts = append(info.contexts, guidance.ExternalInstructionsPath)
		}
		for _, skill := range guidance.Skills {
			info.skills = append(info.skills, skill.Name)
		}
	}
	info.extensions, _ = packageExtensions(scopes)
	for index, path := range info.extensions {
		info.extensions[index] = filepath.Base(path)
	}
	sort.Strings(info.contexts)
	sort.Strings(info.skills)
	sort.Strings(info.extensions)
	return info
}

func (m *tuiModel) startupLines() []string {
	projectInfo := fmt.Sprintf("%d context files · %d Skills · %d extensions", len(m.startupInfo.contexts), len(m.startupInfo.skills), len(m.startupInfo.extensions))
	if m.startupLoading {
		projectInfo = "Loading project context…"
	}
	repository := "Sigma edits this folder directly; changes appear here immediately."
	if !m.startupLoading && !m.startupInfo.repoIsGit {
		repository = "Working directly in this folder · Git diff is unavailable."
	}
	if m.startupInfo.repoDirty {
		repository = "Existing local changes are already here and will be preserved."
	}
	lines := []string{
		"Ask Sigma to explain the project, make a change, or run a verification task.",
		repository,
		"",
		"\x00startup-message:" + projectInfo,
	}
	if m.showStartupDetails {
		lines = append(lines, "", "Context: "+strings.Join(m.startupInfo.contexts, ", "), "Skills: "+strings.Join(m.startupInfo.skills, ", "), "Extensions: "+strings.Join(m.startupInfo.extensions, ", "), "Setup: /repo · /model · /login · /resume · /network on|off", "Enter sends · Shift+Enter adds a line · Ctrl+O hides these details")
	}
	return lines
}

func (m *tuiModel) validateStart() error {
	if err := validateSetupOptions(m.setup); err != nil {
		return err
	}
	if m.setup.resumeID != "" {
		return nil
	}
	info, err := os.Stat(m.setup.repo)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("workspace folder is unavailable: %s", m.setup.repo)
	}
	return nil
}

func renderSigmaMark() string {
	// 5x5 pixel map matching docs/assets/sigma-agent-mark.svg.
	const rows = "K W K W K;K K W K K;K R K R K;K K R K K;K K R K K"
	colors := map[byte]lipgloss.Style{
		'K': lipgloss.NewStyle().Foreground(lipgloss.Color("#111111")),
		'W': lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")),
		'R': lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4d4d")),
	}
	var b strings.Builder
	for _, row := range strings.Split(rows, ";") {
		for index := 0; index < len(row); index++ {
			if row[index] == ' ' {
				continue
			}
			b.WriteString(colors[row[index]].Render("██"))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *tuiModel) syncInputHeight() {
	height := strings.Count(m.input.Value(), "\n") + 1
	if height > 5 {
		height = 5
	}
	if height < 1 {
		height = 1
	}
	m.input.SetHeight(height)
	m.syncTranscriptHeight()
}

func (m *tuiModel) Init() tea.Cmd {
	repo := m.setup.repo
	return func() tea.Msg { return tuiStartupInfoMsg{repo: repo, info: inspectTUIStartupInfo(repo)} }
}

func (m *tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		inputWidth := msg.Width - 6
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.input.SetWidth(inputWidth)
		m.transcript.SetWidth(max(20, msg.Width-2))
		m.height = msg.Height
		m.syncTranscriptHeight()
		if m.phase == "setup" {
			m.lines = m.startupLines()
		}
		m.refreshTranscript(false)
	case tuiStartMsg:
		m.active = true
		m.phase = "starting"
		m.input.Placeholder = "Starting session…"
		m.appendLine("Preparing session…")
		return m, func() tea.Msg {
			err := m.runner(m.setup, m.bridge)
			return tuiRunDoneMsg{err: err}
		}
	case tuiStartupInfoMsg:
		if filepath.Clean(msg.repo) == filepath.Clean(m.setup.repo) {
			m.startupInfo = msg.info
			m.startupLoading = false
			m.lines = m.startupLines()
			m.refreshTranscript(true)
		}
	case tuiReadyMsg:
		m.phase = "chat"
		m.sessionID = msg.sessionID
		m.contextLimit = msg.contextLimit
		if provider, modelID, ok := strings.Cut(msg.modelID, "/"); ok && modelID != "" {
			if normalized, valid := modelconfig.NormalizeProvider(provider); valid {
				m.setup.model = modelconfig.Selection{Provider: normalized, Model: modelID}
			}
		}
		m.input.Placeholder = "Ask Sigma or enter a /command"
		m.appendLine(fmt.Sprintf("Session %s ready · %s", msg.sessionID, msg.workspace))
		for _, message := range msg.messages {
			for _, block := range message.Content {
				switch block.Type {
				case "text":
					if strings.TrimSpace(block.Text) != "" {
						content := safeTerminalText(strings.TrimSpace(block.Text))
						if message.Role == "assistant" {
							m.appendAssistantMessage(content)
							continue
						} else if message.Role == "user" {
							m.appendUserMessage(content)
							continue
						}
						m.appendLine(content)
					}
				case "tool_use":
					m.appendCardMessage("tool", "▸ "+safeTerminalText(block.Name)+" "+toolArgsSummary(block.Input, max(24, m.width-18)))
				case "tool_result":
					m.appendCardMessage("tool", "↳ "+compactToolOutput(block.Content))
				}
			}
		}
	case tuiRunDoneMsg:
		m.active = false
		m.turnRunning = false
		m.input.Placeholder = "Ask Sigma or enter a /command"
		if msg.err != nil {
			m.status = msg.err.Error()
			m.appendError("Run failed: " + msg.err.Error())
		} else {
			m.appendLine("Run finished. This session is saved.")
		}
	case tuiNoticeMsg:
		m.status = string(msg)
		m.appendLine(string(msg))
	case tuiConfirmMsg:
		m.confirmResult = msg.response
		m.openOverlay("confirm", "Extension asks for confirmation", []tuiOverlayItem{{label: "Yes", value: "yes"}, {label: "No", value: "no"}})
		m.appendLine("Extension confirmation: " + msg.message)
	case tuiCommandsMsg:
		m.commands = append([]workflow.Command(nil), msg...)
	case tuiModelSelectionMsg:
		m.setup.model = modelconfig.Selection(msg)
	case tuiTreeItemsMsg:
		if len(msg) == 0 {
			m.status = "This session has no conversation entries yet."
		} else {
			m.openOverlay("tree", "Session history · choose a point to continue", msg)
		}
	case tuiAgentEventMsg:
		m.onAgentEvent(agent.Event(msg))
	case tea.MouseWheelMsg:
		if m.overlay == "" {
			m.transcript, _ = m.transcript.Update(msg)
		}
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		if m.overlay != "" {
			switch key {
			case "esc":
				if m.overlay == "confirm" {
					m.resolveConfirmation(false)
				}
				m.closeOverlay()
				return m, nil
			case "up", "ctrl+p":
				if m.overlayIndex > 0 {
					m.overlayIndex--
				}
				return m, nil
			case "down", "ctrl+n":
				if m.overlayIndex+1 < len(m.overlayItems) {
					m.overlayIndex++
				}
				return m, nil
			case "enter":
				return m, m.selectOverlayItem()
			}
			if m.overlay != "completion" {
				return m, nil
			}
			m.closeOverlay()
		}
		if key == "ctrl+c" {
			if m.active {
				m.bridge.inputs <- "/exit"
				m.status = "Stopping the active turn and saving…"
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		}
		if key == "esc" && m.turnRunning {
			select {
			case m.bridge.inputs <- "/abort":
				m.status = "Abort requested"
			default:
				m.status = "Input queue is full."
			}
			return m, nil
		}
		if key == "esc" && m.secretFor != "" {
			m.secretFor = ""
			m.input.Reset()
			m.input.Placeholder = "Ask Sigma or enter a /command"
			m.status = "Credential entry cancelled"
			m.syncInputHeight()
			return m, nil
		}
		if key == "enter" {
			return m, m.submit()
		}
		if key == "ctrl+l" {
			m.openModelSelector()
			return m, nil
		}
		if key == "ctrl+r" {
			m.openSessionSelector()
			return m, nil
		}
		if key == "ctrl+o" {
			if m.phase == "setup" {
				m.showStartupDetails = !m.showStartupDetails
				m.lines = m.startupLines()
				m.refreshTranscript(true)
			} else {
				m.expandOutputs = !m.expandOutputs
				m.refreshTranscript(true)
			}
			return m, nil
		}
		if key == "tab" {
			m.completePath()
			return m, nil
		}
		if key == "pageup" {
			m.transcript, _ = m.transcript.Update(message)
			return m, nil
		}
		if key == "pagedown" {
			m.transcript, _ = m.transcript.Update(message)
			return m, nil
		}
		if key == "up" && m.atHistoryPreviousBoundary() {
			m.browseHistory(-1)
			return m, nil
		}
		if key == "down" && m.atHistoryNextBoundary() {
			m.browseHistory(1)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	m.syncInputHeight()
	return m, cmd
}

func (m *tuiModel) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return nil
	}
	if m.secretFor != "" {
		if err := m.saveSecret(value); err != nil {
			m.status = "Could not save credential: " + err.Error()
			m.appendError(m.status)
			return nil
		}
		provider := m.secretFor
		m.secretFor = ""
		m.input.Reset()
		m.input.Placeholder = "Ask Sigma or enter a /command"
		m.status = "Saved " + provider + " credential in your user configuration"
		m.appendLine("Saved " + provider + " credential in the user configuration directory.")
		m.appendLine(provider + " is ready. You can start your task now.")
		m.syncInputHeight()
		return nil
	}
	if m.phase == "setup" {
		if strings.HasPrefix(value, "/") {
			m.input.Reset()
			m.syncInputHeight()
			return m.setupCommand(value)
		}
		if err := m.validateStart(); err != nil {
			m.status = err.Error()
			m.appendError(err.Error())
			return nil
		}
		m.addHistory(value)
		m.setup.initialPrompt = value
		m.input.Reset()
		m.syncInputHeight()
		m.appendUserMessage(value)
		m.appendLine("Starting task…")
		return func() tea.Msg { return tuiStartMsg{} }
	}
	if m.active {
		if value == "/model" {
			m.input.Reset()
			m.syncInputHeight()
			m.openModelSelector()
			return nil
		}
		m.addHistory(value)
		m.input.Reset()
		m.syncInputHeight()
		m.appendUserMessage(value)
		select {
		case m.bridge.inputs <- value:
		default:
			m.appendLine("Input queue is full; try again in a moment.")
		}
		return nil
	}
	if value == "/exit" || value == "/quit" {
		m.input.Reset()
		m.syncInputHeight()
		return tea.Quit
	}
	m.appendLine("This run has ended. Restart sigma-agent --tui to open another session.")
	return nil
}

func (m *tuiModel) setupCommand(line string) tea.Cmd {
	command, value, _ := strings.Cut(line, " ")
	value = strings.TrimSpace(value)
	switch command {
	case "/help":
		m.appendLine("Setup: /repo <path>, /model [provider/model-id|save], /login (choose a credential), /logout <provider|github>, /session <id>, /resume <id>, /network on|off, /start, /exit. Enter submits; Shift+Enter inserts a new line.")
	case "/repo":
		if value == "" {
			return m.openRepositorySelector()
		}
		path, err := filepath.Abs(value)
		if err == nil {
			var info os.FileInfo
			info, err = os.Stat(path)
			if err == nil && info.IsDir() {
				if top, gitErr := gitOutput(path, "rev-parse", "--show-toplevel"); gitErr == nil {
					path = strings.TrimSpace(top)
				}
				return m.selectRepository(path)
			}
		}
		if err != nil {
			m.appendError("Could not select workspace folder: " + err.Error())
		} else {
			m.appendError("Workspace path must be an existing folder.")
		}
	case "/model":
		if value == "" {
			m.openModelSelector()
			return nil
		}
		if value == "list" {
			m.appendLine(fmt.Sprintf("Current model: %s/%s. Supported providers:", m.setup.model.Provider, m.setup.model.Model))
			for _, provider := range modelconfig.Providers() {
				configured, err := modelconfig.ProviderHasAPIKey(provider.ID)
				status := "key not configured"
				if err != nil {
					status = "credential status unavailable"
				} else if configured {
					status = "ready"
				}
				m.appendLine(fmt.Sprintf("  %-10s %-12s %s", provider.ID, provider.Name, status))
			}
			m.appendLine("Use /model <provider>/<model-id> to select.")
			break
		}
		if value == "save" {
			hasKey, keyErr := modelconfig.ProviderHasAPIKey(m.setup.model.Provider)
			if keyErr != nil || !hasKey {
				m.appendLine("Configure a " + m.setup.model.Provider + " API key before saving it as the default.")
				break
			}
			if err := modelconfig.SaveDefaultSelection(m.setup.model); err != nil {
				m.appendError("Could not save default model: " + err.Error())
			} else {
				m.appendLine("Saved default model.")
			}
			break
		}
		providerText, modelID, hasModel := strings.Cut(value, "/")
		provider, ok := modelconfig.NormalizeProvider(providerText)
		if !ok {
			m.appendLine("Usage: /model <provider>/<model-id>")
			break
		}
		if !hasModel {
			modelID = modelconfig.DefaultModel(provider)
		}
		if strings.TrimSpace(modelID) == "" {
			m.appendLine("Model ID is required.")
			break
		}
		m.setup.model = modelconfig.Selection{Provider: provider, Model: strings.TrimSpace(modelID)}
		m.setup.modelExplicit = true
		m.appendLine("Selected model " + provider + "/" + modelID)
	case "/login":
		if value == "" {
			items := []tuiOverlayItem{{label: "GitHub · GitHub MCP token", value: "github"}}
			for _, provider := range modelconfig.Providers() {
				configured, err := modelconfig.ProviderHasAPIKey(provider.ID)
				keyStatus := "key not configured"
				if err == nil && configured {
					keyStatus = "key configured · replace"
				}
				items = append(items, tuiOverlayItem{
					label: fmt.Sprintf("%s · %s (%s)", provider.Name, provider.ID, keyStatus),
					value: provider.ID,
				})
			}
			m.openOverlay("login", "Choose a credential to configure", items)
			return nil
		}
		if strings.EqualFold(value, "github") {
			m.beginSecretEntry("github")
		} else if provider, ok := modelconfig.NormalizeProvider(value); ok {
			m.beginSecretEntry(provider)
		} else {
			m.appendLine("Choose /login github or a supported model provider.")
		}
	case "/logout":
		var err error
		if strings.EqualFold(value, "github") {
			err = agent.RemoveGitHubToken()
		} else {
			err = modelconfig.RemoveAPIKey(value)
		}
		if err != nil {
			m.appendError("Could not remove credential: " + err.Error())
		} else {
			m.appendLine("Credential removed.")
		}
	case "/session":
		if !agent.ValidSessionID(value) {
			m.appendError("Invalid session ID.")
			break
		}
		m.setup.sessionID, m.setup.resumeID = value, ""
		m.appendLine("New session ID: " + value)
	case "/resume":
		if value == "" {
			m.openSessionSelector()
			return nil
		}
		if !agent.ValidSessionID(value) {
			m.appendLine("Usage: /resume <id>; press Ctrl+R to pick a saved session.")
			break
		}
		m.setup.resumeID, m.setup.sessionID = value, ""
		m.appendLine("Will resume session: " + value)
	case "/network":
		if value == "on" {
			m.setup.allowNetwork = true
		} else if value == "off" {
			m.setup.allowNetwork = false
		} else {
			m.appendLine("Usage: /network on|off")
		}
	case "/start":
		if err := m.validateStart(); err != nil {
			m.appendError(err.Error())
			break
		}
		return func() tea.Msg { return tuiStartMsg{} }
	case "/exit", "/quit":
		m.quitting = true
		return tea.Quit
	default:
		m.appendLine("Unknown setup command. Use /help.")
	}
	m.appendLine(m.setupSummary())
	return nil
}

func (m *tuiModel) saveSecret(value string) error {
	if m.secretFor == "github" {
		return agent.SaveGitHubToken(value)
	}
	return modelconfig.SaveAPIKey(m.secretFor, value)
}

func (m *tuiModel) beginSecretEntry(provider string) {
	m.secretFor = provider
	m.input.Reset()
	m.input.Placeholder = "Credential (input is hidden)"
	m.status = "Enter " + provider + " credential · Enter to save · Esc to cancel"
	m.appendLine("Enter " + provider + " credential below. Characters are hidden; press Enter to save or Esc to cancel.")
	m.syncInputHeight()
}

func (m *tuiModel) setupSummary() string {
	selection := m.setup.resumeID
	if selection == "" {
		selection = m.setup.sessionID
	}
	if selection == "" {
		selection = "new session"
	}
	network := "off"
	if m.setup.allowNetwork {
		network = "on"
	}
	return fmt.Sprintf("Repo: %s · Session: %s · Model: %s/%s · Docker network: %s", m.setup.repo, selection, m.setup.model.Provider, m.setup.model.Model, network)
}

func (m *tuiModel) openModelSelector() {
	items := []tuiOverlayItem{{label: "Current · " + m.setup.model.Provider + "/" + m.setup.model.Model, value: "current"}}
	for _, provider := range modelconfig.Providers() {
		configured, err := modelconfig.ProviderHasAPIKey(provider.ID)
		keyStatus := "key needed"
		if err == nil && configured {
			keyStatus = "ready"
		}
		items = append(items, tuiOverlayItem{
			label: fmt.Sprintf("%s · %s (%s)", provider.Name, provider.ID, keyStatus),
			value: "provider:" + provider.ID,
		})
	}
	m.openOverlay("model", "Choose a provider / model", items)
}

func (m *tuiModel) openSessionSelector() {
	if m.phase != "setup" {
		m.status = "Choose a session from setup before starting the TUI session."
		return
	}
	root, err := os.Getwd()
	if err != nil {
		m.appendError("Could not find session directory: " + err.Error())
		return
	}
	store, err := agent.NewFileSessionStore(filepath.Join(root, "var", "sessions"))
	if err != nil {
		m.appendError("Could not read saved sessions: " + err.Error())
		return
	}
	sessions, err := store.List()
	if err != nil {
		m.appendError("Could not list saved sessions: " + err.Error())
		return
	}
	items := make([]tuiOverlayItem, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, tuiOverlayItem{
			label: fmt.Sprintf("%s · %s · %d messages · %s", session.ID, session.ModelID, session.Messages, session.UpdatedAt.Local().Format("2006-01-02 15:04")),
			value: session.ID,
		})
	}
	if len(items) == 0 {
		m.status = "No saved sessions found."
		return
	}
	m.openOverlay("session", "Resume a saved session", items)
}

func (m *tuiModel) openOverlay(kind, title string, items []tuiOverlayItem) {
	m.overlay = kind
	m.overlayTitle = title
	m.overlayItems = items
	m.overlayIndex = 0
	m.syncTranscriptHeight()
}

func (m *tuiModel) closeOverlay() {
	m.overlay = ""
	m.overlayTitle = ""
	m.overlayItems = nil
	m.overlayIndex = 0
	m.completion = nil
	m.syncTranscriptHeight()
}

func (m *tuiModel) syncTranscriptHeight() {
	if m.height <= 0 {
		return
	}
	inputLines := strings.Count(m.input.Value(), "\n") + 1
	if inputLines > 5 {
		inputLines = 5
	}
	height := m.height - 14 - (inputLines - 1)
	if m.overlay != "" {
		visible := len(m.overlayItems)
		if visible > 8 {
			visible = 8
		}
		height -= visible + 5
	}
	if height < 3 {
		height = 3
	}
	m.transcript.SetHeight(height)
}

func (m *tuiModel) selectOverlayItem() tea.Cmd {
	if m.overlayIndex < 0 || m.overlayIndex >= len(m.overlayItems) {
		return nil
	}
	item := m.overlayItems[m.overlayIndex]
	switch m.overlay {
	case "login":
		m.closeOverlay()
		m.beginSecretEntry(item.value)
		return nil
	case "repo":
		m.closeOverlay()
		return m.selectRepository(item.value)
	case "confirm":
		m.resolveConfirmation(item.value == "yes")
		m.closeOverlay()
		return nil
	case "model":
		if strings.HasPrefix(item.value, "provider:") {
			provider := strings.TrimPrefix(item.value, "provider:")
			modelID := modelconfig.DefaultModel(provider)
			if modelID == "" {
				m.input.SetValue("/model " + provider + "/")
				m.syncInputHeight()
				m.status = "Enter the provider's model ID, then press Enter."
			} else if m.phase == "setup" {
				m.setup.model = modelconfig.Selection{Provider: provider, Model: modelID}
				m.setup.modelExplicit = true
				m.status = "Selected " + provider + "/" + modelID
			} else {
				select {
				case m.bridge.inputs <- "/model " + provider + "/" + modelID:
				default:
					m.status = "Input queue is full."
				}
			}
		}
		m.closeOverlay()
		return nil
	case "session":
		m.setup.resumeID = item.value
		m.setup.sessionID = ""
		m.status = "Selected session " + item.value
		m.closeOverlay()
		return nil
	case "tree":
		m.closeOverlay()
		select {
		case m.bridge.inputs <- "/tree " + item.value:
		default:
			m.status = "Input queue is full."
		}
		return nil
	case "completion":
		completion := m.completion
		m.closeOverlay()
		if completion != nil {
			m.applyCompletion(completion, item.value)
		}
		return nil
	}
	return nil
}

func (m *tuiModel) openRepositorySelector() tea.Cmd {
	root, err := os.Getwd()
	if err != nil {
		m.appendLine("Could not read the current folder. Use /repo <path> to select a workspace folder.")
		return nil
	}
	repositories := discoverGitRepositories(root)
	if len(repositories) == 0 {
		m.appendLine("No Git repositories found nearby. Use /repo <path> to select any workspace folder.")
		return nil
	}
	if len(repositories) == 1 {
		return m.selectRepository(repositories[0])
	}
	items := make([]tuiOverlayItem, 0, len(repositories))
	for _, repository := range repositories {
		label, err := filepath.Rel(root, repository)
		if err != nil || strings.HasPrefix(label, "..") {
			label = repository
		}
		items = append(items, tuiOverlayItem{label: label, value: repository})
	}
	m.openOverlay("repo", "Choose a Git repository", items)
	return nil
}

func discoverGitRepositories(root string) []string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	found := map[string]struct{}{}
	visited := 0
	var visit func(string, int)
	visit = func(directory string, depth int) {
		if visited >= 300 {
			return
		}
		visited++
		_, metadataErr := os.Stat(filepath.Join(directory, ".git"))
		if directory == absolute || metadataErr == nil {
			root, err := gitOutput(directory, "rev-parse", "--show-toplevel")
			if err != nil {
				return
			}
			root, err = filepath.Abs(strings.TrimSpace(root))
			if err == nil {
				found[filepath.Clean(root)] = struct{}{}
			}
			return
		}
		if depth >= 2 {
			return
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			switch strings.ToLower(entry.Name()) {
			case "appdata", "node_modules", "vendor", "windows", "program files", "program files (x86)":
				continue
			}
			visit(filepath.Join(directory, entry.Name()), depth+1)
		}
	}
	visit(absolute, 0)
	repositories := make([]string, 0, len(found))
	for repository := range found {
		repositories = append(repositories, repository)
	}
	sort.Strings(repositories)
	return repositories
}

func (m *tuiModel) selectRepository(repository string) tea.Cmd {
	m.setup.repo = repository
	m.startupLoading = true
	m.startupInfo = tuiStartupInfo{}
	m.lines = m.startupLines()
	m.appendLine("Repository selected: " + repository)
	return func() tea.Msg { return tuiStartupInfoMsg{repo: repository, info: inspectTUIStartupInfo(repository)} }
}

func (m *tuiModel) resolveConfirmation(confirmed bool) {
	if m.confirmResult != nil {
		select {
		case m.confirmResult <- confirmed:
		default:
		}
		m.confirmResult = nil
	}
}

func (m *tuiModel) renderOverlay() string {
	var b strings.Builder
	visible := 8
	start := 0
	if m.overlayIndex >= visible {
		start = m.overlayIndex - visible + 1
	}
	end := start + visible
	if end > len(m.overlayItems) {
		end = len(m.overlayItems)
	}
	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render(m.overlayTitle) + "\n")
	for index := start; index < end; index++ {
		prefix := "  "
		style := m.mutedStyle
		if index == m.overlayIndex {
			prefix = "› "
			style = m.modelStyle
		}
		b.WriteString(style.Render(prefix+safeTerminalText(m.overlayItems[index].label)) + "\n")
	}
	b.WriteString(m.mutedStyle.Render("↑/↓ select · Enter accept · Esc close"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(b.String()) + "\n"
}

func (m *tuiModel) addHistory(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || (len(m.history) > 0 && m.history[len(m.history)-1] == prompt) {
		return
	}
	m.history = append(m.history, prompt)
	m.historyIndex = -1
	m.historyDraft = ""
}

func (m *tuiModel) atHistoryPreviousBoundary() bool {
	return len(m.history) > 0 && m.input.Line() == 0 && m.input.Column() == 0
}

func (m *tuiModel) atHistoryNextBoundary() bool {
	lines := strings.Split(m.input.Value(), "\n")
	last := []rune(lines[len(lines)-1])
	return len(m.history) > 0 && m.input.Line() == len(lines)-1 && m.input.Column() >= len(last)
}

func (m *tuiModel) browseHistory(direction int) {
	if direction < 0 {
		if m.historyIndex == -1 {
			m.historyDraft = m.input.Value()
			m.historyIndex = len(m.history) - 1
		} else if m.historyIndex > 0 {
			m.historyIndex--
		}
		m.input.SetValue(m.history[m.historyIndex])
		m.syncInputHeight()
		return
	}
	if m.historyIndex == -1 {
		return
	}
	if m.historyIndex < len(m.history)-1 {
		m.historyIndex++
		m.input.SetValue(m.history[m.historyIndex])
		m.syncInputHeight()
		return
	}
	m.historyIndex = -1
	m.input.SetValue(m.historyDraft)
	m.syncInputHeight()
}

func (m *tuiModel) completePath() {
	value := m.input.Value()
	lines := strings.Split(value, "\n")
	lineIndex := m.input.Line()
	if lineIndex < 0 || lineIndex >= len(lines) {
		return
	}
	lineRunes := []rune(lines[lineIndex])
	cursor := m.input.Column()
	if cursor > len(lineRunes) {
		cursor = len(lineRunes)
	}
	start := cursor
	for start > 0 && !unicode.IsSpace(lineRunes[start-1]) {
		start--
	}
	end := cursor
	for end < len(lineRunes) && !unicode.IsSpace(lineRunes[end]) {
		end++
	}
	word := string(lineRunes[start:cursor])
	isFileReference := strings.HasPrefix(word, "@")
	isRepoArgument := m.phase == "setup" && strings.HasPrefix(strings.TrimSpace(lines[lineIndex]), "/repo ")
	if !isFileReference && !isRepoArgument {
		m.completeCommand(lineIndex, start, end, lineRunes)
		return
	}
	pathText := word
	prefix := ""
	if isFileReference {
		prefix = "@"
		pathText = strings.TrimPrefix(word, "@")
	}
	root, err := os.Getwd()
	if err != nil {
		return
	}
	if isFileReference && m.setup.repo != "" {
		root = m.setup.repo
	}
	directoryPart := filepath.Dir(pathText)
	basePrefix := filepath.Base(pathText)
	if pathText == "" || strings.HasSuffix(pathText, string(filepath.Separator)) {
		directoryPart = pathText
		basePrefix = ""
	}
	searchDir := filepath.Join(root, directoryPart)
	if filepath.IsAbs(pathText) {
		searchDir = directoryPart
	}
	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return
	}
	var items []tuiOverlayItem
	for _, entry := range entries {
		if !strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(basePrefix)) {
			continue
		}
		relative := filepath.Join(directoryPart, entry.Name())
		if directoryPart == "." {
			relative = entry.Name()
		}
		label := relative
		if entry.IsDir() {
			label += string(filepath.Separator)
		}
		replacement := prefix + relative
		if entry.IsDir() {
			replacement += string(filepath.Separator)
		} else if isFileReference {
			replacement += " "
		}
		items = append(items, tuiOverlayItem{label: label, value: replacement})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].label) < strings.ToLower(items[j].label) })
	if len(items) == 1 {
		m.applyCompletion(&tuiCompletion{line: lineIndex, start: start, end: end}, items[0].value)
		return
	}
	if len(items) > 0 {
		m.completion = &tuiCompletion{line: lineIndex, start: start, end: end}
		m.openOverlay("completion", "Complete path", items)
	}
}

func (m *tuiModel) completeCommand(lineIndex, start, end int, lineRunes []rune) {
	word := string(lineRunes[start:end])
	if !strings.HasPrefix(word, "/") {
		return
	}
	commands := []string{"/help", "/repo ", "/model ", "/login", "/logout ", "/session ", "/resume ", "/network ", "/start", "/diff", "/tree", "/steer ", "/followup ", "/abort", "/exit", "/skill:"}
	items := make([]tuiOverlayItem, 0)
	for _, command := range commands {
		if strings.HasPrefix(command, word) {
			items = append(items, tuiOverlayItem{label: strings.TrimSpace(command), value: command})
		}
	}
	for _, command := range m.commands {
		value := "/" + command.Name
		if strings.HasPrefix(value, word) {
			label := value
			if command.Description != "" {
				label += "  " + command.Description
			}
			items = append(items, tuiOverlayItem{label: label, value: value})
		}
	}
	if len(items) == 1 {
		m.applyCompletion(&tuiCompletion{line: lineIndex, start: start, end: end}, items[0].value)
	} else if len(items) > 1 {
		m.completion = &tuiCompletion{line: lineIndex, start: start, end: end}
		m.openOverlay("completion", "Commands", items)
	}
}

func (m *tuiModel) applyCompletion(completion *tuiCompletion, replacement string) {
	lines := strings.Split(m.input.Value(), "\n")
	if completion.line < 0 || completion.line >= len(lines) {
		return
	}
	runes := []rune(lines[completion.line])
	if completion.start < 0 || completion.end > len(runes) || completion.start > completion.end {
		return
	}
	updated := append([]rune(nil), runes[:completion.start]...)
	updated = append(updated, []rune(replacement)...)
	updated = append(updated, runes[completion.end:]...)
	lines[completion.line] = string(updated)
	m.input.SetValue(strings.Join(lines, "\n"))
	m.syncInputHeight()
}

func (m *tuiModel) onAgentEvent(event agent.Event) {
	switch event.Type {
	case "model_text_delta":
		if m.assistantLine < 0 {
			m.assistantLine = len(m.lines)
			m.lines = append(m.lines, "\x00assistant-message:")
		}
		m.assistantText += safeTerminalText(event.Text)
		m.lines[m.assistantLine] = "\x00assistant-message:" + m.assistantText
		m.refreshTranscript(false)
	case "turn_start":
		m.turnRunning = true
		m.assistantText = ""
		m.assistantLine = -1
	case "turn_end":
		m.turnRunning = false
		if m.assistantText != "" && m.assistantLine >= 0 && m.assistantLine < len(m.lines) {
			m.lines[m.assistantLine] = "\x00assistant-message:" + m.assistantText
			m.refreshTranscript(true)
		}
	case "tool_execution_start":
		m.turnRunning = true
		label := "▸ " + safeTerminalText(event.Tool)
		args := ""
		if len(event.Args) > 0 {
			args = toolArgsSummary(event.Args, max(24, m.width-18))
			label += " " + args
		}
		m.appendCardMessage("tool", label)
		if event.ToolCallID != "" {
			m.toolOutputs[event.ToolCallID] = &tuiToolOutput{tool: event.Tool, args: args, status: "running", line: len(m.lines) - 1}
			m.appendLine("\x00tool-output:" + event.ToolCallID)
		}
	case "tool_execution_end":
		if output, ok := m.toolOutputs[event.ToolCallID]; ok {
			output.status = event.Status
			output.output = safeTerminalText(event.ResultOutput)
			output.summary = safeTerminalText(event.ResultSummary)
			label := fmt.Sprintf("✓ %s · %s", safeTerminalText(event.Tool), formatDuration(event.Duration))
			if event.Error != "" {
				label = fmt.Sprintf("✗ %s · %s", safeTerminalText(event.Tool), truncateText(safeTerminalText(event.Error), 120))
			}
			if output.args != "" {
				label += "  " + output.args
			}
			if output.line >= 0 && output.line < len(m.lines) {
				kind := "tool"
				if event.Error != "" {
					kind = "error"
				}
				m.lines[output.line] = "\x00" + kind + "-message:" + label
			}
			m.turnRunning = false
			for _, pending := range m.toolOutputs {
				if pending.status == "running" {
					m.turnRunning = true
					break
				}
			}
			m.refreshTranscript(false)
		} else {
			label := fmt.Sprintf("✓ %s · %s", safeTerminalText(event.Tool), formatDuration(event.Duration))
			if event.Error != "" {
				m.appendCardMessage("error", "✗ "+safeTerminalText(event.Tool)+" · "+truncateText(safeTerminalText(event.Error), 120))
			} else {
				m.appendCardMessage("tool", label)
			}
		}
		if event.ResultSummary != "" && event.ToolCallID == "" {
			m.appendCardMessage("tool", "↳ "+compactToolOutput(event.ResultSummary))
		}
	case "model_usage":
		m.inputTokens += event.InputTokens
		m.outputTokens += event.OutputTokens
		m.cacheReadTokens += event.CacheReadInputTokens
		m.cacheWriteTokens += event.CacheCreationInputTokens
		m.latestContext = event.InputTokens
		if event.InputTokens > 0 {
			m.cacheHitRate = float64(event.CacheReadInputTokens) * 100 / float64(event.InputTokens)
		}
	case "context_compacted_model":
		m.appendLine(fmt.Sprintf("Context compacted · %d messages dropped · %d summary tokens", event.MessagesDropped, event.SummaryTokens))
		if event.CompactionSummary != "" {
			m.appendLine("  Summary: " + safeTerminalText(event.CompactionSummary))
		}
	case "model_error", "agent_error", "agent_aborted":
		if event.Error != "" {
			m.appendCardMessage("error", "Sigma · "+safeTerminalText(event.Error))
		}
	}
}

func (m *tuiModel) appendLine(line string) {
	wasAtBottom := m.transcript.AtBottom()
	if !strings.HasPrefix(line, "\x00") {
		line = "\x00system-message:" + line
	}
	if len(m.lines) > 0 && m.lines[len(m.lines)-1] == line {
		return
	}
	m.lines = append(m.lines, line)
	if len(m.lines) > 600 {
		m.lines = append([]string(nil), m.lines[len(m.lines)-600:]...)
	}
	m.refreshTranscript(wasAtBottom)
}

func (m *tuiModel) appendUserMessage(text string) {
	m.appendCardMessage("user", text)
}

func (m *tuiModel) appendAssistantMessage(text string) {
	m.appendCardMessage("assistant", text)
}

func (m *tuiModel) appendError(text string) {
	m.appendCardMessage("error", text)
}

func (m *tuiModel) appendCardMessage(kind, text string) {
	wasAtBottom := m.transcript.AtBottom()
	line := "\x00" + kind + "-message:" + safeTerminalText(text)
	if kind == "error" && len(m.lines) > 0 && m.lines[len(m.lines)-1] == line {
		return
	}
	m.lines = append(m.lines, line)
	if len(m.lines) > 600 {
		m.lines = append([]string(nil), m.lines[len(m.lines)-600:]...)
	}
	m.refreshTranscript(wasAtBottom)
}

func (m *tuiModel) View() tea.View {
	var b strings.Builder
	banner := []string{
		m.brandStyle.Render("Sigma Agent") + m.mutedStyle.Render(" · v"+sigmaAgentVersion),
		m.mutedStyle.Render("Esc interrupt · Ctrl+C exit · / commands · Ctrl+O tool output"),
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, renderSigmaMark(), "  "+strings.Join(banner, "\n")) + "\n")
	repoName := filepath.Base(strings.TrimSpace(m.setup.repo))
	if repoName == "." || repoName == string(filepath.Separator) || repoName == "" {
		repoName = "no repository"
	}
	repoState := "direct workspace"
	if m.startupInfo.repoDirty {
		repoState = "local edits present"
	}
	if !m.startupLoading && !m.startupInfo.repoIsGit {
		repoState = "folder workspace · no Git diff"
	}
	phase := "ready"
	if m.phase == "starting" {
		phase = "starting"
	} else if m.turnRunning {
		phase = "working"
	}
	metadata := fmt.Sprintf("%s  ·  %s  ·  %s", repoName, repoState, strings.ToUpper(phase))
	b.WriteString(m.mutedStyle.Render(truncateText(metadata, max(20, m.width-2))) + "\n")
	ruleWidth := m.width - 2
	if ruleWidth < 20 {
		ruleWidth = 20
	}
	b.WriteString(m.mutedStyle.Render(strings.Repeat("─", ruleWidth)) + "\n")
	b.WriteString(m.transcript.View())
	b.WriteByte('\n')
	if m.overlay != "" {
		b.WriteString(m.renderOverlay())
	}
	if m.phase == "setup" {
		b.WriteString(m.hintStyle.Render("Enter starts · Shift+Enter newline · Tab complete · Ctrl+O details") + "\n")
	} else {
		b.WriteString(m.hintStyle.Render("Esc interrupt · Enter send · Shift+Enter newline · Ctrl+O tool output") + "\n")
	}
	if m.secretFor != "" {
		value := m.input.Value()
		masked := strings.Repeat("•", utf8.RuneCountInString(value))
		label := fmt.Sprintf("Enter %s credential · input hidden · Enter save · Esc cancel", m.secretFor)
		b.WriteString(m.hintStyle.Render(label) + "\n")
		if masked == "" {
			masked = m.hintStyle.Render("Paste or type credential here")
		}
		b.WriteString(m.inputFrameStyle().Render(masked) + "\n")
	} else {
		b.WriteString(m.inputFrameStyle().Render(m.input.View()) + "\n")
	}
	b.WriteString(m.renderStatusBar())
	v := tea.NewView(b.String())
	v.AltScreen = true
	// Leave mouse input with the terminal so users can drag-select and copy text.
	return v
}

func (m *tuiModel) renderStatusBar() string {
	width := m.width
	if width < 20 {
		width = 80
	}
	left := "ready"
	if m.turnRunning {
		left = "working"
	} else if m.phase == "starting" {
		left = "starting"
	} else if m.status != "" {
		left = safeTerminalText(m.status)
	}
	left = strings.Join(strings.Fields(strings.ReplaceAll(safeTerminalText(left), "\n", " ")), " ")
	usage := fmt.Sprintf("↑%s ↓%s R%s W%s CH—", formatTokenCount(m.inputTokens), formatTokenCount(m.outputTokens), formatTokenCount(m.cacheReadTokens), formatTokenCount(m.cacheWriteTokens))
	if m.latestContext > 0 {
		usage = fmt.Sprintf("↑%s ↓%s R%s W%s CH%.0f%%", formatTokenCount(m.inputTokens), formatTokenCount(m.outputTokens), formatTokenCount(m.cacheReadTokens), formatTokenCount(m.cacheWriteTokens), m.cacheHitRate)
	}
	left += " · " + usage
	repoName := filepath.Base(strings.TrimSpace(m.setup.repo))
	if repoName != "" && repoName != "." {
		left += " · " + repoName
	}
	if m.sessionID != "" {
		shortID := m.sessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		left += " · " + shortID
	}
	modelName := m.setup.model.Model
	if modelName == "" {
		modelName = "no model selected"
	} else if m.setup.model.Provider != "" {
		modelName = fmt.Sprintf("(%s) %s", m.setup.model.Provider, modelName)
	}
	contextUsage := "—"
	if m.latestContext > 0 && m.contextLimit > 0 {
		contextUsage = fmt.Sprintf("%.1f%%/%s", float64(m.latestContext)*100/float64(m.contextLimit), formatTokenCount(m.contextLimit))
	}
	right := modelName
	if contextUsage != "—" {
		right += " · " + contextUsage
	}
	if lipgloss.Width(right) > width-9 {
		right = truncateTUIWidth(right, width-9)
	}
	left = truncateTUIWidth(left, max(1, width-lipgloss.Width(right)-1))
	padding := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return m.statusBarStyle.Width(width).MaxHeight(1).Render(left + strings.Repeat(" ", padding) + right)
}

func (m *tuiModel) inputFrameStyle() lipgloss.Style {
	return m.frameStyle.BorderForeground(lipgloss.Color("#ff4d4d"))
}

func truncateTUIWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	var b strings.Builder
	for _, r := range value {
		candidate := b.String() + string(r)
		if lipgloss.Width(candidate)+lipgloss.Width("…") > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func (m *tuiModel) refreshTranscript(keepBottom bool) {
	if m.transcript.Width() == 0 {
		width := m.width - 2
		if width < 20 {
			width = 80
		}
		m.transcript.SetWidth(width)
	}
	if m.transcript.Height() == 0 {
		m.transcript.SetHeight(18)
	}
	lines := make([]string, 0, len(m.lines))
	for _, line := range m.lines {
		if kind, text, ok := messageCard(line); ok {
			lines = append(lines, m.renderMessageCard(kind, text)...)
			continue
		}
		if strings.HasPrefix(line, "\x00tool-output:") {
			id := strings.TrimPrefix(line, "\x00tool-output:")
			output := m.toolOutputs[id]
			if output == nil || (output.status == "running" && output.output == "") {
				lines = append(lines, m.renderMessageCard("tool", "… output pending")...)
				continue
			}
			if output.output == "" {
				lines = append(lines, m.renderMessageCard("tool", "[no output]")...)
				continue
			}
			if !m.expandOutputs {
				preview := output.summary
				if preview == "" {
					preview = compactToolOutput(output.output)
				}
				lines = append(lines, m.renderMessageCard("tool", "↳ "+preview+" · Ctrl+O to expand")...)
				continue
			}
			lines = append(lines, m.renderMessageCard("tool", strings.TrimSpace(output.output))...)
			continue
		}
		lines = append(lines, line)
	}
	wrapped := make([]string, 0, len(lines))
	wrapWidth := m.transcript.Width()
	if wrapWidth < 20 {
		wrapWidth = 80
	}
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(lipgloss.Wrap(line, wrapWidth, " "), "\n")...)
	}
	wasAtBottom := keepBottom || m.transcript.AtBottom()
	m.transcript.SetContentLines(wrapped)
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func messageCard(line string) (kind, text string, ok bool) {
	for _, candidate := range []string{"startup", "user", "assistant", "tool", "error", "system"} {
		prefix := "\x00" + candidate + "-message:"
		if strings.HasPrefix(line, prefix) {
			return candidate, strings.TrimPrefix(line, prefix), true
		}
	}
	return "", "", false
}

func (m *tuiModel) renderMessageCard(kind, text string) []string {
	cardWidth := max(20, m.transcript.Width())
	contentWidth := max(1, cardWidth-4)
	background, foreground, title := "#252b30", "#dce3e7", "Sigma · status"
	var content string
	switch kind {
	case "startup":
		background, foreground, title = "#273a46", "#d5e5ed", "Workspace"
		content = safeTerminalText(text)
	case "user":
		background, foreground, title = "#213b49", "#e2f4fa", "You"
		content = renderTUIMarkdown(text, contentWidth)
	case "assistant":
		background, foreground, title = "#302a45", "#f0eaff", "Sigma"
		content = renderTUIMarkdown(text, contentWidth)
	case "tool":
		background, foreground, title = "#26372e", "#e0f1e5", "Tool"
		content = safeTerminalText(text)
	case "error":
		background, foreground, title = "#492727", "#ffe7e7", "Sigma · error"
		content = safeTerminalText(text)
	default:
		content = safeTerminalText(text)
	}
	style := lipgloss.NewStyle().Background(lipgloss.Color(background)).Foreground(lipgloss.Color(foreground))
	rows := []string{style.Render(strings.Repeat(" ", cardWidth))}
	for _, row := range []string{title, content} {
		for _, contentLine := range strings.Split(row, "\n") {
			contentLine = truncateTUIWidth(contentLine, contentWidth)
			padding := max(0, contentWidth-lipgloss.Width(contentLine))
			rows = append(rows, style.Render("  "+contentLine+strings.Repeat(" ", padding)+"  "))
		}
	}
	rows = append(rows, style.Render(strings.Repeat(" ", cardWidth)))
	return rows
}

func (m *tuiModel) sendEvent(event agent.Event) { m.onAgentEvent(event) }

func interactiveTUISession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool, startingCommit string, runtimeConfig config.Runtime, projectGuidance sigmaagent.ProjectGuidance, extensions *workflow.Host, bridge *tuiBridge) (agent.Result, error) {
	bridge.send(tuiReadyMsg{sessionID: session.ID, workspace: workspace, modelID: session.Model.ModelID(), contextLimit: runtimeConfig.MaxContextTokens, messages: session.Messages()})
	bridge.send(tuiCommandsMsg(extensions.Commands()))
	type outcome struct {
		result agent.Result
		err    error
	}
	var total agent.Result
	var active chan outcome
	exitWhenDone := false
	pendingPrompt := ""
	startRun := func(prompt string, continuation bool) {
		active = make(chan outcome, 1)
		go func(done chan outcome) {
			var item outcome
			if continuation {
				item.result, item.err = session.Continue(ctx)
			} else {
				item.result, item.err = session.Prompt(ctx, prompt)
			}
			done <- item
		}(active)
	}
	if resume {
		messages := session.Messages()
		if len(messages) > 0 && messages[len(messages)-1].Role != "assistant" {
			startRun("", true)
			pendingPrompt = strings.TrimSpace(initialPrompt)
		} else if strings.TrimSpace(initialPrompt) != "" {
			startRun(strings.TrimSpace(initialPrompt), false)
		}
	} else if strings.TrimSpace(initialPrompt) != "" {
		startRun(strings.TrimSpace(initialPrompt), false)
	}
	for {
		select {
		case <-ctx.Done():
			if active != nil {
				<-active
			}
			return total, nil
		case item := <-active:
			active = nil
			addResult(&total, item.result)
			if item.err != nil && !errors.Is(item.err, context.Canceled) {
				bridge.notice("Agent error: " + item.err.Error())
			} else {
				bridge.notice("Turn complete")
			}
			if exitWhenDone {
				return total, nil
			}
			if pendingPrompt != "" {
				prompt := pendingPrompt
				pendingPrompt = ""
				startRun(prompt, false)
			}
		case line := <-bridge.inputs:
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "/") {
				name, args, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
				if handled, commandErr := extensions.RunCommand(ctx, name, strings.TrimSpace(args)); handled {
					if commandErr != nil {
						bridge.notice("Extension command error: " + commandErr.Error())
					}
					continue
				}
			}
			if line == "/exit" || line == "/quit" {
				if active != nil {
					exitWhenDone = true
					session.Abort()
					bridge.notice("Stopping the active turn and saving…")
				} else {
					return total, nil
				}
				continue
			}
			if line == "/abort" {
				if active != nil {
					session.Abort()
					bridge.notice("Abort requested")
				} else {
					bridge.notice("No active Agent turn")
				}
				continue
			}
			if strings.HasPrefix(line, "/steer ") || !strings.HasPrefix(line, "/") {
				message := line
				if strings.HasPrefix(line, "/steer ") {
					message = strings.TrimSpace(strings.TrimPrefix(line, "/steer "))
				}
				if active != nil {
					if err := session.Steer(message); err != nil {
						bridge.notice("Could not queue steering: " + err.Error())
					} else {
						bridge.notice("Steering queued for the next safe turn")
					}
				} else if message != "" {
					startRun(message, false)
				}
				continue
			}
			if strings.HasPrefix(line, "/followup ") {
				if active == nil {
					bridge.notice("/followup is available while the Agent is running")
				} else if err := session.FollowUp(strings.TrimSpace(strings.TrimPrefix(line, "/followup "))); err != nil {
					bridge.notice("Could not queue follow-up: " + err.Error())
				} else {
					bridge.notice("Follow-up queued")
				}
				continue
			}
			switch {
			case line == "/help":
				commands := []string{"/help", "/diff", "/tree [entry-id]", "/model [provider/id]", "/abort", "/steer <text>", "/followup <text>", "/exit"}
				for _, command := range extensions.Commands() {
					label := "/" + command.Name
					if command.Description != "" {
						label += " — " + command.Description
					}
					commands = append(commands, label)
				}
				bridge.notice("Commands: " + strings.Join(commands, ", "))
			case line == "/tree" || strings.HasPrefix(line, "/tree "):
				if active != nil {
					bridge.notice("Wait for the current turn to finish before changing session history")
					continue
				}
				entryID := strings.TrimSpace(strings.TrimPrefix(line, "/tree"))
				if entryID == "" {
					bridge.showSessionTree(sessionTreeItems(session))
				} else if err := session.BranchFrom(entryID); err != nil {
					bridge.notice("Could not continue from that history entry: " + err.Error())
				} else {
					bridge.notice("Continued from " + entryID + "; previous history remains available in /tree")
				}
			case line == "/diff":
				if !sigmaagent.HasGitCommit(workspace) {
					bridge.notice("Git diff is unavailable in this folder")
				} else {
					diff, err := gitOutput(workspace, "diff", "--stat", startingCommit)
					if err != nil {
						bridge.notice(err.Error())
					} else if strings.TrimSpace(diff) == "" {
						bridge.notice("No changes yet")
					} else {
						bridge.notice(strings.TrimSpace(diff))
					}
				}
			case line == "/model" || strings.HasPrefix(line, "/model "):
				if active != nil {
					bridge.notice("Wait for the current turn to finish before changing models")
					continue
				}
				selection, action, err := parseModelCommand(strings.TrimSpace(strings.TrimPrefix(line, "/model")), session.Model)
				if err != nil {
					bridge.notice(err.Error())
				} else if action == "show" {
					bridge.notice(fmt.Sprintf("Current model: %s/%s", selection.Provider, selection.Model))
				} else if action == "save" {
					if err := modelconfig.SaveDefaultSelection(selection); err != nil {
						bridge.notice("Could not save model: " + err.Error())
					} else {
						bridge.notice("Default model saved")
					}
				} else {
					model, err := modelconfig.NewConfigured(selection)
					if err == nil {
						if configurable, ok := model.(interface{ ConfigureRuntime(int, int, time.Duration) }); ok {
							configurable.ConfigureRuntime(runtimeConfig.MaxRetries, runtimeConfig.ModelOutputTokens, time.Duration(runtimeConfig.ModelTimeoutMS)*time.Millisecond)
						}
						err = session.SetModel(model)
					}
					if err != nil {
						bridge.notice("Could not switch model: " + err.Error())
					} else {
						bridge.send(tuiModelSelectionMsg(selection))
						bridge.notice("Switched model to " + selection.Provider + "/" + selection.Model)
					}
				}
			case strings.HasPrefix(line, "/skill:"):
				invocation := strings.TrimPrefix(line, "/skill:")
				name, task, ok := strings.Cut(invocation, " ")
				if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(task) == "" {
					bridge.notice("Usage: /skill:<name> <task>")
					continue
				}
				prompt, err := projectGuidance.ExplicitSkillPrompt(strings.TrimSpace(name), task)
				if err != nil {
					bridge.notice("Could not invoke Skill: " + err.Error())
				} else if active != nil {
					err = session.Steer(prompt)
					if err != nil {
						bridge.notice("Could not queue Skill task: " + err.Error())
					} else {
						bridge.notice("Skill task queued")
					}
				} else {
					startRun(prompt, false)
				}
			default:
				bridge.notice("Unknown command. Use /help")
			}
		}
	}
}

func sessionTreeItems(session *agent.Session) []tuiOverlayItem {
	entries := session.Entries()
	if len(entries) == 0 {
		return nil
	}
	children := make(map[string][]agent.SessionEntry)
	for _, entry := range entries {
		children[entry.ParentID] = append(children[entry.ParentID], entry)
	}
	head := session.HeadID()
	items := make([]tuiOverlayItem, 0, len(entries))
	var visit func(string, int)
	visit = func(parentID string, depth int) {
		branch := children[parentID]
		for index, entry := range branch {
			connector := "├─ "
			if index == len(branch)-1 {
				connector = "└─ "
			}
			label := entry.Type
			if entry.Type == "message" && entry.Message != nil {
				label = entry.Message.Role
				for _, block := range entry.Message.Content {
					if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
						label += ": " + truncateText(strings.TrimSpace(block.Text), 56)
						break
					}
					if block.Type == "tool_use" {
						label += ": " + block.Name
						break
					}
				}
			} else if entry.Type == "model_change" && entry.ModelID != "" {
				label += ": " + entry.ModelID
			}
			label = strings.Repeat("  ", depth) + connector + label + "  [" + entry.ID + "]"
			if entry.ID == head {
				label += "  ← current"
			}
			items = append(items, tuiOverlayItem{label: label, value: entry.ID})
			visit(entry.ID, depth+1)
		}
	}
	visit("", 0)
	return items
}

func toolArgsSummary(raw json.RawMessage, width int) string {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return truncateText(safeTerminalText(string(raw)), width)
	}
	for _, key := range []string{"command", "path", "query", "pattern", "url", "name", "action", "purpose"} {
		value, ok := values[key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil && strings.TrimSpace(text) != "" {
			return truncateText(safeTerminalText(strings.TrimSpace(text)), width)
		}
	}
	return truncateText(safeTerminalText(strings.TrimSpace(string(raw))), width)
}

func compactToolOutput(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "no output"
	}
	lines := strings.Split(strings.ReplaceAll(trimmed, "\r\n", "\n"), "\n")
	first := strings.TrimSpace(lines[0])
	if first == "" {
		first = "output"
	}
	preview := truncateText(safeTerminalText(first), 120)
	if len(lines) > 1 {
		preview += fmt.Sprintf(" · %d lines", len(lines))
	}
	return preview
}

func formatDuration(milliseconds int64) string {
	if milliseconds < 1000 {
		return fmt.Sprintf("%dms", milliseconds)
	}
	return fmt.Sprintf("%.1fs", float64(milliseconds)/1000)
}

func formatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1000000 {
		return fmt.Sprintf("%.0fk", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(tokens)/1000000)
}

func safeTerminalText(value string) string {
	value = strings.ToValidUTF8(value, "�")
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteRune(r)
		case '\t':
			b.WriteString("  ")
		default:
			if !unicode.IsControl(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func renderTUIMarkdown(value string, width int) string {
	if width < 20 {
		width = 80
	}
	renderer, err := glamour.NewTermRenderer(glamour.WithWordWrap(width))
	if err != nil {
		return value
	}
	rendered, err := renderer.Render(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(rendered)
}
