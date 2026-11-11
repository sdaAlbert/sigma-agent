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
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	"github.com/sdaAlbert/sigma-agent/internal/config"
	"github.com/sdaAlbert/sigma-agent/internal/fsx"
	modelconfig "github.com/sdaAlbert/sigma-agent/internal/model"
	"github.com/sdaAlbert/sigma-agent/internal/workflow"
	workspaceagent "github.com/sdaAlbert/sigma-agent/internal/workspace"
)

type tuiBridge struct {
	program            *tea.Program
	inputs             chan string
	appContext         context.Context
	runners            sync.WaitGroup
	mu                 sync.Mutex
	pendingSessionNext *tuiNewSessionMsg
}

func (b *tuiBridge) send(msg tea.Msg) {
	if b != nil && b.program != nil {
		b.program.Send(msg)
	}
}

func (b *tuiBridge) sendEvent(event agent.Event)            { b.send(tuiAgentEventMsg(event)) }
func (b *tuiBridge) notice(message string)                  { b.send(tuiNoticeMsg(safeTerminalText(message))) }
func (b *tuiBridge) output(message string)                  { b.send(tuiOutputMsg(safeTerminalText(message))) }
func (b *tuiBridge) failure(message string)                 { b.send(tuiErrorMsg(safeTerminalText(message))) }
func (b *tuiBridge) showSessionTree(items []tuiOverlayItem) { b.send(tuiTreeItemsMsg(items)) }

func (b *tuiBridge) requestSession(next tuiNewSessionMsg) {
	b.mu.Lock()
	b.pendingSessionNext = &next
	b.mu.Unlock()
}

func (b *tuiBridge) takeSessionRequest() *tuiNewSessionMsg {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.pendingSessionNext
	b.pendingSessionNext = nil
	return next
}

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
type tuiTurnStartMsg struct{}
type tuiNoticeMsg string
type tuiStatusClearMsg string
type tuiOutputMsg string
type tuiErrorMsg string
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
type tuiNewSessionMsg struct {
	resumeID, workspace string
	reload              bool
}
type tuiForkItemsMsg []tuiOverlayItem
type tuiResumeItemsMsg []tuiOverlayItem
type tuiThinkingItemsMsg []tuiOverlayItem
type tuiThinkingSettingMsg struct {
	effort   string
	deepSeek bool
}
type tuiModelsLoadedMsg struct {
	provider string
	models   []modelconfig.ModelInfo
	err      error
}
type tuiRunDoneMsg struct {
	err  error
	next *tuiNewSessionMsg
}
type tuiStartMsg struct{}
type tuiStartupInfoMsg struct {
	workspace string
	info      tuiStartupInfo
}

type tuiOverlayItem struct {
	label string
	value string
}

type tuiToolOutput struct {
	tool, args, output, summary string
	error                       string
	status                      string
	line                        int
	duration                    int64
}

type tuiStartupInfo struct {
	contexts                  []string
	skills                    []string
	extensions                []string
	projectIntegrationsPaused bool
	projectTrustReadError     bool
}

type tuiCompletion struct {
	line, start, end int
	items            []tuiOverlayItem
}

type persistedTUISettings struct {
	DeepSeekThinking bool   `json:"deepseek_thinking"`
	DockerNetwork    bool   `json:"docker_network"`
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
}

type tuiRunner func(interactiveSetupOptions, *tuiBridge) error

type tuiModel struct {
	input                 textarea.Model
	transcript            viewport.Model
	bridge                *tuiBridge
	setup                 interactiveSetupOptions
	runner                tuiRunner
	phase                 string
	secretFor             string
	lines                 []string
	width                 int
	height                int
	active                bool
	turnRunning           bool
	abortRequested        bool
	turnPaused            bool
	quitting              bool
	status                string
	lastError             string
	overlay               string
	overlayTitle          string
	overlayItems          []tuiOverlayItem
	overlayIndex          int
	completion            *tuiCompletion
	suggestions           []tuiOverlayItem
	suggestionIndex       int
	catalogProvider       string
	catalogLoading        bool
	catalogModels         []modelconfig.ModelInfo
	history               []string
	historyIndex          int
	historyDraft          string
	expandOutputs         bool
	toolOutputs           map[string]*tuiToolOutput
	assistantText         string
	assistantLine         int
	thinkingText          string
	thinkingLine          int
	modelStyle            lipgloss.Style
	mutedStyle            lipgloss.Style
	brandStyle            lipgloss.Style
	frameStyle            lipgloss.Style
	hintStyle             lipgloss.Style
	statusBarStyle        lipgloss.Style
	confirmResult         chan bool
	commands              []workflow.Command
	startupInfo           tuiStartupInfo
	startupLoading        bool
	showStartupDetails    bool
	sessionID             string
	contextLimit          int
	inputTokens           int
	outputTokens          int
	cacheReadTokens       int
	cacheWriteTokens      int
	latestContext         int
	cacheHitRate          float64
	newSessionRequested   bool
	newSessionResumeID    string
	newSessionWorkspace   string
	reloadSession         bool
	pendingStartupCommand string
}

func startTUI(setup interactiveSetupOptions, runner tuiRunner) error {
	if saved, err := loadTUISettings(); err == nil && saved != nil {
		setup.deepSeekThinking = saved.DeepSeekThinking
		setup.allowNetwork = saved.DockerNetwork
		if saved.ReasoningEffort != "" {
			setup.modelReasoningEffort = saved.ReasoningEffort
		}
	}
	input := textarea.New()
	input.Placeholder = "Ask Sigma or enter a /command"
	input.Prompt = "› "
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.SetWidth(80)
	input.CharLimit = 64 * 1024
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "new line"))
	input.Focus()
	if absolute, absErr := filepath.Abs(setup.workspace); absErr == nil {
		setup.workspace = absolute
	}
	transcript := viewport.New()
	transcript.SetWidth(80)
	transcript.SetHeight(18)
	transcript.HighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#111111")).Background(lipgloss.Color("#ffffff"))
	app := &tuiModel{
		input: input, transcript: transcript, setup: setup, runner: runner, phase: "starting", active: true,
		assistantLine:  -1,
		thinkingLine:   -1,
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
	appContext, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	bridge := &tuiBridge{inputs: make(chan string, 32), appContext: appContext}
	app.bridge = bridge
	program := tea.NewProgram(app)
	bridge.program = program
	_, err := program.Run()
	cancelApp()
	bridge.runners.Wait()
	return err
}

func inspectTUIStartupInfo(repo, externalInstructionsFile string) tuiStartupInfo {
	info := tuiStartupInfo{}
	scopes, err := packageScopes(repo)
	if err != nil {
		return info
	}
	contributedSkills, _ := packageSkills(scopes)
	executableScopes, trusted, trustErr := executablePackageScopes(scopes, repo)
	info.projectTrustReadError = trustErr != nil
	info.projectIntegrationsPaused = !trusted && projectHasExecutableResources(repo)
	guidance, err := workspaceagent.LoadProjectGuidanceWithProfile(repo, contributedSkills, externalInstructionsFile)
	if err == nil {
		if strings.TrimSpace(guidance.Instructions) != "" {
			if data, readErr := os.ReadFile(filepath.Join(repo, "AGENTS.md")); readErr == nil && strings.TrimSpace(string(data)) != "" {
				info.contexts = append(info.contexts, "AGENTS.md")
			}
		}
		if strings.TrimSpace(guidance.Memory) != "" {
			info.contexts = append(info.contexts, filepath.Join(".sigma-agent", "MEMORY.md"))
		}
		if guidance.ExternalInstructionsPath != "" {
			info.contexts = append(info.contexts, filepath.Base(guidance.ExternalInstructionsPath))
		}
		for _, skill := range guidance.Skills {
			info.skills = append(info.skills, skill.Name)
		}
	}
	info.extensions = packageExtensionLabels(executableScopes)
	sort.Strings(info.contexts)
	sort.Strings(info.skills)
	sort.Strings(info.extensions)
	return info
}

func (m *tuiModel) startupLines() []string {
	repository := "Sigma edits this folder directly; changes appear here immediately."
	lines := strings.Split(renderSigmaMark(), "\n")
	lines = append(lines,
		m.brandStyle.Render("Sigma Agent")+m.mutedStyle.Render(" · v"+sigmaAgentVersion),
		"Ask Sigma to explain the project, make a change, or run a verification task.",
		repository,
		"",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4d4d")).Render("[Context]"),
	)
	if m.startupLoading {
		lines = append(lines, "  Scanning project context…")
	} else if len(m.startupInfo.contexts) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, item := range m.startupInfo.contexts {
			lines = append(lines, "  "+item)
		}
	}
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4d4d"))
	lines = append(lines, "", sectionStyle.Render("[Skills]"))
	if m.startupLoading {
		lines = append(lines, "  Scanning installed Skills…")
	} else if len(m.startupInfo.skills) == 0 {
		lines = append(lines, "  (none)")
	} else {
		lines = append(lines, "  "+strings.Join(m.startupInfo.skills, ", "))
	}
	lines = append(lines, "", sectionStyle.Render("[Extensions]"))
	if m.startupLoading {
		lines = append(lines, "  Scanning installed extensions…")
	} else if len(m.startupInfo.extensions) == 0 {
		lines = append(lines, "  (none)")
	} else {
		lines = append(lines, "  "+strings.Join(m.startupInfo.extensions, ", "))
	}
	if m.startupInfo.projectIntegrationsPaused {
		lines = append(lines, "  Project extensions and MCP are paused · use /trust to enable")
	}
	if m.startupInfo.projectTrustReadError {
		lines = append(lines, "  Project trust settings could not be read · project integrations remain paused")
	}
	if m.showStartupDetails {
		thinkingStatus := "off"
		if m.setup.deepSeekThinking {
			thinkingStatus = "on"
		}
		lines = append(lines, "", "Setup: /model · /login · /resume · /settings · /trust (DeepSeek thinking: "+thinkingStatus+")", "Enter sends · Shift+Enter adds a line · Ctrl+O hides these details")
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
	info, err := os.Stat(m.setup.workspace)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("workspace folder is unavailable: %s", m.setup.workspace)
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
	height := inputVisualLineCount(m.input.Value(), m.input.Width())
	if height > 5 {
		height = 5
	}
	if height < 1 {
		height = 1
	}
	m.input.SetHeight(height)
	m.syncTranscriptHeight()
}

