package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

func (b *tuiBridge) sendEvent(event agent.Event) { b.send(tuiAgentEventMsg(event)) }
func (b *tuiBridge) notice(message string)       { b.send(tuiNoticeMsg(safeTerminalText(message))) }

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
type tuiReadyMsg struct {
	sessionID, workspace string
	messages             []agent.Message
}
type tuiRunDoneMsg struct{ err error }
type tuiStartMsg struct{}

type tuiOverlayItem struct {
	label string
	value string
}

type tuiToolOutput struct {
	tool, args, output string
	status             string
}

type tuiCompletion struct {
	line, start, end int
	items            []tuiOverlayItem
}

type tuiRunner func(interactiveSetupOptions, *tuiBridge) error

type tuiModel struct {
	input         textarea.Model
	transcript    viewport.Model
	bridge        *tuiBridge
	setup         interactiveSetupOptions
	runner        tuiRunner
	phase         string
	secretFor     string
	lines         []string
	width         int
	height        int
	active        bool
	quitting      bool
	status        string
	overlay       string
	overlayTitle  string
	overlayItems  []tuiOverlayItem
	overlayIndex  int
	completion    *tuiCompletion
	history       []string
	historyIndex  int
	historyDraft  string
	expandOutputs bool
	toolOutputs   map[string]*tuiToolOutput
	assistantText string
	assistantLine int
	modelStyle    lipgloss.Style
	userStyle     lipgloss.Style
	toolStyle     lipgloss.Style
	mutedStyle    lipgloss.Style
	confirmResult chan bool
	commands      []workflow.Command
}