func inputVisualLineCount(value string, width int) int {
	if width < 1 {
		width = 1
	}
	height := 0
	for _, line := range strings.Split(value, "\n") {
		lineWidth := ansi.StringWidth(line)
		lines := max(1, (lineWidth+width-1)/width)
		height += lines
	}
	return height
}

func (m *tuiModel) Init() tea.Cmd {
	workspace := m.setup.workspace
	externalInstructionsFile := m.setup.projectInstructionsFile
	return tea.Batch(func() tea.Msg {
		return tuiStartupInfoMsg{workspace: workspace, info: inspectTUIStartupInfo(workspace, externalInstructionsFile)}
	}, func() tea.Msg { return tuiStartMsg{} })
}

func (m *tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.MouseWheelMsg:
		// Let the viewport own wheel scrolling so it navigates transcript history.
		m.transcript, _ = m.transcript.Update(message)
		return m, nil
	case tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseMotionMsg:
		// Mouse buttons are otherwise left to the terminal; wheel events above
		// are the one interaction Sigma needs to scroll its alternate-screen view.
		return m, nil
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
		m.status = ""
		m.input.Placeholder = "Ask Sigma or enter a /command"
		m.syncTranscriptHeight()
		m.bridge.runners.Add(1)
		return m, func() tea.Msg {
			defer m.bridge.runners.Done()
			err := m.runner(m.setup, m.bridge)
			return tuiRunDoneMsg{err: err, next: m.bridge.takeSessionRequest()}
		}
	case tuiStartupInfoMsg:
		if filepath.Clean(msg.workspace) == filepath.Clean(m.setup.workspace) {
			m.startupInfo = msg.info
			m.startupLoading = false
			m.lines = m.startupLines()
			m.refreshTranscript(true)
		}
	case tuiReadyMsg:
		m.phase = "chat"
		m.sessionID = msg.sessionID
		m.contextLimit = msg.contextLimit
		m.expandOutputs = false
		if provider, modelID, ok := strings.Cut(msg.modelID, "/"); ok && modelID != "" {
			if normalized, valid := modelconfig.NormalizeProvider(provider); valid {
				m.setup.model = modelconfig.Selection{Provider: normalized, Model: modelID}
			}
		}
		m.input.Placeholder = "Ask Sigma or enter a /command"
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
		if m.pendingStartupCommand != "" {
			command := m.pendingStartupCommand
			m.pendingStartupCommand = ""
			select {
			case m.bridge.inputs <- command:
			default:
				m.status = "Input queue is full."
			}
		}
	case tuiRunDoneMsg:
		if msg.next != nil {
			m.newSessionRequested = true
			m.newSessionResumeID = msg.next.resumeID
			m.newSessionWorkspace = msg.next.workspace
			m.reloadSession = msg.next.reload
		}
		wasStarting := m.phase == "starting"
		m.active = false
		m.turnRunning = false
		m.abortRequested = false
		m.turnPaused = false
		m.status = ""
		m.input.Placeholder = "Ask Sigma or enter a /command"
		if m.newSessionRequested {
			reloadSession := m.reloadSession
			m.newSessionRequested = false
			m.reloadSession = false
			m.phase = "setup"
			m.setup.initialPrompt = ""
			m.setup.sessionID = ""
			m.setup.resumeID = m.newSessionResumeID
			m.newSessionResumeID = ""
			if m.newSessionWorkspace != "" {
				m.setup.workspace = m.newSessionWorkspace
				m.newSessionWorkspace = ""
			}
			m.sessionID = ""
			m.expandOutputs = false
			m.input.Reset()
			if !reloadSession {
				m.inputTokens, m.outputTokens = 0, 0
				m.cacheReadTokens, m.cacheWriteTokens = 0, 0
				m.latestContext, m.contextLimit = 0, 0
				m.cacheHitRate = 0
			}
			m.assistantText = ""
			m.assistantLine = -1
			m.lines = m.startupLines()
			m.status = ""
			m.syncInputHeight()
			m.refreshTranscript(true)
			if reloadSession {
				m.phase = "starting"
				m.active = true
				m.startupLoading = true
				m.bridge.runners.Add(1)
				return m, tea.Batch(
					func() tea.Msg {
						return tuiStartupInfoMsg{workspace: m.setup.workspace, info: inspectTUIStartupInfo(m.setup.workspace, m.setup.projectInstructionsFile)}
					},
					func() tea.Msg {
						defer m.bridge.runners.Done()
						err := m.runner(m.setup, m.bridge)
						return tuiRunDoneMsg{err: err, next: m.bridge.takeSessionRequest()}
					},
				)
			}
			if msg.err != nil && !isExpectedCancellation(msg.err) {
				m.appendError("Previous session ended with an error: " + msg.err.Error())
			}
		} else if msg.err != nil && !isExpectedCancellation(msg.err) {
			if wasStarting {
				m.startupLoading = false
				if m.sessionID == "" {
					m.phase = "setup"
				} else {
					m.phase = "chat"
				}
			}
			m.syncTranscriptHeight()
			message := "Run failed: " + msg.err.Error()
			if wasStarting {
				message = "Could not start session: " + msg.err.Error()
			}
			if !strings.Contains(m.lastError, msg.err.Error()) {
				m.appendError(message)
			}
		} else if wasStarting {
			m.startupLoading = false
			if m.sessionID == "" {
				m.phase = "setup"
			} else {
				m.phase = "chat"
			}
			m.syncTranscriptHeight()
		}
		if m.quitting {
			return m, tea.Quit
		}
	case tuiNoticeMsg:
		m.status = string(msg)
		status := m.status
		return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tuiStatusClearMsg(status) })
	case tuiStatusClearMsg:
		if m.status == string(msg) {
			m.status = ""
		}
	case tuiOutputMsg:
		m.appendAssistantMessage(string(msg))
	case tuiErrorMsg:
		m.appendError(string(msg))
	case tuiConfirmMsg:
		m.confirmResult = msg.response
		m.openOverlay("confirm", "Extension asks for confirmation", []tuiOverlayItem{{label: "Yes", value: "yes"}, {label: "No", value: "no"}})
		m.appendLine("Extension confirmation: " + msg.message)
	case tuiCommandsMsg:
		m.commands = append([]workflow.Command(nil), msg...)
	case tuiModelSelectionMsg:
		m.setup.model = modelconfig.Selection(msg)
	case tuiForkItemsMsg:
		m.openOverlay("fork", "Fork from a user message", msg)
		m.overlayIndex = len(msg) - 1
	case tuiResumeItemsMsg:
		m.openOverlay("resume", "Resume a saved session", msg)
	case tuiThinkingItemsMsg:
		m.openOverlay("thinking", "Thinking level", msg)
	case tuiThinkingSettingMsg:
		m.setup.modelReasoningEffort = msg.effort
		m.setup.deepSeekThinking = msg.deepSeek
	case tuiModelsLoadedMsg:
		if msg.provider == m.catalogProvider {
			m.catalogLoading = false
			m.catalogModels = msg.models
			if msg.err != nil {
				m.appendError(msg.err.Error())
			}
			m.setSuggestions(m.buildSuggestions(), true)
			m.syncTranscriptHeight()
		}
	case tuiTreeItemsMsg:
		if len(msg) == 0 {
			m.status = "This session has no conversation entries yet."
		} else {
			m.openOverlay("tree", "Session history · choose a point to continue", msg)
		}
	case tuiAgentEventMsg:
		m.onAgentEvent(agent.Event(msg))
	case tuiTurnStartMsg:
		m.turnRunning = true
		m.status = ""
		m.syncTranscriptHeight()
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
		if key == "esc" && m.turnRunning {
			select {
			case m.bridge.inputs <- "/abort":
				m.abortRequested = true
				m.status = ""
			default:
				m.status = "Input queue is full."
			}
			return m, nil
		}
		if key == "esc" && len(m.suggestions) > 0 {
			m.suggestions = nil
			m.suggestionIndex = 0
			m.status = "Suggestions closed"
			m.syncTranscriptHeight()
			return m, nil
		}
		if key == "ctrl+c" {
			if m.active {
				m.bridge.inputs <- "/abort"
				m.abortRequested = true
				m.status = ""
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
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
			if len(m.suggestions) > 0 {
				selected := m.suggestions[m.suggestionIndex]
				if selected.value != "" && strings.TrimSpace(selected.value) == strings.TrimSpace(m.input.Value()) && !strings.HasPrefix(selected.value, "provider:") && !strings.HasPrefix(selected.value, "model:") {
					return m, m.submit()
				}
				return m, m.chooseSuggestion()
			}
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
		if key == "ctrl+end" {
			m.transcript.GotoBottom()
			return m, nil
		}
		if (msg.Code == tea.KeyUp || key == "up" || key == "ctrl+p") && len(m.suggestions) > 0 {
			if m.suggestionIndex > 0 {
				m.suggestionIndex--
			}
			return m, nil
		}
		if (msg.Code == tea.KeyDown || key == "down" || key == "ctrl+n") && len(m.suggestions) > 0 {
			if m.suggestionIndex+1 < len(m.suggestions) {
				m.suggestionIndex++
			}
			return m, nil
		}
		if key == "up" && m.atHistoryPreviousBoundary() {
			m.browseHistory(-1)
			return m, m.refreshSuggestions()
		}
		if key == "down" && m.atHistoryNextBoundary() {
			if len(m.suggestions) > 0 {
				if m.suggestionIndex+1 < len(m.suggestions) {
					m.suggestionIndex++
				}
				return m, nil
			}
			m.browseHistory(1)
			return m, m.refreshSuggestions()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	m.syncInputHeight()
	return m, tea.Batch(cmd, m.refreshSuggestions())
}

func (m *tuiModel) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		if m.phase == "setup" {
			value = strings.TrimSpace(m.setup.initialPrompt)
		}
		if value == "" {
			return nil
		}
	}
	m.suggestions = nil
	m.syncTranscriptHeight()
	if value == "/settings" {
		m.input.Reset()
		m.syncInputHeight()
		m.openSettingsSelector()
		return nil
	}
	if value == "/trust" {
		m.input.Reset()
		m.syncInputHeight()
		m.openProjectTrustSelector()
		return nil
	}
	if value == "/thinking" && m.phase == "chat" {
		m.input.Reset()
		m.syncInputHeight()
		m.openThinkingSelector()
		return nil
	}
	if m.secretFor != "" {
		if err := m.saveSecret(value); err != nil {
			m.appendError("Could not save credential: " + err.Error())
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
		return func() tea.Msg { return tuiStartMsg{} }
	}
	if m.phase == "setup" {
		if strings.HasPrefix(value, "/") {
			m.input.Reset()
			m.syncInputHeight()
			return m.setupCommand(value)
		}
		if err := m.validateStart(); err != nil {
			m.appendError(err.Error())
			return nil
		}
		m.addHistory(value)
		m.setup.initialPrompt = value
		m.input.Reset()
		m.syncInputHeight()
		m.appendUserMessage(value)
		return func() tea.Msg { return tuiStartMsg{} }
	}
	if m.active {
		if value == "/model" {
			m.input.Reset()
			m.syncInputHeight()
			m.openModelSelector()
			return nil
		}
		if strings.HasPrefix(value, "/login") || strings.HasPrefix(value, "/logout") {
			m.input.Reset()
			m.syncInputHeight()
			return m.setupCommand(value)
		}
		if strings.HasPrefix(value, "/") {
			m.input.Reset()
			m.syncInputHeight()
			if value == "/abort" && m.turnRunning {
				m.abortRequested = true
				m.status = ""
			}
			if value == "/quit" {
				m.quitting = true
			}
			select {
			case m.bridge.inputs <- value:
			default:
				m.status = "Input queue is full."
			}
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
	if value == "/quit" {
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
		m.appendLine("Commands: /help, /model, /settings, /trust, /thinking, /login, /logout, /resume, /session, /new, /fork, /clone, /compact, /import, /reload, /name, /export, /diff, /tree, /steer, /followup, /abort, /quit.")
	case "/settings":
		m.openSettingsSelector()
		return nil
	case "/thinking":
		if value == "" {
			m.openThinkingSelector()
			return nil
		}
		if !validTUIThinkingEffort(value) {
			m.appendLine("Choose /thinking and select a supported level.")
			break
		}
		m.setup.modelReasoningEffort = value
		if strings.EqualFold(m.setup.model.Provider, "deepseek") {
			m.setup.deepSeekThinking = value != "off"
		}
		if err := saveTUISettings(m.persistedSettings()); err != nil {
			m.appendError("Could not save thinking setting: " + err.Error())
		}
	case "/model":
		if value == "" {
			m.input.SetValue("/model ")
			m.refreshSuggestions()
			return nil
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
			m.appendLine("Use /model and choose a provider from the suggestions below the input.")
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
		if value != "" {
			m.appendLine("Session IDs are assigned automatically. Use /resume <id> to open a saved session.")
		} else {
			m.appendLine("No session is active yet. A session will be created when you start a task.")
		}
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
		return func() tea.Msg { return tuiStartMsg{} }
	case "/quit":
		m.quitting = true
		return tea.Quit
	default:
		if !strings.HasPrefix(line, "/") {
			m.appendLine("Unknown command. Use /help.")
			return nil
		}
		if err := m.validateStart(); err != nil {
			m.appendError(err.Error())
			return nil
		}
		m.pendingStartupCommand = line
		return func() tea.Msg { return tuiStartMsg{} }
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
	thinking := "off"
	if m.setup.deepSeekThinking {
		thinking = "on"
	}
	return fmt.Sprintf("Folder: %s · Session: %s · Model: %s/%s · DeepSeek thinking: %s · Docker network: %s", m.setup.workspace, selection, m.setup.model.Provider, m.setup.model.Model, thinking, network)
}

func (m *tuiModel) openModelSelector() {
	m.input.SetValue("/model ")
	m.suggestions = m.buildSuggestions()
	m.suggestionIndex = 0
	m.syncInputHeight()
}

func (m *tuiModel) openSessionSelector() {
	root := strings.TrimSpace(m.setup.workspace)
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			m.appendError("Could not find session directory: " + err.Error())
			return
		}
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

func (m *tuiModel) openProjectTrustSelector() {
	trusted, err := projectTrustDecision(m.setup.workspace)
	if err != nil {
		m.appendError("Could not read project trust settings: " + err.Error())
		return
	}
	trustLabel := "Trust this folder · allow project extensions and MCP servers"
	untrustLabel := "Keep untrusted · pause project extensions and MCP servers"
	if trusted {
		trustLabel = "✓ " + trustLabel + " · current"
	} else {
		untrustLabel = "✓ " + untrustLabel + " · current"
	}
	m.openOverlay("project-trust", "Project trust · choose an access level", []tuiOverlayItem{
		{label: trustLabel, value: "trust"},
		{label: untrustLabel, value: "untrust"},
	})
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func (m *tuiModel) persistedSettings() persistedTUISettings {
	return persistedTUISettings{
		DeepSeekThinking: m.setup.deepSeekThinking,
		DockerNetwork:    m.setup.allowNetwork,
		ReasoningEffort:  m.setup.modelReasoningEffort,
	}
}

func tuiSettingsPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "sigma-agent", "tui-settings.json"), nil
}

func loadTUISettings() (*persistedTUISettings, error) {
	path, err := tuiSettingsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var settings persistedTUISettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func saveTUISettings(settings persistedTUISettings) error {
	path, err := tuiSettingsPath()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".tui-settings-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return fsx.ReplaceFile(temporaryPath, path)
}

func (m *tuiModel) openSettingsSelector() {
	thinkingLabel := "off"
	thinkingValue := "thinking:on"
	if m.setup.deepSeekThinking {
		thinkingLabel, thinkingValue = "on", "thinking:off"
	}
	networkLabel := "off"
	networkValue := "network:on"
	if m.setup.allowNetwork {
		networkLabel, networkValue = "on", "network:off"
	}
	effort := m.setup.modelReasoningEffort
	if effort == "" {
		effort = "max"
	}
	items := []tuiOverlayItem{
		{label: "DeepSeek thinking · " + thinkingLabel + " (toggle)", value: thinkingValue},
		{label: "Reasoning effort · " + effort, value: "effort:menu"},
		{label: "Docker network · " + networkLabel + " (toggle)", value: networkValue},
	}
	m.openOverlay("settings", "Settings · applies to the next session", items)
}

func validTUIThinkingEffort(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "off", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func (m *tuiModel) openThinkingSelector() {
	current := m.setup.modelReasoningEffort
	if current == "" {
		current = "max"
	}
	levels := []string{"low", "medium", "high", "xhigh", "max"}
	if strings.EqualFold(m.setup.model.Provider, "deepseek") {
		if !m.setup.deepSeekThinking {
			current = "off"
		}
		levels = append([]string{"off"}, levels...)
	}
	items := make([]tuiOverlayItem, 0, len(levels))
	for _, effort := range levels {
		label := effort
		if effort == current {
			label = "✓ " + label + " · current"
		}
		items = append(items, tuiOverlayItem{label: label, value: effort})
	}
	m.openOverlay("thinking", "Thinking level · choose an option", items)
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
	inputLines := inputVisualLineCount(m.input.Value(), m.input.Width())
	if inputLines > 5 {
		inputLines = 5
	}
	height := m.height - 5 - (inputLines - 1)
	if m.overlay != "" {
		visible := len(m.overlayItems)
		if visible > 8 {
			visible = 8
		}
		height -= visible + 5
	}
	if len(m.suggestions) > 6 {
		height -= 6
	} else {
		height -= len(m.suggestions)
	}
	if height < 3 {
		height = 3
	}
	m.transcript.SetHeight(height)
}

func (m *tuiModel) inputScreenY() int {
	y := m.transcript.Height()
	if m.overlay != "" {
		y += lipgloss.Height(m.renderOverlay()) + 1
	}
	return y
}

func (m *tuiModel) overlayIndexAtMouseY(y int) int {
	startY := m.transcript.Height() + 3 // top border, blank line, title
	visible := 8
	startIndex := 0
	if m.overlayIndex >= visible {
		startIndex = m.overlayIndex - visible + 1
	}
	index := startIndex + y - startY
	if index < startIndex || index >= len(m.overlayItems) || y < startY || y >= startY+min(visible, len(m.overlayItems)-startIndex) {
		return -1
	}
	return index
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
	case "confirm":
		m.resolveConfirmation(item.value == "yes")
		m.closeOverlay()
		return nil
	case "project-trust":
		m.closeOverlay()
		trusted := item.value == "trust"
		if err := saveProjectTrustDecision(m.setup.workspace, trusted); err != nil {
			m.appendError("Could not save project trust decision: " + err.Error())
			return nil
		}
		state := "untrusted"
		if trusted {
			state = "trusted"
		}
		m.status = "Project integrations " + state + " · use /reload to apply"
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
		if m.phase == "setup" {
			return func() tea.Msg { return tuiStartMsg{} }
		}
		return nil
	case "resume":
		m.closeOverlay()
		select {
		case m.bridge.inputs <- "/resume " + item.value:
		default:
			m.status = "Input queue is full."
		}
		return nil
	case "fork":
		entryID := item.value
		m.closeOverlay()
		select {
		case m.bridge.inputs <- "/fork " + entryID:
		default:
			m.status = "Input queue is full."
		}
		return nil
	case "thinking":
		effort := item.value
		if m.phase != "setup" {
			m.closeOverlay()
			select {
			case m.bridge.inputs <- "/thinking " + effort:
			default:
				m.status = "Input queue is full."
			}
			return nil
		}
		if effort == "off" {
			m.setup.deepSeekThinking = false
		} else {
			m.setup.modelReasoningEffort = effort
			if strings.EqualFold(m.setup.model.Provider, "deepseek") {
				m.setup.deepSeekThinking = true
			}
		}
		m.closeOverlay()
		if err := saveTUISettings(m.persistedSettings()); err != nil {
			m.status = "Could not save thinking setting: " + err.Error()
		} else {
			m.status = "Saved thinking level " + effort
		}
		return nil
	case "settings":
		switch item.value {
		case "thinking:on":
			if !strings.EqualFold(m.setup.model.Provider, "deepseek") {
				m.status = "Select a DeepSeek model before enabling thinking."
				m.closeOverlay()
				return nil
			}
			m.setup.deepSeekThinking = true
		case "thinking:off":
			m.setup.deepSeekThinking = false
		case "network:on":
			m.setup.allowNetwork = true
		case "network:off":
			m.setup.allowNetwork = false
		case "effort:menu":
			m.closeOverlay()
			m.openThinkingSelector()
			return nil
		}
		m.closeOverlay()
		if err := saveTUISettings(m.persistedSettings()); err != nil {
			m.status = "Could not save settings: " + err.Error()
		} else {
			m.status = fmt.Sprintf("Saved settings: DeepSeek thinking %s · Docker network %s", onOff(m.setup.deepSeekThinking), onOff(m.setup.allowNetwork))
		}
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
		label := truncateTUIWidth(safeTerminalText(m.overlayItems[index].label), max(1, m.width-10))
		b.WriteString(style.Render(prefix+label) + "\n")
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
	if !isFileReference {
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
	if isFileReference && m.setup.workspace != "" {
		root = m.setup.workspace
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
	items := m.commandSuggestions(word)
	items = append(items, tuiOverlayItem{label: "/skill:  Invoke an installed Skill", value: "/skill:"})
	if len(items) == 1 {
		m.applyCompletion(&tuiCompletion{line: lineIndex, start: start, end: end}, items[0].value)
	} else if len(items) > 1 {
		m.completion = &tuiCompletion{line: lineIndex, start: start, end: end}
		m.openOverlay("completion", "Commands", items)
	}
}

func (m *tuiModel) setSuggestions(items []tuiOverlayItem, preserveSelection bool) {
	selected := ""
	if preserveSelection && m.suggestionIndex >= 0 && m.suggestionIndex < len(m.suggestions) {
		selected = m.suggestions[m.suggestionIndex].value
	}
	m.suggestions = items
	m.suggestionIndex = 0
	if selected == "" {
		return
	}
	for index, item := range items {
		if item.value == selected {
			m.suggestionIndex = index
			return
		}
	}
}

func (m *tuiModel) refreshSuggestions() tea.Cmd {
	if m.overlay != "" || m.secretFor != "" || strings.Contains(m.input.Value(), "\n") {
		m.suggestions = nil
		m.syncTranscriptHeight()
		return nil
	}
	line := strings.TrimSpace(m.input.Value())
	provider := ""
	if line == "/model" {
		m.setSuggestions(m.providerSuggestions(), true)
		m.syncTranscriptHeight()
		return nil
	}
	if strings.HasPrefix(line, "/model ") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "/model"))
		if providerText, modelPrefix, hasSlash := strings.Cut(rest, "/"); hasSlash {
			provider, _ = modelconfig.NormalizeProvider(providerText)
			if provider != "" && (m.catalogProvider != provider || m.catalogLoading) {
				m.setSuggestions([]tuiOverlayItem{{label: "Loading " + provider + " models…", value: ""}}, true)
				m.syncTranscriptHeight()
				if m.catalogProvider != provider {
					m.catalogProvider = provider
					m.catalogLoading = true
					return func() tea.Msg {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						models, err := modelconfig.ListModels(ctx, provider)
						return tuiModelsLoadedMsg{provider: provider, models: models, err: err}
					}
				}
				return nil
			}
			m.setSuggestions(m.modelSuggestions(provider, modelPrefix), true)
			m.syncTranscriptHeight()
			return nil
		}
		if rest == "" {
			m.setSuggestions(m.providerSuggestions(), true)
			m.syncTranscriptHeight()
			return nil
		}
	}
	if strings.HasPrefix(line, "/login ") {
		prefix := strings.TrimSpace(strings.TrimPrefix(line, "/login"))
		m.setSuggestions(m.providerCommandSuggestions("/login", prefix), true)
		m.syncTranscriptHeight()
		return nil
	}
	if strings.HasPrefix(line, "/logout ") {
		m.setSuggestions(m.providerCommandSuggestions("/logout", strings.TrimSpace(strings.TrimPrefix(line, "/logout"))), true)
		m.syncTranscriptHeight()
		return nil
	}
	if strings.HasPrefix(line, "/thinking ") {
		m.setSuggestions(filterTUISuggestions([]tuiOverlayItem{
			{label: "/thinking off  Turn reasoning off", value: "/thinking off"},
			{label: "/thinking low  Light reasoning", value: "/thinking low"},
			{label: "/thinking medium  Moderate reasoning", value: "/thinking medium"},
			{label: "/thinking high  Deep reasoning", value: "/thinking high"},
			{label: "/thinking xhigh  Extra-high reasoning", value: "/thinking xhigh"},
			{label: "/thinking max  Maximum reasoning", value: "/thinking max"},
		}, strings.TrimSpace(strings.TrimPrefix(line, "/thinking"))), true)
		m.syncTranscriptHeight()
		return nil
	}
	if line == "/model save" {
		m.setSuggestions([]tuiOverlayItem{{label: "/model save  Save current model as default", value: "/model save"}}, true)
		m.syncTranscriptHeight()
		return nil
	}
	if strings.HasPrefix(line, "/") {
		m.setSuggestions(m.commandSuggestions(line), true)
	} else {
		m.setSuggestions(nil, false)
	}
	m.syncTranscriptHeight()
	return nil
}

func (m *tuiModel) buildSuggestions() []tuiOverlayItem {
	line := strings.TrimSpace(m.input.Value())
	if line == "/model" {
		return m.providerSuggestions()
	}
	if strings.HasPrefix(line, "/model ") {
		rest := strings.TrimSpace(strings.TrimPrefix(line, "/model"))
		if providerText, modelPrefix, ok := strings.Cut(rest, "/"); ok {
			if provider, valid := modelconfig.NormalizeProvider(providerText); valid {
				return m.modelSuggestions(provider, modelPrefix)
			}
		}
		if rest == "" {
			return m.providerSuggestions()
		}
	}
	return nil
}

func (m *tuiModel) providerSuggestions() []tuiOverlayItem {
	items := make([]tuiOverlayItem, 0, len(modelconfig.Providers())+1)
	for _, provider := range modelconfig.Providers() {
		configured, err := modelconfig.ProviderHasAPIKey(provider.ID)
		status := "key needed"
		if err == nil && configured {
			status = "ready"
		}
		items = append(items, tuiOverlayItem{label: fmt.Sprintf("%s · %s", provider.Name, status), value: "provider:" + provider.ID})
	}
	items = append(items, tuiOverlayItem{label: "Save current model as default", value: "/model save"})
	return items
}

func (m *tuiModel) modelSuggestions(provider, prefix string) []tuiOverlayItem {
	if m.catalogProvider != provider {
		return []tuiOverlayItem{{label: "Loading " + provider + " models…", value: ""}}
	}
	if m.catalogLoading {
		return []tuiOverlayItem{{label: "Loading " + provider + " models…", value: ""}}
	}
	items := make([]tuiOverlayItem, 0, len(m.catalogModels)+1)
	for _, model := range m.catalogModels {
		if !strings.HasPrefix(strings.ToLower(model.ID), strings.ToLower(prefix)) {
			continue
		}
		label := model.ID
		if model.Name != "" && model.Name != model.ID {
			label += " · " + model.Name
		}
		items = append(items, tuiOverlayItem{label: label, value: "model:" + provider + ":" + model.ID})
	}
	if len(items) == 0 {
		if m.catalogProvider == provider && len(m.catalogModels) == 0 {
			items = append(items, tuiOverlayItem{label: "No catalog available · choose /login " + provider, value: "/login " + provider})
		} else {
			items = append(items, tuiOverlayItem{label: "No matching models", value: ""})
		}
	}
	return items
}

func (m *tuiModel) providerCommandSuggestions(command, prefix string) []tuiOverlayItem {
	items := make([]tuiOverlayItem, 0, len(modelconfig.Providers()))
	for _, provider := range modelconfig.Providers() {
		if strings.HasPrefix(provider.ID, strings.ToLower(prefix)) || strings.HasPrefix(strings.ToLower(provider.Name), strings.ToLower(prefix)) {
			items = append(items, tuiOverlayItem{label: command + " " + provider.ID, value: command + " " + provider.ID})
		}
	}
	return items
}

func (m *tuiModel) commandSuggestions(line string) []tuiOverlayItem {
	commands := []tuiOverlayItem{
		{label: "/help  List commands and their descriptions", value: "/help"},
		{label: "/model  Choose a provider and model from the catalog", value: "/model "},
		{label: "/settings  View or change saved Sigma settings", value: "/settings"},
		{label: "/trust  Choose whether project extensions and MCP may run", value: "/trust"},
		{label: "/thinking  Choose the model reasoning level", value: "/thinking"},
		{label: "/login  Save a provider key or GitHub token", value: "/login"},
		{label: "/logout  Remove a saved provider key or GitHub token", value: "/logout "},
		{label: "/resume  Resume a saved session by ID", value: "/resume "},
		{label: "/hotkeys  Show keyboard and mouse shortcuts", value: "/hotkeys"},
	}
	commands = append(commands,
		tuiOverlayItem{label: "/session  Show details for this session", value: "/session"},
		tuiOverlayItem{label: "/new  Save this session and begin a new one", value: "/new"},
		tuiOverlayItem{label: "/fork  Start a branch from a selected user message", value: "/fork"},
		tuiOverlayItem{label: "/clone  Copy this session at its current history point", value: "/clone"},
		tuiOverlayItem{label: "/compact  Summarize older conversation context", value: "/compact"},
		tuiOverlayItem{label: "/import  Replace this session with a Pi JSONL session", value: "/import "},
		tuiOverlayItem{label: "/reload  Reload extensions, Skills, and project context", value: "/reload"},
		tuiOverlayItem{label: "/name  Set or clear the saved session name", value: "/name "},
		tuiOverlayItem{label: "/export  Export the conversation as Markdown", value: "/export "},
		tuiOverlayItem{label: "/diff  Show current workspace changes", value: "/diff"},
		tuiOverlayItem{label: "/tree  Browse history or continue from a past point", value: "/tree"},
		tuiOverlayItem{label: "/steer  Guide the active turn at its next safe point", value: "/steer "},
		tuiOverlayItem{label: "/followup  Queue a message after the active turn", value: "/followup "},
		tuiOverlayItem{label: "/abort  Interrupt the active turn", value: "/abort"},
	)
	commands = append(commands, tuiOverlayItem{label: "/quit  Save and close Sigma", value: "/quit"})
	for _, command := range m.commands {
		value := "/" + command.Name
		label := value
		if command.Description != "" {
			label += "  " + command.Description
		}
		commands = append(commands, tuiOverlayItem{label: label, value: value})
	}
	return filterTUISuggestions(commands, line)
}

func filterTUISuggestions(items []tuiOverlayItem, prefix string) []tuiOverlayItem {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	filtered := make([]tuiOverlayItem, 0, len(items))
	for _, item := range items {
		value := strings.ToLower(strings.TrimSpace(item.value))
		if prefix == "" || strings.HasPrefix(value, prefix) || strings.HasPrefix(strings.ToLower(item.label), prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (m *tuiModel) chooseSuggestion() tea.Cmd {
	if m.suggestionIndex < 0 || m.suggestionIndex >= len(m.suggestions) {
		return nil
	}
	item := m.suggestions[m.suggestionIndex]
	if strings.HasPrefix(item.value, "provider:") {
		provider := strings.TrimPrefix(item.value, "provider:")
		m.input.SetValue("/model " + provider + "/")
		m.suggestionIndex = 0
		m.syncInputHeight()
		return m.refreshSuggestions()
	}
	if strings.HasPrefix(item.value, "model:") {
		_, remainder, _ := strings.Cut(item.value, ":")
		provider, modelID, _ := strings.Cut(remainder, ":")
		selection := modelconfig.Selection{Provider: provider, Model: modelID}
		if m.phase == "setup" {
			m.setup.model = selection
			m.setup.modelExplicit = true
			m.status = "Selected " + provider + "/" + modelID
			m.input.Reset()
			m.suggestions = nil
			m.syncInputHeight()
			m.appendLine("Selected model " + provider + "/" + modelID)
			return nil
		}
		m.input.Reset()
		m.suggestions = nil
		m.syncInputHeight()
		select {
		case m.bridge.inputs <- "/model " + provider + "/" + modelID:
		default:
			m.status = "Input queue is full."
		}
		return nil
	}
	if item.value == "/model save" {
		m.input.Reset()
		m.suggestions = nil
		m.syncInputHeight()
		if m.phase == "setup" {
			return m.setupCommand(item.value)
		}
		select {
		case m.bridge.inputs <- item.value:
		default:
			m.status = "Input queue is full."
		}
		return nil
	}
	if item.value == "" {
		return nil
	}
	m.input.SetValue(item.value)
	m.suggestionIndex = 0
	m.syncInputHeight()
	return m.refreshSuggestions()
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
	case "model_thinking_delta":
		if strings.TrimSpace(event.Text) == "" {
			return
		}
		if m.thinkingLine < 0 {
			m.thinkingText = ""
			m.thinkingLine = len(m.lines)
			m.lines = append(m.lines, "\x00thinking-message:")
		}
		m.thinkingText += safeTerminalText(event.Text)
		m.lines[m.thinkingLine] = "\x00thinking-message:" + m.thinkingText
		m.refreshTranscript(false)
	case "model_text_delta":
		m.thinkingLine = -1
		if m.assistantLine < 0 {
			m.assistantLine = len(m.lines)
			m.lines = append(m.lines, "\x00assistant-message:")
		}
		m.assistantText += safeTerminalText(event.Text)
		m.lines[m.assistantLine] = "\x00assistant-message:" + m.assistantText
		m.refreshTranscript(false)
	case "turn_start":
		m.turnRunning = true
		m.abortRequested = false
		m.turnPaused = false
		m.lastError = ""
		m.status = ""
		m.syncTranscriptHeight()
		m.assistantText = ""
		m.assistantLine = -1
		m.thinkingText = ""
		m.thinkingLine = -1
	case "turn_end":
		m.turnRunning = false
		m.syncTranscriptHeight()
		if m.assistantText != "" && m.assistantLine >= 0 && m.assistantLine < len(m.lines) {
			m.lines[m.assistantLine] = "\x00assistant-message:" + m.assistantText
			m.refreshTranscript(true)
		}
	case "tool_execution_start":
		m.turnRunning = true
		m.thinkingLine = -1
		m.syncTranscriptHeight()
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
			output.output = safeTerminalText(formatToolOutputForDisplay(event.Tool, event.ResultOutput))
			output.summary = safeTerminalText(summarizeToolOutput(event.Tool, event.ResultOutput, event.ResultSummary))
			output.error = ""
			if event.Error != "" && isExpectedCancellationText(event.Error) {
				output.status = "stopped"
			} else if event.Error != "" {
				output.status = "failed"
				output.error = safeTerminalText(event.Error)
			}
			output.duration = event.Duration
			label := fmt.Sprintf("✓ %s · %s", safeTerminalText(event.Tool), formatDuration(event.Duration))
			if event.Error != "" && isExpectedCancellationText(event.Error) {
				label = fmt.Sprintf("Stopped %s", safeTerminalText(event.Tool))
			} else if event.Error != "" {
				label = fmt.Sprintf("✗ %s · %s", safeTerminalText(event.Tool), truncateText(safeTerminalText(event.Error), 120))
			}
			if output.args != "" {
				label += "  " + output.args
			}
			if output.line >= 0 && output.line < len(m.lines) {
				m.lines[output.line] = "\x00tool-message:" + label
			}
			m.refreshTranscript(false)
		} else {
			label := fmt.Sprintf("✓ %s · %s", safeTerminalText(event.Tool), formatDuration(event.Duration))
			if event.Error != "" && !isExpectedCancellationText(event.Error) {
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
	case "model_error", "agent_error":
		if event.Error != "" && !isExpectedCancellationText(event.Error) {
			m.lastError = event.Error
			m.appendError("Sigma · " + safeTerminalText(event.Error))
		}
	case "agent_aborted":
		m.status = ""
		m.turnRunning = false
		m.turnPaused = m.abortRequested
		m.abortRequested = false
		m.syncTranscriptHeight()
	case "agent_end":
		m.turnRunning = false
		m.turnPaused = m.abortRequested
		m.abortRequested = false
		m.syncTranscriptHeight()
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
	if text == m.lastError {
		return
	}
	m.lastError = text
	m.status = ""
	m.appendCardMessage("error", text)
}

func isExpectedCancellation(err error) bool {
	return err != nil && (errors.Is(err, context.Canceled) || isExpectedCancellationText(err.Error()))
}

func isExpectedCancellationText(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "context canceled") || strings.Contains(message, "context cancelled")
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
	b.WriteString(m.transcript.View())
	b.WriteByte('\n')
	if m.overlay != "" {
		b.WriteString(m.renderOverlay())
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
	if len(m.suggestions) > 0 {
		visible := 6
		start := 0
		if m.suggestionIndex >= visible {
			start = m.suggestionIndex - visible + 1
		}
		end := min(len(m.suggestions), start+visible)
		for index := start; index < end; index++ {
			prefix := "  "
			style := m.mutedStyle
			if index == m.suggestionIndex {
				prefix = "› "
				style = m.modelStyle
			}
			b.WriteString(style.Render(prefix+safeTerminalText(m.suggestions[index].label)) + "\n")
		}
	}
	b.WriteString(m.renderStatusBar())
	b.WriteByte('\n')
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "σ Sigma"
	// Enable mouse reporting so the transcript receives wheel events. Terminals
	// generally require Shift+drag for native text selection while this is active.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *tuiModel) renderStatusBar() string {
	width := m.width
	if width < 20 {
		width = 80
	}
	folderName := filepath.Base(strings.TrimSpace(m.setup.workspace))
	left := folderName
	if left == "" || left == "." {
		left = "current folder"
	}
	if m.status != "" {
		left = strings.Join(strings.Fields(strings.ReplaceAll(safeTerminalText(m.status), "\n", " ")), " ") + " · " + left
	}
	usage := fmt.Sprintf("↑%s ↓%s R%s W%s CH—", formatTokenCount(m.inputTokens), formatTokenCount(m.outputTokens), formatTokenCount(m.cacheReadTokens), formatTokenCount(m.cacheWriteTokens))
	if m.latestContext > 0 {
		usage = fmt.Sprintf("↑%s ↓%s R%s W%s CH%.0f%%", formatTokenCount(m.inputTokens), formatTokenCount(m.outputTokens), formatTokenCount(m.cacheReadTokens), formatTokenCount(m.cacheWriteTokens), m.cacheHitRate)
	}
	left += " · " + usage
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
	right := modelName
	if m.latestContext > 0 && m.contextLimit > 0 {
		right += fmt.Sprintf(" · %.1f%%/%s", float64(m.latestContext)*100/float64(m.contextLimit), formatTokenCount(m.contextLimit))
	}
	if lipgloss.Width(right) > width-9 {
		right = truncateTUIWidth(right, width-9)
	}
	center := ""
	centerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4d4d")).Background(lipgloss.Color("#000000")).Bold(true)
	if m.abortRequested || m.turnPaused {
		center = centerStyle.Render(" pause ")
	} else if m.turnRunning {
		center = centerStyle.Blink(true).Render(" working ")
	}
	centerWidth := lipgloss.Width(center)
	rightWidth := lipgloss.Width(right)
	if centerWidth == 0 {
		left = truncateTUIWidth(left, max(1, width-rightWidth-1))
		padding := max(1, width-lipgloss.Width(left)-rightWidth)
		return m.statusBarStyle.Width(width).MaxHeight(1).Render(left + strings.Repeat(" ", padding) + right)
	}
	centerStart := (width - centerWidth) / 2
	rightStart := width - rightWidth
	leftMax := max(1, min(centerStart-1, rightStart-centerWidth-1))
	left = truncateTUIWidth(left, leftMax)
	leftWidth := lipgloss.Width(left)
	leftGap := max(1, centerStart-leftWidth)
	centerGap := max(1, rightStart-centerStart-centerWidth)
	return m.statusBarStyle.Width(width).MaxHeight(1).Render(left + strings.Repeat(" ", leftGap) + center + strings.Repeat(" ", centerGap) + right)
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
	for index, line := range m.lines {
		if kind, text, ok := messageCard(line); ok {
			if kind == "tool" {
				if output := m.toolOutputAtLineIndex(index); output != nil {
					lines = append(lines, m.renderCompactToolCard(output)...)
					continue
				}
			}
			lines = append(lines, m.renderMessageCard(kind, text)...)
			continue
		}
		if strings.HasPrefix(line, "\x00tool-output:") {
			id := strings.TrimPrefix(line, "\x00tool-output:")
			output := m.toolOutputs[id]
			if !m.expandOutputs {
				continue
			}
			if output == nil || (output.status == "running" && output.output == "") {
				lines = append(lines, m.renderMessageCard("tool", "Output pending")...)
				continue
			}
			if output.output == "" {
				lines = append(lines, m.renderMessageCard("tool", "No output")...)
				continue
			}
			lines = append(lines, m.renderMessageCard("tool", "Output:\n"+strings.TrimSpace(output.output))...)
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
	m.transcript.HighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#111111")).Background(lipgloss.Color("#ffffff"))
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func (m *tuiModel) toolOutputAtLineIndex(line int) *tuiToolOutput {
	for _, output := range m.toolOutputs {
		if output != nil && output.line == line {
			return output
		}
	}
	return nil
}

func (m *tuiModel) renderCompactToolCard(output *tuiToolOutput) []string {
	width := max(20, m.transcript.Width())
	contentWidth := max(1, width-4)
	background, foreground := "#26372e", "#e0f1e5"
	status := "✓"
	if output.status == "running" {
		status = "…"
	} else if output.status == "failed" || output.error != "" {
		status = "✗"
		background, foreground = "#492727", "#ffe7e7"
	}
	line1 := fmt.Sprintf("Tool · %s %s", status, strings.Join(strings.Fields(safeTerminalText(output.tool)), " "))
	if output.status == "running" {
		line1 += " · running"
	} else if output.status == "stopped" {
		line1 += " · stopped"
	} else if output.duration > 0 {
		line1 += " · " + formatDuration(output.duration)
	}
	args := strings.Join(strings.Fields(safeTerminalText(output.args)), " ")
	if args == "" {
		args = "(no arguments)"
	}
	line2 := "args: " + args
	result := strings.Join(strings.Fields(safeTerminalText(output.summary)), " ")
	if output.error != "" {
		result = strings.Join(strings.Fields(output.error), " ")
	}
	if result == "" {
		switch output.status {
		case "running":
			result = "running"
		case "stopped":
			result = "stopped"
		default:
			result = compactToolOutput(output.output)
		}
	}
	if result == "" {
		result = "no output"
	}
	line3 := "↳ " + safeTerminalText(result)
	if output.status != "running" {
		line3 += " · Ctrl+O"
	}
	style := lipgloss.NewStyle().Background(lipgloss.Color(background)).Foreground(lipgloss.Color(foreground))
	rows := make([]string, 0, 3)
	for _, line := range []string{line1, line2, line3} {
		line = truncateTUIWidth(line, contentWidth)
		rows = append(rows, style.Render("  "+line+strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))+"  "))
	}
	return rows
}

func messageCard(line string) (kind, text string, ok bool) {
	for _, candidate := range []string{"startup", "user", "assistant", "thinking", "tool", "error", "system"} {
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
	background, foreground, title := "#252b30", "#dce3e7", ""
	var content string
	switch kind {
	case "startup":
		background, foreground, title = "#273a46", "#d5e5ed", "Workspace"
		content = safeTerminalText(text)
	case "user":
		background, foreground, title = "#213b49", "#e2f4fa", "You"
		content = renderTUIMarkdown(text, contentWidth)
	case "assistant":
		background, foreground, title = "#1d3d70", "#edf4ff", "Sigma"
		content = renderTUIMarkdown(text, contentWidth)
	case "thinking":
		background, foreground, title = "#292735", "#d1c9e8", "Thinking"
		content = safeTerminalText(text)
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
	if kind == "thinking" {
		style = style.Italic(true)
	}
	rows := []string{style.Render(strings.Repeat(" ", cardWidth))}
	cardRows := []string{content}
	if title != "" {
		cardRows = []string{title, content}
	}
	for _, row := range cardRows {
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

func formatTUISessionInfo(session *agent.Session, usage agent.Result) string {
	messages := session.Messages()
	users, assistants, toolCalls, toolResults := 0, 0, 0, 0
	for _, message := range messages {
		switch message.Role {
		case "user":
			users++
		case "assistant":
			assistants++
		}
		for _, block := range message.Content {
			switch block.Type {
			case "tool_use":
				toolCalls++
			case "tool_result":
				toolResults++
			}
		}
	}
	modelID := "unknown"
	if session.Model != nil {
		modelID = session.Model.ModelID()
	}
	file := "In-memory"
	if store, ok := session.Store.(*agent.FileSessionStore); ok {
		file = filepath.Join(store.Directory, session.ID+".json")
	}
	uncached := max(0, usage.InputTokens-usage.CacheReadInputTokens)
	totalTokens := usage.InputTokens + usage.OutputTokens
	var info strings.Builder
	fmt.Fprintf(&info, "Session Info\n\nFile: %s\nID: %s\n", file, session.ID)
	if name := session.DisplayName(); name != "" {
		fmt.Fprintf(&info, "Name: %s\n", name)
	}
	fmt.Fprintf(&info, "Model: %s\n\nMessages\nTotal: %s\nUser: %s\nAssistant: %s\nTools: %s calls, %s results\n\nTokens (this process)\nInput: %s\nCached: %s\nUncached: %s\nOutput: %s\nTotal: %s",
		modelID,
		formatTUIInteger(len(messages)), formatTUIInteger(users), formatTUIInteger(assistants),
		formatTUIInteger(toolCalls), formatTUIInteger(toolResults), formatTUIInteger(usage.InputTokens),
		formatTUIInteger(usage.CacheReadInputTokens), formatTUIInteger(uncached),
		formatTUIInteger(usage.OutputTokens), formatTUIInteger(totalTokens))
	if usage.CacheCreationInputTokens > 0 {
		fmt.Fprintf(&info, "\nCache writes: %s", formatTUIInteger(usage.CacheCreationInputTokens))
	}
	return info.String()
}

func sessionModelProvider(session *agent.Session) string {
	if session == nil || session.Model == nil {
		return ""
	}
	provider, _, _ := strings.Cut(session.Model.ModelID(), "/")
	return provider
}

func formatTUIInteger(value int) string {
	text := fmt.Sprintf("%d", value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func sessionForkItems(session *agent.Session) []tuiOverlayItem {
	var items []tuiOverlayItem
	for _, entry := range session.Entries() {
		if entry.Type != "message" || entry.Message == nil || entry.Message.Role != "user" {
			continue
		}
		var text string
		for _, block := range entry.Message.Content {
			if block.Type == "text" {
				text = strings.TrimSpace(block.Text)
				if text != "" {
					break
				}
			}
		}
		if text == "" {
			continue
		}
		items = append(items, tuiOverlayItem{
			label: entry.ID + " · " + truncateTUIWidth(strings.ReplaceAll(text, "\n", " "), 72),
			value: entry.ID,
		})
	}
	return items
}

func sessionMarkdown(session *agent.Session) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# Sigma session %s\n\n", session.ID)
	if name := session.DisplayName(); name != "" {
		fmt.Fprintf(&output, "**Name:** %s\n\n", name)
	}
	for _, message := range session.Messages() {
		role := strings.Title(message.Role)
		if role == "" {
			role = "Message"
		}
		fmt.Fprintf(&output, "## %s\n\n", role)
		for _, block := range message.Content {
			switch block.Type {
			case "text":
				output.WriteString(strings.TrimSpace(block.Text))
				output.WriteString("\n\n")
			case "tool_use":
				fmt.Fprintf(&output, "**Tool call:** `%s`\n\n```json\n%s\n```\n\n", block.Name, strings.TrimSpace(string(block.Input)))
			case "tool_result":
				fmt.Fprintf(&output, "**Tool result:**\n\n```\n%s\n```\n\n", strings.TrimSpace(block.Content))
			}
		}
	}
	return output.String()
}

func interactiveTUISession(ctx context.Context, session *agent.Session, workspace, initialPrompt string, resume bool, startingCommit string, runtimeConfig config.Runtime, projectGuidance workspaceagent.ProjectGuidance, extensions *workflow.Host, bridge *tuiBridge) (agent.Result, error) {
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
		bridge.send(tuiTurnStartMsg{})
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
				bridge.failure("Agent error: " + item.err.Error())
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
						bridge.failure("Extension command error: " + commandErr.Error())
					}
					continue
				}
			}
			if line == "/new" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before starting a new session")
					continue
				}
				bridge.requestSession(tuiNewSessionMsg{})
				return total, nil
			}
			command, arguments, _ := strings.Cut(line, " ")
			arguments = strings.TrimSpace(arguments)
			if command == "/resume" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before resuming another session")
					continue
				}
				if arguments == "" {
					store, storeErr := agent.NewFileSessionStore(filepath.Join(workspace, "var", "sessions"))
					if storeErr != nil {
						bridge.failure("Could not list saved sessions: " + storeErr.Error())
						continue
					}
					sessions, listErr := store.List()
					if listErr != nil {
						bridge.failure("Could not list saved sessions: " + listErr.Error())
						continue
					}
					items := make([]tuiOverlayItem, 0, len(sessions))
					for _, saved := range sessions {
						items = append(items, tuiOverlayItem{label: fmt.Sprintf("%s · %s · %d messages · %s", saved.ID, saved.ModelID, saved.Messages, saved.UpdatedAt.Local().Format("2006-01-02 15:04")), value: saved.ID})
					}
					if len(items) == 0 {
						bridge.notice("No saved sessions found")
					} else {
						bridge.send(tuiResumeItemsMsg(items))
					}
					continue
				}
				if !agent.ValidSessionID(arguments) {
					bridge.failure("Usage: /resume <saved-session-id>")
					continue
				}
				store, storeErr := agent.NewFileSessionStore(filepath.Join(workspace, "var", "sessions"))
				if storeErr == nil {
					_, storeErr = store.Load(arguments)
				}
				if storeErr != nil {
					bridge.failure("Could not resume session: " + storeErr.Error())
					continue
				}
				bridge.requestSession(tuiNewSessionMsg{resumeID: arguments})
				return total, nil
			}
			if command == "/compact" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before compacting context")
					continue
				}
				prompt := "Call compact_context immediately, by itself, to summarize the existing conversation into an actionable handoff. Do not inspect the workspace, list or search files, call Git, or run any other tools; compaction needs only the conversation already in context. Preserve the current task, constraints, decisions, changed files, verified results, blockers, and next action."
				if arguments != "" {
					prompt += " Pay special attention to: " + arguments
				}
				startRun(prompt, false)
				continue
			}
			if command == "/reload" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before reloading session resources")
					continue
				}
				bridge.requestSession(tuiNewSessionMsg{resumeID: session.ID, workspace: workspace, reload: true})
				return total, nil
			}
			if command == "/import" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before importing a session")
					continue
				}
				importPath, pathErr := parseSessionImportPath(arguments)
				if pathErr != nil {
					bridge.failure(pathErr.Error())
					continue
				}
				imported, importErr := readPiSessionJSONL(importPath, workspace)
				if importErr != nil {
					bridge.failure("Could not import Pi session: " + importErr.Error())
					continue
				}
				processDir, cwdErr := os.Getwd()
				if cwdErr != nil {
					bridge.failure("Could not locate Sigma session storage: " + cwdErr.Error())
					continue
				}
				store, storeErr := agent.NewFileSessionStore(filepath.Join(processDir, "var", "sessions"))
				if storeErr != nil {
					bridge.failure("Could not access Sigma session storage: " + storeErr.Error())
					continue
				}
				if _, statErr := os.Stat(filepath.Join(store.Directory, imported.snapshot.ID+".json")); statErr == nil {
					confirmed, confirmErr := bridge.Confirm(ctx, fmt.Sprintf("Replace Sigma session %s with this Pi session?", imported.snapshot.ID))
					if confirmErr != nil {
						bridge.failure("Could not confirm session replacement: " + confirmErr.Error())
						continue
					}
					if !confirmed {
						bridge.notice("Session import cancelled")
						continue
					}
				}
				if err := saveImportedPiSession(imported, processDir); err != nil {
					bridge.failure("Could not save imported session: " + err.Error())
					continue
				}
				bridge.requestSession(tuiNewSessionMsg{resumeID: imported.snapshot.ID, workspace: imported.workspace})
				return total, nil
			}
			if command == "/thinking" {
				if arguments == "" {
					levels := []string{"low", "medium", "high", "xhigh", "max"}
					if strings.EqualFold(sessionModelProvider(session), "deepseek") {
						levels = append([]string{"off"}, levels...)
					}
					items := make([]tuiOverlayItem, 0, len(levels))
					for _, level := range levels {
						items = append(items, tuiOverlayItem{label: level, value: level})
					}
					bridge.send(tuiThinkingItemsMsg(items))
					continue
				}
				if active != nil {
					bridge.notice("Wait for the current turn to finish before changing thinking level")
					continue
				}
				if !validTUIThinkingEffort(arguments) || arguments == "off" && !strings.EqualFold(sessionModelProvider(session), "deepseek") {
					bridge.failure("Unsupported thinking level for the current model")
					continue
				}
				deepSeekEnabled := runtimeConfig.DeepSeekThinkingEnabled
				if arguments == "off" {
					deepSeekEnabled = false
					if configurable, ok := session.Model.(interface{ ConfigureDeepSeekThinking(bool) }); ok {
						configurable.ConfigureDeepSeekThinking(false)
					}
				} else {
					runtimeConfig.ModelReasoningEffort = arguments
					if configurable, ok := session.Model.(interface{ ConfigureReasoningEffort(string) }); ok {
						configurable.ConfigureReasoningEffort(arguments)
					}
					if strings.EqualFold(sessionModelProvider(session), "deepseek") {
						deepSeekEnabled = true
						if configurable, ok := session.Model.(interface{ ConfigureDeepSeekThinking(bool) }); ok {
							configurable.ConfigureDeepSeekThinking(true)
						}
					}
				}
				runtimeConfig.DeepSeekThinkingEnabled = deepSeekEnabled
				bridge.send(tuiThinkingSettingMsg{effort: runtimeConfig.ModelReasoningEffort, deepSeek: deepSeekEnabled})
				if err := saveTUISettings(persistedTUISettings{DeepSeekThinking: deepSeekEnabled, DockerNetwork: runtimeConfig.AllowContainerNetwork, ReasoningEffort: runtimeConfig.ModelReasoningEffort}); err != nil {
					bridge.failure("Could not save thinking setting: " + err.Error())
				} else {
					bridge.notice("Thinking level set to " + arguments)
				}
				continue
			}
			if command == "/name" {
				if arguments == "" {
					name := session.DisplayName()
					if name == "" {
						name = "(unnamed)"
					}
					bridge.output("Session name: " + name)
				} else if active != nil {
					bridge.notice("Wait for the current turn to finish before renaming this session")
				} else {
					if strings.EqualFold(arguments, "clear") {
						arguments = ""
					}
					if err := session.SetName(arguments); err != nil {
						bridge.failure("Could not set session name: " + err.Error())
					} else if session.DisplayName() == "" {
						bridge.notice("Session name cleared")
					} else {
						bridge.notice("Session name set to " + session.DisplayName())
					}
				}
				continue
			}
			if command == "/export" {
				if active != nil {
					bridge.notice("Wait for the current turn to finish before exporting this session")
					continue
				}
				exportPath := arguments
				if exportPath == "" {
					exportPath = filepath.Join(workspace, "var", "sessions", "exports", session.ID+".md")
				} else if !filepath.IsAbs(exportPath) {
					exportPath = filepath.Join(workspace, exportPath)
				}
				err := os.MkdirAll(filepath.Dir(exportPath), 0o700)
				if err == nil {
					err = os.WriteFile(exportPath, []byte(sessionMarkdown(session)), 0o600)
				}
				if err != nil {
					bridge.failure("Could not export session: " + err.Error())
				} else {
					bridge.output("Session exported to " + exportPath)
				}
				continue
			}
			if line == "/hotkeys" {
				bridge.output("Keyboard shortcuts\nEnter · send message / choose highlighted command\nShift+Enter · insert a line\n↑ / ↓ · move through suggestions or history\nPageUp / PageDown · scroll conversation\nCtrl+End · jump to latest message\nCtrl+O · expand or collapse tool output\nCtrl+L · choose model\nCtrl+R · resume a session\nEsc · close the current selector\nShift+left-drag · select and copy text using the terminal")
				continue
			}
			if line == "/fork" {
				items := sessionForkItems(session)
				if len(items) == 0 {
					bridge.notice("No user messages available to fork from")
				} else {
					bridge.send(tuiForkItemsMsg(items))
				}
				continue
			}
			if strings.HasPrefix(line, "/fork ") || line == "/clone" {
				entryID := session.HeadID()
				if strings.HasPrefix(line, "/fork ") {
					entryID = strings.TrimSpace(strings.TrimPrefix(line, "/fork "))
				} else if entryID == "" {
					bridge.notice("Nothing to clone yet")
					continue
				}
				if active != nil {
					bridge.notice("Wait for the current turn to finish before branching this session")
					continue
				}
				cloneID, idErr := newRunID()
				if idErr == nil {
					idErr = copySessionMetadata(session, cloneID)
				}
				if idErr == nil {
					idErr = session.CloneAt(cloneID, entryID)
				}
				if idErr != nil {
					bridge.failure("Could not create session branch: " + idErr.Error())
					continue
				}
				bridge.requestSession(tuiNewSessionMsg{resumeID: cloneID})
				return total, nil
			}
			if line == "/session" || strings.HasPrefix(line, "/session ") {
				bridge.output(formatTUISessionInfo(session, total))
				continue
			}
			if line == "/quit" {
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
						bridge.failure("Could not queue steering: " + err.Error())
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
					bridge.failure("/followup is available while the Agent is running")
				} else if err := session.FollowUp(strings.TrimSpace(strings.TrimPrefix(line, "/followup "))); err != nil {
					bridge.failure("Could not queue follow-up: " + err.Error())
				} else {
					bridge.notice("Follow-up queued")
				}
				continue
			}
			switch {
			case line == "/help":
				commands := []string{
					"/help — list commands and descriptions", "/model — choose provider and model", "/settings — change saved settings", "/trust — allow or pause project integrations",
					"/thinking — set reasoning level", "/login — save credentials", "/logout — remove credentials",
					"/resume <id> — resume a saved session", "/session — show current session details", "/hotkeys — show shortcuts",
					"/new — start a new session", "/fork — branch from a user message", "/clone — copy the current session point",
					"/compact [focus] — summarize older context", "/import <path> — import a Pi JSONL session", "/reload — reload session resources",
					"/name [text|clear] — name this session", "/export [path] — export Markdown",
					"/tree [entry-id] — browse or branch history", "/diff — show workspace changes", "/steer <text> — guide the active turn",
					"/followup <text> — queue a follow-up", "/abort — interrupt the active turn", "/quit — save and close Sigma",
				}
				for _, command := range extensions.Commands() {
					label := "/" + command.Name
					if command.Description != "" {
						label += " — " + command.Description
					}
					commands = append(commands, label)
				}
				bridge.output("Commands: " + strings.Join(commands, ", ") + "\nUse the mouse wheel to scroll history; hold Shift while dragging to select and copy text.")
			case line == "/tree" || strings.HasPrefix(line, "/tree "):
				if active != nil {
					bridge.notice("Wait for the current turn to finish before changing session history")
					continue
				}
				entryID := strings.TrimSpace(strings.TrimPrefix(line, "/tree"))
				if entryID == "" {
					bridge.showSessionTree(sessionTreeItems(session))
				} else if err := session.BranchFrom(entryID); err != nil {
					bridge.failure("Could not continue from that history entry: " + err.Error())
				} else {
					bridge.notice("Continued from " + entryID + "; previous history remains available in /tree")
				}
			case line == "/diff":
				if !workspaceagent.HasGitCommit(workspace) {
					bridge.failure("Git diff is unavailable in this folder")
				} else {
					args := []string{"diff", "--stat"}
					if startingCommit != "" {
						args = append(args, startingCommit)
					} else {
						args = append(args, "HEAD")
					}
					args = append(args, "--", ".", ":(exclude).sigma-agent/MEMORY.md", ":(exclude).sigma-agent/container-home", ":(exclude).sigma-agent/command-environment.json", ":(exclude)var/runs", ":(exclude)var/sessions")
					diff, err := gitOutput(workspace, args...)
					if err != nil {
						bridge.failure(err.Error())
					} else if strings.TrimSpace(diff) == "" {
						untracked, listErr := gitOutput(workspace, "ls-files", "--others", "--exclude-standard")
						untracked = filterSigmaManagedGitPaths(untracked)
						if listErr != nil || strings.TrimSpace(untracked) == "" {
							bridge.notice("No changes yet")
						} else {
							bridge.output("Untracked files:\n" + untracked)
						}
					} else {
						untracked, listErr := gitOutput(workspace, "ls-files", "--others", "--exclude-standard")
						untracked = filterSigmaManagedGitPaths(untracked)
						if listErr == nil && strings.TrimSpace(untracked) != "" {
							diff = strings.TrimSpace(diff) + "\nUntracked files:\n" + strings.TrimSpace(untracked)
						}
						bridge.output(strings.TrimSpace(diff))
					}
				}
			case line == "/model" || strings.HasPrefix(line, "/model "):
				if active != nil {
					bridge.notice("Wait for the current turn to finish before changing models")
					continue
				}
				selection, action, err := parseModelCommand(strings.TrimSpace(strings.TrimPrefix(line, "/model")), session.Model)
				if err != nil {
					bridge.failure(err.Error())
				} else if action == "show" {
					bridge.output(fmt.Sprintf("Current model: %s/%s", selection.Provider, selection.Model))
				} else if action == "save" {
					if err := modelconfig.SaveDefaultSelection(selection); err != nil {
						bridge.failure("Could not save model: " + err.Error())
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
						bridge.failure("Could not switch model: " + err.Error())
					} else {
						bridge.send(tuiModelSelectionMsg(selection))
						bridge.notice("Switched model to " + selection.Provider + "/" + selection.Model)
					}
				}
			case strings.HasPrefix(line, "/skill:"):
				invocation := strings.TrimPrefix(line, "/skill:")
				name, task, ok := strings.Cut(invocation, " ")
				if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(task) == "" {
					bridge.failure("Usage: /skill:<name> <task>")
					continue
				}
				prompt, err := projectGuidance.ExplicitSkillPrompt(strings.TrimSpace(name), task)
				if err != nil {
					bridge.failure("Could not invoke Skill: " + err.Error())
				} else if active != nil {
					err = session.Steer(prompt)
					if err != nil {
						bridge.failure("Could not queue Skill task: " + err.Error())
					} else {
						bridge.notice("Skill task queued")
					}
				} else {
					startRun(prompt, false)
				}
			default:
				bridge.failure("Unknown command. Use /help")
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
	var visit func(string)
	visit = func(parentID string) {
		branch := children[parentID]
		for _, entry := range branch {
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
			label = "• " + label + "  [" + entry.ID + "]"
			if entry.ID == head {
				label += "  ← current"
			}
			items = append(items, tuiOverlayItem{label: label, value: entry.ID})
			visit(entry.ID)
		}
	}
	visit("")
	return items
}

func copySessionMetadata(session *agent.Session, newID string) error {
	if session == nil || !agent.ValidSessionID(session.ID) || !agent.ValidSessionID(newID) {
		return errors.New("invalid source or branch session id")
	}
	store, ok := session.Store.(*agent.FileSessionStore)
	if !ok || store == nil {
		return errors.New("file session storage is unavailable")
	}
	metadata, err := os.ReadFile(filepath.Join(store.Directory, session.ID, "metadata.json"))
	if err != nil {
		return fmt.Errorf("read source session metadata: %w", err)
	}
	if !json.Valid(metadata) {
		return errors.New("source session metadata is invalid JSON")
	}
	branchDir := filepath.Join(store.Directory, newID)
	if err := os.MkdirAll(branchDir, 0o700); err != nil {
		return fmt.Errorf("create branch session metadata folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(branchDir, "metadata.json"), metadata, 0o600); err != nil {
		return fmt.Errorf("write branch session metadata: %w", err)
	}
	return nil
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

func formatToolOutputForDisplay(tool, raw string) string {
	var result struct {
		Path       string `json:"path"`
		Offset     int    `json:"offset"`
		TotalLines int    `json:"total_lines"`
		Content    string `json:"content"`
	}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &result) != nil {
		return raw
	}
	if result.Content != "" && (tool == "read_file" || result.Path != "") {
		return result.Content
	}
	return raw
}

func summarizeToolOutput(tool, raw, fallback string) string {
	var result struct {
		Path       string `json:"path"`
		Offset     int    `json:"offset"`
		TotalLines int    `json:"total_lines"`
		Content    string `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &result) == nil && result.Path != "" && result.Content != "" && (tool == "read_file" || result.TotalLines > 0) {
		lineCount := strings.Count(result.Content, "\n") + 1
		first := result.Offset
		if first < 1 {
			first = 1
		}
		last := first + lineCount - 1
		if result.TotalLines > 0 {
			return fmt.Sprintf("%s · lines %d–%d/%d", result.Path, first, last, result.TotalLines)
		}
		return fmt.Sprintf("%s · %d lines", result.Path, lineCount)
	}
	return fallback
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
	preview := truncateText(safeTerminalText(first), 72)
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