func startTUI(setup interactiveSetupOptions, runner tuiRunner) error {
	input := textarea.New()
	input.Placeholder = "Task or /command"
	input.Prompt = "› "
	input.SetHeight(3)
	input.SetWidth(80)
	input.CharLimit = 64 * 1024
	input.SetValue(setup.initialPrompt)
	input.Focus()
	transcript := viewport.New()
	transcript.SetWidth(80)
	transcript.SetHeight(18)
	app := &tuiModel{
		input: input, transcript: transcript, setup: setup, runner: runner, phase: "setup",
		assistantLine: -1,
		historyIndex:  -1,
		toolOutputs:   make(map[string]*tuiToolOutput),
		lines:         []string{"Sigma Agent  |  setup", "Enter a task to start. Use /help for setup commands."},
		modelStyle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		userStyle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
		toolStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		mutedStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
	app.refreshTranscript(false)
	bridge := &tuiBridge{inputs: make(chan string, 32)}
	app.bridge = bridge
	program := tea.NewProgram(app)
	bridge.program = program
	_, err := program.Run()
	return err
}

func (m *tuiModel) Init() tea.Cmd { return nil }

func (m *tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		inputWidth := msg.Width - 6
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.input.SetWidth(inputWidth)
		m.transcript.SetWidth(inputWidth)
		m.height = msg.Height
		m.syncTranscriptHeight()
		m.refreshTranscript(false)
	case tuiStartMsg:
		m.active = true
		m.phase = "starting"
		m.appendLine("Preparing session…")
		return m, func() tea.Msg {
			err := m.runner(m.setup, m.bridge)
			return tuiRunDoneMsg{err: err}
		}
	case tuiReadyMsg:
		m.phase = "chat"
		m.appendLine(fmt.Sprintf("Session %s ready · %s", msg.sessionID, msg.workspace))
		for _, message := range msg.messages {
			for _, block := range message.Content {
				switch block.Type {
				case "text":
					if strings.TrimSpace(block.Text) != "" {
						content := safeTerminalText(strings.TrimSpace(block.Text))
						if message.Role == "assistant" {
							content = renderTUIMarkdown(content, m.width-6)
						}
						m.appendLine(message.Role + "  " + content)
					}
				case "tool_use":
					m.appendLine(m.toolStyle.Render("[" + safeTerminalText(block.Name) + "] " + safeTerminalText(string(block.Input))))
				case "tool_result":
					m.appendLine(m.mutedStyle.Render("[tool result] " + truncateText(block.Content, 400)))
				}
			}
		}
	case tuiRunDoneMsg:
		m.active = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.appendLine("Run failed: " + msg.err.Error())
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
				m.selectOverlayItem()
				return m, nil
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
		if key == "ctrl+enter" || key == "ctrl+s" {
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
			m.expandOutputs = !m.expandOutputs
			m.refreshTranscript(true)
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
	return m, cmd
}

func (m *tuiModel) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return nil
	}
	m.input.Reset()
	if m.secretFor != "" {
		if err := m.saveSecret(value); err != nil {
			m.appendLine("Could not save credential: " + err.Error())
		} else {
			m.appendLine("Credential saved in the user configuration directory.")
		}
		m.secretFor = ""
		m.input.Placeholder = "Task or /command"
		return nil
	}
	if m.phase == "setup" {
		if strings.HasPrefix(value, "/") {
			return m.setupCommand(value)
		}
		m.addHistory(value)
		m.setup.initialPrompt = value
		if err := validateSetupOptions(m.setup); err != nil {
			m.appendLine(err.Error())
			return nil
		}
		m.appendLine(m.userStyle.Render("You  ") + value)
		m.appendLine("Starting task…")
		return func() tea.Msg { return tuiStartMsg{} }
	}
	if m.active {
		if value == "/model" {
			m.openModelSelector()
			return nil
		}
		m.addHistory(value)
		m.appendLine(m.userStyle.Render("You  ") + value)
		select {
		case m.bridge.inputs <- value:
		default:
			m.appendLine("Input queue is full; try again in a moment.")
		}
		return nil
	}
	if value == "/exit" || value == "/quit" {
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
		m.appendLine("Setup: /repo <path>, /model [provider/model-id|save], /login <provider|github>, /logout <provider|github>, /session <id>, /resume <id>, /network on|off, /publish <branch>|off, /github-write on|off, /start, /exit. Ctrl+Enter submits; Ctrl+C exits.")
	case "/repo":
		if value == "" {
			m.appendLine("Usage: /repo <path>")
			break
		}
		path, err := filepath.Abs(value)
		if err == nil {
			var top string
			top, err = gitOutput(path, "rev-parse", "--show-toplevel")
			if err == nil {
				m.setup.repo = strings.TrimSpace(top)
				m.appendLine("Repository: " + m.setup.repo)
			}
		}
		if err != nil {
			m.appendLine("Could not select repository: " + err.Error())
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
				m.appendLine("Could not save default model: " + err.Error())
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
		if strings.EqualFold(value, "github") {
			m.secretFor = "github"
		} else if provider, ok := modelconfig.NormalizeProvider(value); ok {
			m.secretFor = provider
		} else {
			m.appendLine("Choose /login github or a supported model provider.")
			break
		}
		m.input.Placeholder = "Credential (input is masked)"
		m.appendLine("Enter credential, then press Ctrl+Enter. Input is masked on screen.")
	case "/logout":
		var err error
		if strings.EqualFold(value, "github") {
			err = agent.RemoveGitHubToken()
		} else {
			err = modelconfig.RemoveAPIKey(value)
		}
		if err != nil {
			m.appendLine("Could not remove credential: " + err.Error())
		} else {
			m.appendLine("Credential removed.")
		}
	case "/session":
		if !agent.ValidSessionID(value) {
			m.appendLine("Invalid session ID.")
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
	case "/publish":
		if value == "off" {
			m.setup.pushBranch, m.setup.githubWrite = "", false
			m.appendLine("Publishing disabled.")
			break
		}
		checked, err := gitOutput(m.setup.repo, "check-ref-format", "--branch", value)
		if err != nil || strings.TrimSpace(checked) != value {
			m.appendLine("Invalid branch name.")
			break
		}
		m.setup.pushBranch = value
		m.appendLine("Publishing enabled for new branch " + value)
	case "/github-write":
		if value != "on" && value != "off" {
			m.appendLine("Usage: /github-write on|off")
		} else if value == "on" && (!m.setup.githubMCP || m.setup.pushBranch == "" || !agent.GitHubMCPConfigured()) {
			m.appendLine("Enable GitHub MCP, set /publish <branch>, and configure GitHub credentials first.")
		} else {
			m.setup.githubWrite = value == "on"
		}
	case "/start":
		if err := validateSetupOptions(m.setup); err != nil {
			m.appendLine(err.Error())
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
		m.appendLine("Could not find session directory: " + err.Error())
		return
	}
	store, err := agent.NewFileSessionStore(filepath.Join(root, "var", "sessions"))
	if err != nil {
		m.appendLine("Could not read saved sessions: " + err.Error())
		return
	}
	sessions, err := store.List()
	if err != nil {
		m.appendLine("Could not list saved sessions: " + err.Error())
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
	height := m.height - 11
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

func (m *tuiModel) selectOverlayItem() {
	if m.overlayIndex < 0 || m.overlayIndex >= len(m.overlayItems) {
		return
	}
	item := m.overlayItems[m.overlayIndex]
	switch m.overlay {
	case "confirm":
		m.resolveConfirmation(item.value == "yes")
		m.closeOverlay()
	case "model":
		if strings.HasPrefix(item.value, "provider:") {
			provider := strings.TrimPrefix(item.value, "provider:")
			modelID := modelconfig.DefaultModel(provider)
			if modelID == "" {
				m.input.SetValue("/model " + provider + "/")
				m.status = "Enter the provider's model ID, then press Ctrl+Enter."
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
	case "session":
		m.setup.resumeID = item.value
		m.setup.sessionID = ""
		m.status = "Selected session " + item.value
		m.closeOverlay()
	case "completion":
		completion := m.completion
		m.closeOverlay()
		if completion != nil {
			m.applyCompletion(completion, item.value)
		}
	}
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
		return
	}
	if m.historyIndex == -1 {
		return
	}
	if m.historyIndex < len(m.history)-1 {
		m.historyIndex++
		m.input.SetValue(m.history[m.historyIndex])
		return
	}
	m.historyIndex = -1
	m.input.SetValue(m.historyDraft)
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
	commands := []string{"/help", "/repo ", "/model ", "/login ", "/logout ", "/session ", "/resume ", "/network ", "/publish ", "/github-write ", "/start", "/diff", "/tree", "/branch ", "/steer ", "/followup ", "/abort", "/exit", "/skill:"}
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
}

func (m *tuiModel) onAgentEvent(event agent.Event) {
	switch event.Type {
	case "model_text_delta":
		if m.assistantLine < 0 {
			m.assistantLine = len(m.lines)
			m.lines = append(m.lines, "Assistant  ")
		}
		m.assistantText += safeTerminalText(event.Text)
		m.lines[m.assistantLine] = "Assistant  " + m.assistantText
		m.refreshTranscript(false)
	case "turn_start":
		m.assistantText = ""
		m.assistantLine = -1
	case "turn_end":
		if m.assistantText != "" && m.assistantLine >= 0 && m.assistantLine < len(m.lines) {
			m.lines[m.assistantLine] = "Assistant  " + renderTUIMarkdown(m.assistantText, m.width-6)
			m.refreshTranscript(true)
		}
	case "tool_execution_start":
		label := "[" + event.Tool + "]"
		if len(event.Args) > 0 {
			label += " " + safeTerminalText(string(event.Args))
		}
		m.appendLine(m.toolStyle.Render(label))
		if event.ToolCallID != "" {
			m.toolOutputs[event.ToolCallID] = &tuiToolOutput{tool: event.Tool, args: safeTerminalText(string(event.Args)), status: "running"}
			m.appendLine("\x00tool-output:" + event.ToolCallID)
		}
	case "tool_execution_end":
		label := fmt.Sprintf("[%s] finished (%d ms)", event.Tool, event.Duration)
		if event.Error != "" {
			label += " · " + safeTerminalText(event.Error)
		}
		m.appendLine(m.toolStyle.Render(label))
		if output, ok := m.toolOutputs[event.ToolCallID]; ok {
			output.status = event.Status
			output.output = safeTerminalText(event.ResultOutput)
			m.refreshTranscript(false)
		}
		if event.ResultSummary != "" && event.ToolCallID == "" {
			m.appendLine("  " + safeTerminalText(event.ResultSummary))
		}
	case "context_compacted_model":
		m.appendLine(fmt.Sprintf("Context compacted · %d messages dropped · %d summary tokens", event.MessagesDropped, event.SummaryTokens))
		if event.CompactionSummary != "" {
			m.appendLine("  Summary: " + safeTerminalText(event.CompactionSummary))
		}
	case "model_error", "agent_error", "agent_aborted":
		if event.Error != "" {
			m.appendLine("Agent: " + safeTerminalText(event.Error))
		}
	}
}

func (m *tuiModel) appendLine(line string) {
	wasAtBottom := m.transcript.AtBottom()
	m.lines = append(m.lines, line)
	if len(m.lines) > 600 {
		m.lines = append([]string(nil), m.lines[len(m.lines)-600:]...)
	}
	m.refreshTranscript(wasAtBottom)
}

func (m *tuiModel) View() tea.View {
	var b strings.Builder
	if m.phase == "setup" {
		b.WriteString(m.modelStyle.Render("Sigma Agent  ·  Setup") + "\n")
		b.WriteString(m.setupSummary() + "\n\n")
	} else {
		b.WriteString(m.modelStyle.Render("Sigma Agent  ·  "+m.phase) + "\n\n")
	}
	b.WriteString(m.transcript.View())
	b.WriteByte('\n')
	if m.overlay != "" {
		b.WriteString(m.renderOverlay())
	}
	if m.status != "" {
		b.WriteString(m.mutedStyle.Render(m.status) + "\n")
	}
	if m.secretFor != "" {
		value := m.input.Value()
		masked := strings.Repeat("•", utf8.RuneCountInString(value))
		b.WriteString("\n" + masked + "\n")
	} else {
		b.WriteString("\n" + m.input.View() + "\n")
	}
	if m.phase == "setup" {
		b.WriteString(m.mutedStyle.Render("Ctrl+Enter submit · Ctrl+L models · Ctrl+R sessions · Tab complete · Ctrl+C exit"))
	} else {
		b.WriteString(m.mutedStyle.Render("Ctrl+Enter send · Ctrl+O tool output · Ctrl+L models · Ctrl+R sessions · /abort stop"))
	}
	v := tea.NewView(b.String())
	v.MouseMode = tea.MouseModeCellMotion
	return v // Main-screen inline rendering preserves terminal scrollback.
}

func (m *tuiModel) refreshTranscript(keepBottom bool) {
	if m.transcript.Width() == 0 {
		width := m.width - 6
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
		if strings.HasPrefix(line, "\x00tool-output:") {
			id := strings.TrimPrefix(line, "\x00tool-output:")
			output := m.toolOutputs[id]
			if output == nil || (output.status == "running" && output.output == "") {
				lines = append(lines, m.mutedStyle.Render("  [tool output pending]"))
				continue
			}
			if output.output == "" {
				lines = append(lines, m.mutedStyle.Render("  [no output]"))
				continue
			}
			if !m.expandOutputs {
				preview := truncateText(strings.TrimSpace(output.output), 180)
				lines = append(lines, m.mutedStyle.Render("  "+preview+"  (Ctrl+O to expand)"))
				continue
			}
			for _, outputLine := range strings.Split(strings.TrimSpace(output.output), "\n") {
				lines = append(lines, "  "+outputLine)
			}
			continue
		}
		lines = append(lines, line)
	}
	wasAtBottom := keepBottom || m.transcript.AtBottom()
	m.transcript.SetContentLines(lines)
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func (m *tuiModel) sendEvent(event agent.Event) { m.onAgentEvent(event) }

func interactiveTUISession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool, startingCommit string, runtimeConfig config.Runtime, projectGuidance sigmaagent.ProjectGuidance, extensions *workflow.Host, bridge *tuiBridge) (agent.Result, error) {
	bridge.send(tuiReadyMsg{sessionID: session.ID, workspace: workspace, messages: session.Messages()})
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
				commands := []string{"/help", "/diff", "/tree", "/branch <entry-id>", "/model [provider/id]", "/abort", "/steer <text>", "/followup <text>", "/exit"}
				for _, command := range extensions.Commands() {
					label := "/" + command.Name
					if command.Description != "" {
						label += " — " + command.Description
					}
					commands = append(commands, label)
				}
				bridge.notice("Commands: " + strings.Join(commands, ", "))
			case line == "/tree":
				bridge.notice(formatSessionTree(session))
			case line == "/diff":
				diff, err := gitOutput(workspace, "diff", "--stat", startingCommit)
				if err != nil {
					bridge.notice(err.Error())
				} else if strings.TrimSpace(diff) == "" {
					bridge.notice("No changes yet")
				} else {
					bridge.notice(strings.TrimSpace(diff))
				}
			case strings.HasPrefix(line, "/branch "):
				if active != nil {
					bridge.notice("Wait for the current turn to finish before branching")
				} else if err := session.BranchFrom(strings.TrimSpace(strings.TrimPrefix(line, "/branch "))); err != nil {
					bridge.notice("Could not branch: " + err.Error())
				} else {
					bridge.notice("Session branch updated")
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

func formatSessionTree(session *agent.Session) string {
	entries := session.Entries()
	if len(entries) == 0 {
		return "This session has no conversation entries yet."
	}
	var b strings.Builder
	b.WriteString("Session history:\n")
	head := session.HeadID()
	for _, entry := range entries {
		marker := " "
		if entry.ID == head {
			marker = "*"
		}
		detail := entry.Type
		if entry.Message != nil {
			for _, block := range entry.Message.Content {
				if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
					detail += ": " + truncateText(strings.TrimSpace(block.Text), 72)
					break
				}
			}
		}
		fmt.Fprintf(&b, "%s %s  %s\n", marker, entry.ID, detail)
	}
	return b.String()
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
