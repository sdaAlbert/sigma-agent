package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/tracecontext"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ToolHandler func(context.Context, json.RawMessage) (string, error)

type toolTurnContextKey struct{}
type toolCallIDContextKey struct{}
type toolIdempotencyKeyContextKey struct{}
type sessionRunContextKey struct{}
type runMetadataContextKey struct{}

type RunMetadata struct {
	TraceContext tracecontext.Context
	TaskID       string
	RunID        string
	WorkerID     string
	EventSink    EventSink
}

func WithRunMetadata(ctx context.Context, metadata RunMetadata) context.Context {
	return context.WithValue(ctx, runMetadataContextKey{}, metadata)
}

func RunMetadataFromContext(ctx context.Context) (RunMetadata, bool) {
	metadata, ok := ctx.Value(runMetadataContextKey{}).(RunMetadata)
	return metadata, ok
}

// ToolCallID returns the model-supplied ID for the tool call currently being
// authorized or executed.
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDContextKey{}).(string)
	return id
}

// ToolIdempotencyKey returns a durable key for the current tool operation.
// Adapters should forward it to external systems that support idempotency keys.
func ToolIdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(toolIdempotencyKeyContextKey{}).(string)
	return key
}

// SessionRunContext returns the parent agent-run context inside a tool handler.
// Long-running child tasks can outlive their individual tool-call timeout while
// still being cancelled when the parent run ends.
func SessionRunContext(ctx context.Context) context.Context {
	if sessionContext, ok := ctx.Value(sessionRunContextKey{}).(context.Context); ok {
		return sessionContext
	}
	return ctx
}

type ToolRecoveryResult struct {
	Status string // "completed", "failed", "not_found", or "unknown".
	Output string
	Error  string
}

type ToolReplayPolicy string

const (
	// ToolReplayNever is the zero-value policy. Interrupted calls stay unknown
	// until a person or a tool-specific recovery adapter resolves them.
	ToolReplayNever ToolReplayPolicy = "never_replay"
	// ToolReplaySafe permits automatic recovery-time re-execution. Use only for
	// side-effect-free operations or handlers whose target honors the stable key.
	ToolReplaySafe ToolReplayPolicy = "safe_to_replay"
)

const (
	ToolRecoveryCompleted = "completed"
	ToolRecoveryFailed    = "failed"
	ToolRecoveryNotFound  = "not_found"
	ToolRecoveryUnknown   = "unknown"
)

type ToolRecoveryHandler func(context.Context, json.RawMessage, string) (ToolRecoveryResult, error)
type ToolCompensationHandler func(context.Context, json.RawMessage, string) error

// AuthorizationRequest is the reviewable summary shown before a gated tool
// operation runs and persisted in the authorization audit event.
type AuthorizationRequest struct {
	ID        string
	Tool      string
	RiskClass string
	Target    string
	Reason    string
	Args      json.RawMessage
}

// ToolTurn returns the assistant turn in which the current tool call was made.
func ToolTurn(ctx context.Context) (int, bool) {
	turn, ok := ctx.Value(toolTurnContextKey{}).(int)
	return turn, ok
}

type Tool struct {
	Definition        ToolDefinition
	OnDemand          bool             // Hidden from model requests until search(scope=tools) activates it.
	InternalOnly      bool             // Executed through an internal router, never exposed as a model tool.
	ExecutionMode     string           // "parallel" for independent reads; "parallel_write" for path-locked mutations; otherwise serialized.
	ModifiesWorkspace bool             // Successful calls force the normal test-and-diff review gate.
	ReplayPolicy      ToolReplayPolicy // Defaults to never_replay; safe_to_replay is an explicit adapter guarantee.
	Validate          func(json.RawMessage) error
	Handle            ToolHandler
	Recover           ToolRecoveryHandler
	Compensate        ToolCompensationHandler
}

// ToolOperation records the intent and outcome of one side-effect-capable call.
type ToolOperation struct {
	ToolCallID      string           `json:"tool_call_id"`
	Turn            int              `json:"turn"`
	Tool            string           `json:"tool"`
	IdempotencyKey  string           `json:"idempotency_key"`
	CompensationKey string           `json:"compensation_key,omitempty"`
	ReplayPolicy    ToolReplayPolicy `json:"replay_policy,omitempty"`
	Status          string           `json:"status"`
	Args            json.RawMessage  `json:"args,omitempty"`
	Output          string           `json:"output,omitempty"`
	Error           string           `json:"error,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type ToolRegistry struct {
	byName map[string]Tool
	active map[string]bool
	mu     sync.RWMutex
}

func NewToolRegistry(tools []Tool) (*ToolRegistry, error) {
	registry := &ToolRegistry{byName: make(map[string]Tool, len(tools)), active: make(map[string]bool, len(tools))}
	for _, tool := range tools {
		if tool.Definition.Name == "" || tool.Handle == nil {
			return nil, errors.New("every tool needs a name and handler")
		}
		if _, exists := registry.byName[tool.Definition.Name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", tool.Definition.Name)
		}
		switch tool.Definition.Name {
		case sessionSearchToolName, sessionHistorySearchToolName, toolSearchToolName:
			return nil, fmt.Errorf("tool name %q is reserved for the session search router", tool.Definition.Name)
		case "read_tool_output":
			if !tool.InternalOnly {
				return nil, errors.New("read_tool_output is reserved for the internal read-file artifact route")
			}
		}
		if tool.ReplayPolicy == "" {
			tool.ReplayPolicy = ToolReplayNever
		}
		if tool.ReplayPolicy != ToolReplayNever && tool.ReplayPolicy != ToolReplaySafe {
			return nil, fmt.Errorf("tool %q has unsupported replay policy %q", tool.Definition.Name, tool.ReplayPolicy)
		}
		registry.byName[tool.Definition.Name] = tool
		if !tool.OnDemand {
			registry.active[tool.Definition.Name] = true
		}
	}
	return registry, nil
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.active))
	for name, active := range r.active {
		if active && !r.byName[name].InternalOnly {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	definitions := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, r.byName[name].Definition)
	}
	return definitions
}

// SearchScopes reports the indexes available through the single model-facing
// search tool. Internal search handlers stay in the registry but have no
// standalone model schema.
func (r *ToolRegistry) SearchScopes() []string {
	scopes := []string{"session_history"}
	if r == nil {
		return scopes
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if hasOnDemandTools(r.byName) {
		scopes = append(scopes, "tools")
	}
	for name, tool := range r.byName {
		if !tool.InternalOnly {
			continue
		}
		switch name {
		case "search_memory":
			scopes = append(scopes, "memory")
		case "search_skills":
			scopes = append(scopes, "skills")
		case "search_prior_sessions":
			scopes = append(scopes, "prior_sessions")
		}
	}
	sort.Strings(scopes)
	return scopes
}

const toolSearchToolName = "tool_search"

func hasOnDemandTools(tools map[string]Tool) bool {
	for _, tool := range tools {
		if tool.OnDemand {
			return true
		}
	}
	return false
}

// ActivateRelevantTools exposes likely relevant catalog entries before the
// first model request, so routine tasks do not pay an extra discovery turn.
func (r *ToolRegistry) ActivateRelevantTools(query string) []string {
	if r == nil {
		return nil
	}
	return r.activateSearch(query, 4)
}

func (r *ToolRegistry) ActiveToolNames() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.active))
	for name, active := range r.active {
		tool, exists := r.byName[name]
		if active && exists && tool.OnDemand {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (r *ToolRegistry) RestoreActiveToolNames(names []string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		if tool, ok := r.byName[name]; ok && tool.OnDemand {
			r.active[name] = true
		}
	}
}

func (r *ToolRegistry) isActive(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active[name]
}

func (r *ToolRegistry) activateSearch(query string, limit int) []string {
	queryTerms := toolSearchTerms(query)
	if len(queryTerms) == 0 {
		return nil
	}
	type match struct {
		name  string
		score int
	}
	var matches []match
	r.mu.RLock()
	for name, tool := range r.byName {
		if !tool.OnDemand {
			continue
		}
		text := strings.ToLower(name + " " + tool.Definition.Description)
		terms := make(map[string]bool)
		for _, term := range toolSearchTerms(text) {
			terms[term] = true
		}
		score := 0
		for _, term := range queryTerms {
			if terms[term] {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, match{name: name, score: score})
		}
	}
	r.mu.RUnlock()
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].name < matches[j].name
	})
	if limit <= 0 || limit > 8 {
		limit = 4
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	r.mu.Lock()
	activated := make([]string, 0, len(matches))
	for _, candidate := range matches {
		r.active[candidate.name] = true
		activated = append(activated, candidate.name)
	}
	r.mu.Unlock()
	return activated
}

func toolSearchTerms(value string) []string {
	value = strings.ToLower(value)
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x4e00 && r <= 0x9fff)
	})
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "from": true, "that": true, "this": true, "tool": true, "tools": true, "please": true, "use": true, "into": true, "我": true, "要": true, "帮": true, "请": true, "用": true, "的": true, "一下": true, "这个": true}
	seen := make(map[string]bool)
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if len([]rune(field)) < 2 || stop[field] || seen[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	for phrase, aliases := range map[string][]string{
		"文件": {"file", "read", "write"}, "代码": {"code", "search", "workspace"}, "搜索": {"search", "find"},
		"查找": {"search", "find"}, "读取": {"read", "file"}, "写入": {"write", "file"}, "修改": {"edit", "patch", "file"},
		"测试": {"test", "run", "command"}, "命令": {"command", "run", "shell"}, "运行": {"run", "command"},
		"网页": {"web", "search"}, "网络": {"web", "api"}, "浏览器": {"browser", "web"}, "接口": {"api", "mcp"},
		"技能": {"skill"}, "创建": {"create"},
		"仓库": {"repository", "git", "github"}, "提交": {"git", "commit"}, "分支": {"git", "branch"},
	} {
		if strings.Contains(value, phrase) {
			for _, alias := range aliases {
				if !seen[alias] {
					seen[alias] = true
					terms = append(terms, alias)
				}
			}
		}
	}
	return terms
}

func (r *ToolRegistry) searchTool() Tool {
	return Tool{ReplayPolicy: ToolReplaySafe, Definition: ToolDefinition{
		Name:        toolSearchToolName,
		Description: "Search the available tool catalog and activate relevant tools. Tools activated by this call become available in the next model turn. Use when the current tool list lacks a capability.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Capability, service, or task to search for"},"limit":{"type":"integer","minimum":1,"maximum":8}},"required":["query"],"additionalProperties":false}`),
	}, Handle: func(_ context.Context, raw json.RawMessage) (string, error) {
		var input struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
		activated := r.activateSearch(input.Query, input.Limit)
		if len(activated) == 0 {
			return "No matching tools found. Rephrase the capability or continue with the currently available tools.", nil
		}
		return "Available tools for the next model turn (matching tools are active): " + strings.Join(activated, ", "), nil
	}}
}

type ContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Text      string          `json:"text,omitempty"`
	Content   string          `json:"content,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type Message struct {
	ID      string         `json:"id,omitempty"`
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ModelResponse struct {
	Content                  []ContentBlock `json:"content"`
	StopReason               string         `json:"stop_reason"`
	InputTokens              int            `json:"input_tokens,omitempty"`
	OutputTokens             int            `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens,omitempty"`
	CacheMissInputTokens     int            `json:"cache_miss_input_tokens,omitempty"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens,omitempty"`
}

type ModelEvent struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Index int    `json:"index,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Model owns provider transport and normalization. The session owns conversation policy.
type Model interface {
	Stream(context.Context, string, []Message, []ToolDefinition, func(ModelEvent)) (ModelResponse, error)
	ModelID() string
}

type Event struct {
	At                               time.Time       `json:"at"`
	Type                             string          `json:"type"`
	TraceID                          string          `json:"trace_id,omitempty"`
	SpanID                           string          `json:"span_id,omitempty"`
	ParentSpanID                     string          `json:"parent_span_id,omitempty"`
	TaskID                           string          `json:"task_id,omitempty"`
	RunID                            string          `json:"run_id,omitempty"`
	WorkerID                         string          `json:"worker_id,omitempty"`
	Session                          string          `json:"session_id,omitempty"`
	Turn                             int             `json:"turn,omitempty"`
	Tool                             string          `json:"tool,omitempty"`
	ToolCallID                       string          `json:"tool_call_id,omitempty"`
	Duration                         int64           `json:"duration_ms,omitempty"`
	Bytes                            int             `json:"bytes,omitempty"`
	MessagesDropped                  int             `json:"messages_dropped,omitempty"`
	EstimatedTokensBefore            int             `json:"estimated_tokens_before,omitempty"`
	EstimatedTokensAfter             int             `json:"estimated_tokens_after,omitempty"`
	InputTokens                      int             `json:"input_tokens,omitempty"`
	OutputTokens                     int             `json:"output_tokens,omitempty"`
	CacheReadInputTokens             int             `json:"cache_read_input_tokens,omitempty"`
	CacheMissInputTokens             int             `json:"cache_miss_input_tokens,omitempty"`
	CacheCreationInputTokens         int             `json:"cache_creation_input_tokens,omitempty"`
	SummaryTokens                    int             `json:"summary_tokens,omitempty"`
	ContextCompactionThresholdTokens int             `json:"context_compaction_threshold_tokens,omitempty"`
	ContextKeepRecentTokens          int             `json:"context_keep_recent_tokens,omitempty"`
	ContextSummaryTokens             int             `json:"context_summary_tokens,omitempty"`
	CompactionSummary                string          `json:"compaction_summary,omitempty"`
	Error                            string          `json:"error,omitempty"`
	Text                             string          `json:"text,omitempty"`
	Args                             json.RawMessage `json:"args,omitempty"`
	ResultSummary                    string          `json:"result_summary,omitempty"`
	ResultOutput                     string          `json:"result_output,omitempty"`
	Status                           string          `json:"status,omitempty"`
	AuthorizationID                  string          `json:"authorization_id,omitempty"`
	RiskClass                        string          `json:"risk_class,omitempty"`
	Decision                         string          `json:"decision,omitempty"`
	ApprovedBy                       string          `json:"approved_by,omitempty"`
	Target                           string          `json:"target,omitempty"`
	RiskReason                       string          `json:"risk_reason,omitempty"`
}

type EventSink func(Event)
type ToolPolicy func(context.Context, Tool, json.RawMessage) error
type ToolObserver func(context.Context, Tool, json.RawMessage, string, error)

type Limits struct {
	MaxTurns                  int
	MaxToolCalls              int
	MaxRepeatedFailedCommands int
	MaxToolResultBytes        int
	ToolTimeout               time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxTurns: 128, MaxToolCalls: 256, MaxToolResultBytes: 50 * 1024, ToolTimeout: 60 * time.Second}
}

type Result struct {
	Text                          string
	VerificationStatus            string
	Turns                         int
	ToolCalls                     int
	InputTokens                   int
	OutputTokens                  int
	CacheReadInputTokens          int
	CacheMissInputTokens          int
	CacheCreationInputTokens      int
	ContextCompactions            int
	ContextCompactionInputTokens  int
	ContextCompactionOutputTokens int
}

type Session struct {
	ID                 string
	TaskID             string
	RunID              string
	WorkerID           string
	TraceContext       tracecontext.Context
	Name               string
	System             string
	Model              Model
	Tools              *ToolRegistry
	Limits             Limits
	RequiredSkills     []string
	OnEvent            EventSink
	BeforeTool         ToolPolicy
	AfterTool          ToolObserver
	SerializeToolCalls bool
	Store              SessionStore
	Context            ContextPreparer
	ToolOutputs        ToolOutputStore

	mu                 sync.Mutex
	messages           []Message
	compactionSummary  string
	compactionEvidence string
	compactionBoundary int
	entries            []SessionEntry
	head               string
	modelID            string
	loadedSkills       map[string]bool
	steering           []Message
	followUps          []Message
	pendingTools       []ContentBlock
	modelDraft         string
	modelDraftTurn     int
	toolOperations     []ToolOperation
	running            bool
	cancel             context.CancelFunc
}

func (s *Session) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMessages(s.messages)
}

// SetContextWindow applies new request-window settings between user turns.
// Callers must not change the window while Prompt or Continue is running.
func (s *Session) SetContextWindow(window WindowContextManager) {
	s.Context = window
	s.emit(Event{
		Type: "context_settings_changed", ContextCompactionThresholdTokens: window.CompactionThresholdTokens,
		ContextKeepRecentTokens: window.KeepRecentTokens, ContextSummaryTokens: window.SummaryTokens,
	})
}

// Steer injects a message at the next safe turn boundary. It does not interrupt a provider stream.
func (s *Session) Steer(text string) error {
	return s.SteerWithID("", text)
}

// SteerWithID queues a steering message once. The stable ID makes delivery
// from the task API's durable inbox safe to retry after a process restart.
func (s *Session) SteerWithID(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" && s.hasMessageIDLocked(id) {
		return nil
	}
	s.steering = append(s.steering, userMessageWithID(id, text))
	if err := s.persistLocked(); err != nil {
		s.steering = s.steering[:len(s.steering)-1]
		return err
	}
	return nil
}

// FollowUp queues a message to run only after the current request would otherwise settle.
func (s *Session) FollowUp(text string) error {
	return s.FollowUpWithID("", text)
}

// FollowUpWithID queues a follow-up message once, even if inbox delivery is retried.
func (s *Session) FollowUpWithID(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" && s.hasMessageIDLocked(id) {
		return nil
	}
	s.followUps = append(s.followUps, userMessageWithID(id, text))
	if err := s.persistLocked(); err != nil {
		s.followUps = s.followUps[:len(s.followUps)-1]
		return err
	}
	return nil
}

func (s *Session) Abort() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Session) Prompt(ctx context.Context, text string) (Result, error) {
	return s.start(ctx, userMessage(text), false)
}

func (s *Session) Continue(ctx context.Context) (Result, error) {
	return s.start(ctx, Message{}, true)
}

func (s *Session) start(parent context.Context, prompt Message, continuation bool) (result Result, runErr error) {
	if s.Model == nil || s.Tools == nil {
		return Result{}, errors.New("session requires a model and tool registry")
	}
	if !continuation {
		var initialTask strings.Builder
		for _, block := range prompt.Content {
			if block.Type == "text" {
				initialTask.WriteString(block.Text)
				initialTask.WriteByte('\n')
			}
		}
		s.Tools.ActivateRelevantTools(initialTask.String())
	}
	limits := s.Limits
	if limits.MaxTurns <= 0 {
		limits = DefaultLimits()
	}
	if limits.MaxToolCalls <= 0 {
		limits.MaxToolCalls = DefaultLimits().MaxToolCalls
	}
	if limits.MaxRepeatedFailedCommands < 0 {
		limits.MaxRepeatedFailedCommands = 0
	}
	if limits.MaxToolResultBytes <= 0 {
		limits.MaxToolResultBytes = DefaultLimits().MaxToolResultBytes
	}
	if limits.ToolTimeout <= 0 {
		limits.ToolTimeout = DefaultLimits().ToolTimeout
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		cancel()
		return Result{}, errors.New("session is already running")
	}
	if continuation && s.modelDraft != "" {
		previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
		previousDraft, previousDraftTurn := s.modelDraft, s.modelDraftTurn
		s.appendMessagesLocked([]Message{{Role: "assistant", Content: []ContentBlock{{Type: "text", Text: s.modelDraft}}}})
		s.appendMessagesLocked([]Message{userMessage("The previous model stream stopped before its response completed. Continue from the saved draft above, preserving its content and avoiding repetition. Provider-level token-position resume is unavailable for this checkpoint.")})
		s.modelDraft, s.modelDraftTurn = "", 0
		if err := s.persistLocked(); err != nil {
			s.messages = s.messages[:previousMessages]
			s.entries = s.entries[:previousEntries]
			s.head = previousHead
			s.modelDraft, s.modelDraftTurn = previousDraft, previousDraftTurn
			s.mu.Unlock()
			cancel()
			return Result{}, fmt.Errorf("prepare model draft continuation: %w", err)
		}
	}
	if continuation && len(s.messages) > 0 && s.messages[len(s.messages)-1].Role == "assistant" {
		if len(s.pendingTools) > 0 {
			// A tool-result message must close the saved assistant tool call before
			// queued user instructions can be appended to the transcript.
		} else if len(s.steering) > 0 {
			if _, err := s.consumeQueueLocked(&s.steering); err != nil {
				s.mu.Unlock()
				cancel()
				return Result{}, err
			}
		} else if len(s.followUps) > 0 {
			if _, err := s.consumeQueueLocked(&s.followUps); err != nil {
				s.mu.Unlock()
				cancel()
				return Result{}, err
			}
		}
	}
	if continuation {
		pendingToolExchange := len(s.pendingTools) > 0 && len(s.messages) > 0 && s.messages[len(s.messages)-1].Role == "assistant"
		if !pendingToolExchange && (len(s.messages) == 0 || s.messages[len(s.messages)-1].Role == "assistant") {
			s.mu.Unlock()
			cancel()
			return Result{}, errors.New("cannot continue: transcript must end with a user or tool result message")
		}
	} else {
		previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
		s.appendMessagesLocked([]Message{prompt})
		if err := s.persistLocked(); err != nil {
			s.messages = s.messages[:previousMessages]
			s.entries = s.entries[:previousEntries]
			s.head = previousHead
			s.mu.Unlock()
			cancel()
			return Result{}, fmt.Errorf("persist prompt: %w", err)
		}
	}
	s.running = true
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		terminal := Event{Type: "agent_end"}
		if runErr != nil {
			terminal.Error = runErr.Error()
		}
		s.emit(terminal)
		s.mu.Lock()
		s.cancel = nil
		s.running = false
		s.mu.Unlock()
	}()

	s.emit(Event{Type: "agent_start"})
	if continuation {
		replayed, err := s.recoverPendingTools(ctx, limits)
		result.ToolCalls += replayed
		if err != nil {
			return result, err
		}
	}
	if _, err := s.consumeSteering(); err != nil {
		return result, err
	}
	workflow := newWorkflowPolicy(limits.MaxRepeatedFailedCommands)
	outputContinuationRetries := 0
	skillBootstrapRetries := 0
	emptyStopReasonRetries := 0
	const maxOutputContinuationRetries = 2
	const maxSkillBootstrapRetries = 2
	const maxEmptyStopReasonRetries = 2
	for turn := 1; turn <= limits.MaxTurns; turn++ {
		s.updateToolTokenBudget()
		if err := ctx.Err(); err != nil {
			s.emit(Event{Type: "agent_aborted", Turn: turn, Error: err.Error()})
			return result, err
		}
		result.Turns = turn
		s.emit(Event{Type: "turn_start", Turn: turn})
		modelTrace, traceErr := tracecontext.Child(s.TraceContext)
		if traceErr != nil {
			modelTrace = s.TraceContext
		}
		modelStarted := time.Now()
		emitModelEvent := func(event Event) {
			event.TraceID = modelTrace.TraceID
			event.SpanID = modelTrace.SpanID
			event.ParentSpanID = modelTrace.ParentSpanID
			s.emit(event)
		}
		emitModelEvent(Event{Type: "model_request", Turn: turn})
		if window, ok := s.Context.(WindowContextManager); ok {
			compaction, estimatedBefore, compactErr := s.compactAutomatically(ctx, turn, window)
			if compactErr != nil {
				s.emit(Event{Type: "context_compaction_model_failed", Turn: turn, Tool: "automatic_context_compaction", Error: compactErr.Error()})
			} else if compaction != nil {
				result.ContextCompactions++
				result.InputTokens += compaction.InputTokens
				result.OutputTokens += compaction.OutputTokens
				result.CacheReadInputTokens += compaction.CacheReadInputTokens
				result.CacheMissInputTokens += compaction.CacheMissInputTokens
				result.CacheCreationInputTokens += compaction.CacheCreationInputTokens
				result.ContextCompactionInputTokens += compaction.InputTokens
				result.ContextCompactionOutputTokens += compaction.OutputTokens
				visibleSystem, visibleMessages := s.modelContext(s.System)
				s.emit(Event{Type: "context_compacted_model", Turn: turn, Tool: "automatic_context_compaction", MessagesDropped: compaction.MessagesCompacted, EstimatedTokensBefore: estimatedBefore, EstimatedTokensAfter: estimateTextTokens(visibleSystem) + estimateMessageTokens(visibleMessages), InputTokens: compaction.InputTokens, OutputTokens: compaction.OutputTokens, CacheReadInputTokens: compaction.CacheReadInputTokens, CacheMissInputTokens: compaction.CacheMissInputTokens, CacheCreationInputTokens: compaction.CacheCreationInputTokens, SummaryTokens: compaction.SummaryTokens, CompactionSummary: truncateToolOutput(compaction.Summary, 2048)})
			}
		}
		systemPrompt := s.System
		systemPrompt, requestMessages := s.modelContext(systemPrompt)
		if s.Context != nil {
			prepared, prepareErr := s.Context.Prepare(systemPrompt, requestMessages)
			if prepareErr != nil {
				return result, fmt.Errorf("prepare model context: %w", prepareErr)
			}
			systemPrompt, requestMessages = prepared.System, prepared.Messages
			if prepared.Compacted {
				result.ContextCompactions++
				s.emit(Event{Type: "context_compacted", Turn: turn, MessagesDropped: prepared.DroppedMessages, EstimatedTokensBefore: prepared.EstimatedBefore, EstimatedTokensAfter: prepared.EstimatedAfter})
			}
		}
		toolDefinitions := s.Tools.Definitions()
		toolDefinitions = requiredSkillBootstrapTools(toolDefinitions, s.pendingRequiredSkills())
		// Keep the transcript-recovery tool in a stable position in every request
		// so compaction does not change the tool schema prefix seen by providers.
		toolDefinitions = WithSessionSearchTool(toolDefinitions, s.Tools.SearchScopes())
		var partialText string
		var lastDraftCheckpoint time.Time
		var draftCheckpointErr error
		response, err := s.Model.Stream(ctx, systemPrompt, requestMessages, toolDefinitions, func(modelEvent ModelEvent) {
			emitModelEvent(Event{Type: "model_" + modelEvent.Type, Turn: turn, Text: modelEvent.Text, Tool: modelEvent.Name})
			if modelEvent.Type == "text_delta" && modelEvent.Text != "" {
				partialText += modelEvent.Text
				if time.Since(lastDraftCheckpoint) >= 200*time.Millisecond || lastDraftCheckpoint.IsZero() {
					if checkpointErr := s.setModelDraft(partialText, turn); checkpointErr != nil && draftCheckpointErr == nil {
						draftCheckpointErr = checkpointErr
					}
					lastDraftCheckpoint = time.Now()
				}
			}
		})
		if partialText != "" {
			if checkpointErr := s.setModelDraft(partialText, turn); checkpointErr != nil && draftCheckpointErr == nil {
				draftCheckpointErr = checkpointErr
			}
		}
		if draftCheckpointErr != nil {
			return result, fmt.Errorf("persist interrupted model draft: %w", draftCheckpointErr)
		}
		if err != nil {
			emitModelEvent(Event{Type: "model_error", Turn: turn, Duration: time.Since(modelStarted).Milliseconds(), Error: err.Error()})
			return result, err
		}
		normalizedContent, invalidToolInputs := normalizeToolCallInputs(response.Content)
		response.Content = normalizedContent
		result.InputTokens += response.InputTokens
		result.OutputTokens += response.OutputTokens
		result.CacheReadInputTokens += response.CacheReadInputTokens
		result.CacheMissInputTokens += response.CacheMissInputTokens
		result.CacheCreationInputTokens += response.CacheCreationInputTokens
		if !shouldRecoverMissingStopReason(response.StopReason) {
			emptyStopReasonRetries = 0
		}
		emitModelEvent(Event{Type: "model_usage", Turn: turn, Duration: time.Since(modelStarted).Milliseconds(), InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, CacheReadInputTokens: response.CacheReadInputTokens, CacheMissInputTokens: response.CacheMissInputTokens, CacheCreationInputTokens: response.CacheCreationInputTokens})
		result.Text += textBlocks(response.Content)
		toolCalls := make([]ContentBlock, 0)
		for _, block := range response.Content {
			if block.Type == "tool_use" {
				toolCalls = append(toolCalls, block)
			}
		}
		if response.StopReason == "max_tokens" || response.StopReason == "length" {
			if len(toolCalls) > 0 {
				if err := s.appendRecoveryPrompt(truncatedToolCallRecoveryPrompt()); err != nil {
					return result, err
				}
			} else {
				if err := s.appendAssistant(response.Content, nil); err != nil {
					return result, err
				}
				if err := s.appendRecoveryPrompt(truncatedResponseRecoveryPrompt()); err != nil {
					return result, err
				}
			}
			s.emit(Event{Type: "turn_end", Turn: turn})
			if outputContinuationRetries >= maxOutputContinuationRetries {
				return result, fmt.Errorf("model response remained truncated after %d continuation attempts", maxOutputContinuationRetries)
			}
			outputContinuationRetries++
			continue
		}
		outputContinuationRetries = 0
		if len(toolCalls) > 0 {
			if result.ToolCalls+len(toolCalls) > limits.MaxToolCalls {
				if err := s.appendAssistant(response.Content, toolCalls); err != nil {
					return result, err
				}
				results := make([]ContentBlock, 0, len(toolCalls))
				for _, call := range toolCalls {
					results = append(results, toolResult(call.ID, "Tool call limit reached for this run; continue the saved session to proceed.", true))
				}
				if err := s.appendToolResults(results); err != nil {
					return result, err
				}
				return result, fmt.Errorf("tool call limit (%d) reached", limits.MaxToolCalls)
			}
			if err := s.appendAssistant(response.Content, toolCalls); err != nil {
				return result, err
			}
			result.ToolCalls += len(toolCalls)
			compactRequested := false
			for _, call := range toolCalls {
				compactRequested = compactRequested || isCompactContextTool(call.Name)
			}
			var toolResults []ContentBlock
			var toolErr error
			if compactRequested && len(toolCalls) > 1 {
				toolResults = make([]ContentBlock, len(toolCalls))
				for i, call := range toolCalls {
					message := "compact_context must be called by itself; no tools in this batch were executed"
					if rejected := invalidToolInputs[call.ID]; rejected != "" {
						message = rejected
					}
					s.emit(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Args: eventToolArgs(call.Input), Status: "running"})
					s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultSummary: message, ResultOutput: message, Status: "failed"})
					toolResults[i] = toolResult(call.ID, message, true)
				}
			} else {
				toolResults, toolErr = s.executeTools(ctx, turn, toolCalls, invalidToolInputs, limits.ToolTimeout, limits.MaxToolResultBytes)
			}
			compactIndex, compactCount := -1, 0
			for i, call := range toolCalls {
				if isCompactContextTool(call.Name) {
					compactIndex = i
					compactCount++
				}
			}
			var compaction *contextCompaction
			if compactCount > 0 && compactIndex < len(toolResults) {
				switch {
				case compactCount > 1 || len(toolCalls) > 1:
					for i, call := range toolCalls {
						if isCompactContextTool(call.Name) {
							toolResults[i] = toolResult(call.ID, "Call compact_context by itself in a separate turn so its summary includes all completed tool results.", true)
						}
					}
				case toolResults[compactIndex].IsError:
					// The normal tool policy or validation error is already recorded.
				default:
					compacted, compactErr := s.summarizeContext(ctx, turn, toolCalls[compactIndex].Input)
					if compactErr != nil {
						s.emit(Event{Type: "context_compaction_model_failed", Turn: turn, Tool: compactContextToolName, Error: compactErr.Error()})
						toolResults[compactIndex] = toolResult(toolCalls[compactIndex].ID, "Context compaction failed: "+compactErr.Error(), true)
					} else {
						compaction = &compacted
						result.InputTokens += compacted.InputTokens
						result.OutputTokens += compacted.OutputTokens
						result.CacheReadInputTokens += compacted.CacheReadInputTokens
						result.CacheMissInputTokens += compacted.CacheMissInputTokens
						result.CacheCreationInputTokens += compacted.CacheCreationInputTokens
						result.ContextCompactionInputTokens += compacted.InputTokens
						result.ContextCompactionOutputTokens += compacted.OutputTokens
						toolResults[compactIndex] = toolResult(toolCalls[compactIndex].ID, fmt.Sprintf("Context summary created (%d estimated tokens). The previous session history is archived in the session record.", compacted.SummaryTokens), false)
					}
				}
			}
			stalledReason := ""
			for index, call := range toolCalls {
				if index >= len(toolResults) {
					continue
				}
				feedback, stalled := workflow.observe(call, toolResults[index], s.Tools)
				if feedback != "" {
					toolResults[index] = toolResult(call.ID, toolResults[index].Content+"\n\n"+feedback, toolResults[index].IsError)
				}
				if stalled != "" {
					stalledReason = stalled
				}
			}
			if len(toolResults) > 0 {
				var appendErr error
				if compaction != nil {
					appendErr = s.appendToolResultsAndCompact(toolResults, compaction.Summary, compaction.Evidence)
				} else {
					appendErr = s.appendToolResults(toolResults)
				}
				if appendErr != nil {
					return result, appendErr
				}
				if compaction != nil {
					result.ContextCompactions++
					_, visible := s.modelContext(s.System)
					s.emit(Event{Type: "context_compacted_model", Turn: turn, Tool: compactContextToolName, MessagesDropped: compaction.MessagesCompacted, EstimatedTokensBefore: compaction.EstimatedTokensBefore, EstimatedTokensAfter: estimateTextTokens(s.System) + estimateMessageTokens(visible), InputTokens: compaction.InputTokens, OutputTokens: compaction.OutputTokens, CacheReadInputTokens: compaction.CacheReadInputTokens, CacheMissInputTokens: compaction.CacheMissInputTokens, CacheCreationInputTokens: compaction.CacheCreationInputTokens, SummaryTokens: compaction.SummaryTokens, CompactionSummary: truncateToolOutput(compaction.Summary, 2048)})
				}
			}
			if checkpoint := workflow.checkpoint(turn, limits.MaxTurns); checkpoint != "" {
				if err := s.appendRecoveryPrompt(checkpoint); err != nil {
					return result, err
				}
			}
			s.emit(Event{Type: "turn_end", Turn: turn})
			if stalledReason != "" {
				s.emit(Event{Type: "agent_stalled", Turn: turn, Error: stalledReason})
				return result, errors.New(stalledReason)
			}
			if toolErr != nil {
				return result, toolErr
			}
			if _, err := s.consumeSteering(); err != nil {
				return result, err
			}
			continue
		}
		if pendingSkills := s.pendingRequiredSkills(); len(pendingSkills) > 0 {
			if err := s.appendAssistant(response.Content, nil); err != nil {
				return result, err
			}
			if skillBootstrapRetries >= maxSkillBootstrapRetries {
				return result, fmt.Errorf("required repository Skill(s) were not loaded: %s", strings.Join(pendingSkills, ", "))
			}
			if err := s.appendRecoveryPrompt(requiredSkillsRecoveryPrompt()); err != nil {
				return result, err
			}
			skillBootstrapRetries++
			s.emit(Event{Type: "turn_end", Turn: turn})
			continue
		}
		if shouldRecoverMissingStopReason(response.StopReason) {
			emptyStopReasonRetries++
			if emptyStopReasonRetries > maxEmptyStopReasonRetries {
				return result, fmt.Errorf("model stream completed without a finish reason %d consecutive times", emptyStopReasonRetries)
			}
			if err := s.appendAssistant(response.Content, nil); err != nil {
				return result, err
			}
			if err := s.appendRecoveryPrompt(missingStopReasonRecoveryPrompt()); err != nil {
				return result, err
			}
			s.emit(Event{Type: "model_response_recovered", Turn: turn, Error: fmt.Sprintf("empty finish reason; retry %d of %d", emptyStopReasonRetries, maxEmptyStopReasonRetries)})
			s.emit(Event{Type: "turn_end", Turn: turn})
			continue
		}
		if err := s.appendAssistant(response.Content, nil); err != nil {
			return result, err
		}
		s.emit(Event{Type: "turn_end", Turn: turn})
		if response.StopReason != "end_turn" && response.StopReason != "stop" {
			return result, fmt.Errorf("model stopped with unsupported reason %q", response.StopReason)
		}
		hasSteering, err := s.consumeSteering()
		if err != nil {
			return result, err
		}
		hasFollowUps := false
		if !hasSteering {
			hasFollowUps, err = s.consumeFollowUps()
			if err != nil {
				return result, err
			}
		}
		if !hasSteering && !hasFollowUps {
			result.VerificationStatus = workflow.verificationStatus()
			return result, nil
		}
	}
	result.VerificationStatus = workflow.verificationStatus()
	s.emit(Event{Type: "turn_limit_reached", Error: fmt.Sprintf("agent turn limit (%d) reached", limits.MaxTurns)})
	if err := s.appendRecoveryPrompt(workflow.continuationAtLimit()); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Session) updateToolTokenBudget() {
	if s == nil || s.Context == nil || s.Tools == nil {
		return
	}
	tokens := EstimateToolDefinitionTokens(WithSessionSearchTool(s.Tools.Definitions(), s.Tools.SearchScopes()))
	switch context := s.Context.(type) {
	case WindowContextManager:
		context.ToolTokens = tokens
		s.Context = context
	case *WindowContextManager:
		if context != nil {
			context.ToolTokens = tokens
		}
	}
}

func (s *Session) resolveToolCall(name string, input json.RawMessage) (Tool, bool) {
	switch name {
	case sessionSearchToolName:
		tool := s.sessionSearchTool()
		tool.Definition.Name = sessionSearchPolicyToolName(input)
		return tool, true
	case sessionHistorySearchToolName:
		return s.sessionHistorySearchTool(), true
	case toolSearchToolName:
		if s.Tools != nil {
			return s.Tools.searchTool(), true
		}
	}
	if s.Tools == nil {
		return Tool{}, false
	}
	tool, ok := s.Tools.byName[name]
	if ok && name == "read_file" && hasToolOutputArtifact(input) && s.ToolOutputs != nil {
		inlineLimit := s.Limits.MaxToolResultBytes
		if inlineLimit <= 0 {
			inlineLimit = DefaultLimits().MaxToolResultBytes
		}
		tool.Handle = ReadToolOutputTool(s.ToolOutputs, inlineLimit).Handle
	}
	return tool, ok
}

func isSearchRouterCall(name string) bool {
	return name == sessionSearchToolName || name == sessionHistorySearchToolName || name == toolSearchToolName
}

func hasToolOutputArtifact(input json.RawMessage) bool {
	var args struct {
		ArtifactID string `json:"artifact_id"`
	}
	return json.Unmarshal(input, &args) == nil && strings.TrimSpace(args.ArtifactID) != ""
}

func currentVerificationStatus(lastWriteOrder, lastTestOrder, lastDiffOrder int, lastTestPassed bool) string {
	if lastWriteOrder == 0 {
		return "no_changes"
	}
	if lastDiffOrder > lastWriteOrder && lastTestOrder <= lastWriteOrder {
		return "diff_reviewed"
	}
	if lastTestOrder > lastWriteOrder && lastDiffOrder > lastWriteOrder {
		if lastTestPassed {
			return "verified"
		}
		return "test_failed"
	}
	return "incomplete"
}

func (s *Session) hasMessageIDLocked(id string) bool {
	contains := func(messages []Message) bool {
		for _, message := range messages {
			if message.ID == id {
				return true
			}
		}
		return false
	}
	return contains(s.messages) || contains(s.steering) || contains(s.followUps)
}

func normalizeToolCallInputs(content []ContentBlock) ([]ContentBlock, map[string]string) {
	normalized := append([]ContentBlock(nil), content...)
	var rejected map[string]string
	for i := range normalized {
		if normalized[i].Type != "tool_use" {
			continue
		}
		input := strings.TrimSpace(string(normalized[i].Input))
		if input == "" {
			normalized[i].Input = json.RawMessage(`{}`)
			continue
		}
		if json.Valid([]byte(input)) {
			normalized[i].Input = json.RawMessage(input)
			continue
		}
		normalized[i].Input = json.RawMessage(`{}`)
		if rejected == nil {
			rejected = make(map[string]string)
		}
		rejected[normalized[i].ID] = "tool arguments are malformed JSON; the tool was not executed. Retry this tool call with a valid JSON object matching its input schema."
	}
	return normalized, rejected
}

func (s *Session) executeTools(ctx context.Context, turn int, calls []ContentBlock, rejectedInputs map[string]string, timeout time.Duration, maxResultBytes int) ([]ContentBlock, error) {
	parallel := len(calls) > 1 && !s.SerializeToolCalls
	if len(s.pendingRequiredSkills()) > 0 {
		parallel = false
	}
	mode := ""
	for _, call := range calls {
		tool, ok := s.Tools.byName[call.Name]
		if !ok || (tool.ExecutionMode != "parallel" && tool.ExecutionMode != "parallel_write") {
			parallel = false
			break
		}
		if mode == "" {
			mode = tool.ExecutionMode
		} else if mode != tool.ExecutionMode {
			// Keep mixed read/write batches ordered. Pure reads and pure
			// per-file writes each have their own safe concurrency policy.
			parallel = false
			break
		}
	}
	results := make([]ContentBlock, len(calls))
	execute := func(call ContentBlock) ContentBlock {
		if message := rejectedInputs[call.ID]; message != "" {
			started := time.Now()
			s.emit(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Args: eventToolArgs(call.Input), Status: "running"})
			s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Error: message, ResultOutput: message, ResultSummary: message, Status: "failed"})
			return toolResult(call.ID, message, true)
		}
		return s.executeTool(ctx, turn, call, timeout, maxResultBytes)
	}
	if parallel {
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Add(1)
			go func(i int, call ContentBlock) {
				defer wg.Done()
				results[i] = execute(call)
			}(i, call)
		}
		wg.Wait()
	} else {
		for i, call := range calls {
			results[i] = execute(call)
			if ctx.Err() != nil {
				break
			}
		}
	}
	for i, call := range calls {
		if results[i].Type == "" {
			results[i] = toolResult(call.ID, "Tool call cancelled before execution.", true)
		}
	}
	return results, nil
}

func commandFingerprint(call ContentBlock) string {
	var canonical any
	if err := json.Unmarshal(call.Input, &canonical); err == nil {
		if encoded, err := json.Marshal(canonical); err == nil {
			return call.Name + ":" + string(encoded)
		}
	}
	return call.Name + ":" + string(call.Input)
}

func repeatedFailedCommandFingerprint(call, result ContentBlock) (string, bool) {
	if result.IsError {
		return "", false
	}
	switch call.Name {
	case "run_command":
		var input struct {
			Command          string `json:"command"`
			Purpose          string `json:"purpose"`
			Action           string `json:"action"`
			WorkingDirectory string `json:"working_directory"`
			Image            string `json:"image"`
		}
		if json.Unmarshal(call.Input, &input) != nil || strings.TrimSpace(input.Command) == "" {
			return "", false
		}
		if input.Action == "" {
			input.Action = "run"
		}
		if input.Action != "run" {
			return "", false
		}
		status := resultStatusLine(result.Content, "SIGMA_RUN_COMMAND ")
		if !strings.Contains(status, "status=failed") || strings.Contains(status, "workspace_changed=true") {
			return "", false
		}
		return commandFingerprint(call), true
	default:
		return "", false
	}
}

func successfulTestCommand(call, result ContentBlock) bool {
	if result.IsError {
		return false
	}
	switch call.Name {
	case "run_command":
		var input struct {
			Purpose string `json:"purpose"`
		}
		if json.Unmarshal(call.Input, &input) != nil || input.Purpose != "test" {
			return false
		}
		status := resultStatusLine(result.Content, "SIGMA_RUN_COMMAND ")
		return strings.Contains(status, "status=passed") && strings.Contains(status, "exit_code=0") && !strings.Contains(status, "workspace_changed=true")
	default:
		return false
	}
}

func resultStatusLine(content, marker string) string {
	index := strings.Index(content, marker)
	if index < 0 {
		return ""
	}
	line := strings.SplitN(content[index+len(marker):], "\n", 2)[0]
	return marker + line
}

func toolCallChangedWorkspaceOrEnvironment(call, result ContentBlock, registry *ToolRegistry) bool {
	if result.IsError {
		return false
	}
	switch call.Name {
	case "create_file", "edit_file", "prepare_environment":
		return true
	case "create_skill":
		var input struct {
			Scope string `json:"scope"`
		}
		_ = json.Unmarshal(call.Input, &input)
		return input.Scope != "user"
	}
	if tool, ok := registry.byName[call.Name]; ok && tool.ModifiesWorkspace {
		return true
	}
	if call.Name != "run_command" {
		return false
	}
	status := resultStatusLine(result.Content, "SIGMA_RUN_COMMAND ")
	return strings.Contains(status, "workspace_changed=true")
}

func toolIdempotencyKey(sessionID string, turn int, call ContentBlock) string {
	canonicalInput := call.Input
	var value any
	if json.Unmarshal(call.Input, &value) == nil {
		if encoded, err := json.Marshal(value); err == nil {
			canonicalInput = encoded
		}
	}
	identity := fmt.Sprintf("sigma-tool-v1\x00%s\x00%d\x00%s\x00%s\x00%s", sessionID, turn, call.ID, call.Name, canonicalInput)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func (s *Session) beginToolOperation(turn int, call ContentBlock, key string, replayPolicy ToolReplayPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := cloneToolOperations(s.toolOperations)
	operation := ToolOperation{ToolCallID: call.ID, Turn: turn, Tool: call.Name, IdempotencyKey: key, ReplayPolicy: replayPolicy, Status: "running", Args: append(json.RawMessage(nil), call.Input...), UpdatedAt: time.Now().UTC()}
	index := s.toolOperationIndexLocked(call.ID, turn)
	if index >= 0 {
		s.toolOperations[index] = operation
	} else {
		s.toolOperations = append(s.toolOperations, operation)
	}
	if err := s.persistLocked(); err != nil {
		s.toolOperations = previous
		return err
	}
	return nil
}

func (s *Session) finishToolOperation(callID string, turn int, status, output, operationError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.toolOperationIndexLocked(callID, turn)
	if index < 0 {
		return errors.New("tool operation intent is missing")
	}
	previous := s.toolOperations[index]
	s.toolOperations[index].Status = status
	s.toolOperations[index].Output = output
	s.toolOperations[index].Error = operationError
	s.toolOperations[index].UpdatedAt = time.Now().UTC()
	if err := s.persistLocked(); err != nil {
		s.toolOperations[index] = previous
		return err
	}
	return nil
}

func (s *Session) toolOperationIndexLocked(callID string, turn int) int {
	for i := range s.toolOperations {
		if s.toolOperations[i].ToolCallID == callID && s.toolOperations[i].Turn == turn {
			return i
		}
	}
	return -1
}

func (s *Session) existingToolOperation(callID string) (ToolOperation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.toolOperations) - 1; i >= 0; i-- {
		if s.toolOperations[i].ToolCallID == callID {
			operation := s.toolOperations[i]
			operation.Args = append(json.RawMessage(nil), operation.Args...)
			return operation, true
		}
	}
	return ToolOperation{}, false
}

func (s *Session) updateToolOperation(callID string, turn int, status, output, operationError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.toolOperationIndexLocked(callID, turn)
	if index < 0 {
		return errors.New("tool operation intent is missing")
	}
	previous := s.toolOperations[index]
	s.toolOperations[index].Status = status
	s.toolOperations[index].Output = output
	s.toolOperations[index].Error = operationError
	s.toolOperations[index].UpdatedAt = time.Now().UTC()
	if err := s.persistLocked(); err != nil {
		s.toolOperations[index] = previous
		return err
	}
	return nil
}

// recoverPendingTools closes the saved assistant tool exchange before a resumed
// model request. Completed results are reused; interrupted calls are only replayed
// when both the persisted intent and current tool registration explicitly allow it.
func (s *Session) recoverPendingTools(ctx context.Context, limits Limits) (int, error) {
	s.mu.Lock()
	calls := append([]ContentBlock(nil), s.pendingTools...)
	s.mu.Unlock()
	if len(calls) == 0 {
		return 0, nil
	}

	results := make([]ContentBlock, 0, len(calls))
	replayed := 0
	for _, call := range calls {
		operation, exists := s.existingToolOperation(call.ID)
		if !exists {
			results = append(results, toolResult(call.ID, "Previous process stopped before a durable operation record was written. Its side-effect status is unknown; inspect the target before deciding whether to retry.", true))
			continue
		}
		switch operation.Status {
		case "completed":
			results = append(results, toolResult(call.ID, operation.Output, false))
			continue
		case "failed":
			results = append(results, toolResult(call.ID, operation.Output, true))
			continue
		}

		tool, registered := s.resolveToolCall(operation.Tool, operation.Args)
		if !registered {
			results = append(results, toolResult(call.ID, "Tool is no longer registered; previous execution outcome is unknown. Inspect the target before retrying.", true))
			continue
		}
		canReplay := operation.ReplayPolicy == ToolReplaySafe && tool.ReplayPolicy == ToolReplaySafe

		if tool.Recover != nil {
			recoveryCtx, cancel := context.WithTimeout(ctx, limits.ToolTimeout)
			recoveryCtx = context.WithValue(recoveryCtx, toolTurnContextKey{}, operation.Turn)
			recoveryCtx = context.WithValue(recoveryCtx, toolCallIDContextKey{}, operation.ToolCallID)
			recoveryCtx = context.WithValue(recoveryCtx, toolIdempotencyKeyContextKey{}, operation.IdempotencyKey)
			s.emit(Event{Type: "tool_recovery_start", Turn: operation.Turn, Tool: operation.Tool, ToolCallID: operation.ToolCallID, Status: "running"})
			var recoveryErr error
			if s.BeforeTool != nil {
				recoveryErr = s.BeforeTool(recoveryCtx, tool, operation.Args)
			}
			var recovered ToolRecoveryResult
			if recoveryErr == nil {
				recovered, recoveryErr = tool.Recover(recoveryCtx, operation.Args, operation.IdempotencyKey)
			}
			cancel()
			recoveryEvent := Event{Type: "tool_recovery_end", Turn: operation.Turn, Tool: operation.Tool, ToolCallID: operation.ToolCallID, Status: "unknown"}
			if recoveryErr != nil {
				recoveryEvent.Error = recoveryErr.Error()
			} else {
				recoveryEvent.Status = recovered.Status
			}
			s.emit(recoveryEvent)
			if recoveryErr == nil {
				switch recovered.Status {
				case ToolRecoveryCompleted:
					if err := s.updateToolOperation(call.ID, operation.Turn, "completed", recovered.Output, ""); err != nil {
						return replayed, fmt.Errorf("persist recovered tool result: %w", err)
					}
					results = append(results, toolResult(call.ID, recovered.Output, false))
					continue
				case ToolRecoveryFailed:
					if err := s.updateToolOperation(call.ID, operation.Turn, "failed", recovered.Error, recovered.Error); err != nil {
						return replayed, fmt.Errorf("persist recovered tool failure: %w", err)
					}
					results = append(results, toolResult(call.ID, recovered.Error, true))
					continue
				case ToolRecoveryNotFound:
					if err := s.updateToolOperation(call.ID, operation.Turn, "not_found", "", "External system reports no operation for this idempotency key."); err != nil {
						return replayed, fmt.Errorf("persist missing external operation state: %w", err)
					}
				case ToolRecoveryUnknown:
					// Fall through to safe replay or the default unknown outcome.
				}
			} else {
				operation.Error = "Status lookup failed: " + recoveryErr.Error()
			}
		}

		if !canReplay {
			message := "Previous process stopped while this tool was running. Its side-effect status is unknown; inspect the target before deciding whether to retry."
			if operation.Error != "" {
				message += " " + operation.Error
			}
			if err := s.updateToolOperation(call.ID, operation.Turn, "unknown", "", message); err != nil {
				return replayed, fmt.Errorf("persist unknown tool outcome: %w", err)
			}
			results = append(results, toolResult(call.ID, message, true))
			continue
		}

		if err := s.updateToolOperation(call.ID, operation.Turn, "safe_to_replay", "", ""); err != nil {
			return replayed, fmt.Errorf("persist safe-to-replay state: %w", err)
		}
		results = append(results, s.executeTool(ctx, operation.Turn, call, limits.ToolTimeout, limits.MaxToolResultBytes))
		replayed++
	}
	if err := s.appendToolResults(results); err != nil {
		return replayed, err
	}
	return replayed, nil
}

// CompensateToolOperation explicitly invokes a tool's configured compensator.
// Compensation is never automatic because the correct rollback is domain-specific.
func (s *Session) CompensateToolOperation(ctx context.Context, callID string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("cannot compensate while the session is running")
	}
	index := s.toolOperationIndexLocked(callID, 0)
	if index < 0 {
		for i := len(s.toolOperations) - 1; i >= 0; i-- {
			if s.toolOperations[i].ToolCallID == callID {
				index = i
				break
			}
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return errors.New("tool operation was not found")
	}
	operation := s.toolOperations[index]
	var tool Tool
	exists := false
	if s.Tools != nil {
		tool, exists = s.Tools.byName[operation.Tool]
	}
	s.mu.Unlock()
	if !exists || tool.Compensate == nil {
		return errors.New("tool does not provide compensation")
	}
	if operation.Status != "completed" && operation.Status != "compensating" && operation.Status != "compensation_unknown" {
		return fmt.Errorf("cannot compensate tool operation in %q state", operation.Status)
	}
	if operation.CompensationKey == "" {
		operation.CompensationKey = operation.IdempotencyKey + ":compensate"
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("cannot compensate while the session is running")
	}
	index = s.toolOperationIndexLocked(callID, operation.Turn)
	if index < 0 {
		s.mu.Unlock()
		return errors.New("tool operation disappeared before compensation")
	}
	previous := s.toolOperations[index]
	s.toolOperations[index].Status = "compensating"
	s.toolOperations[index].CompensationKey = operation.CompensationKey
	s.toolOperations[index].UpdatedAt = time.Now().UTC()
	if err := s.persistLocked(); err != nil {
		s.toolOperations[index] = previous
		s.mu.Unlock()
		return fmt.Errorf("persist compensation intent: %w", err)
	}
	operation = s.toolOperations[index]
	s.mu.Unlock()
	ctx = context.WithValue(ctx, toolCallIDContextKey{}, operation.ToolCallID)
	ctx = context.WithValue(ctx, toolIdempotencyKeyContextKey{}, operation.CompensationKey)
	if err := tool.Compensate(ctx, operation.Args, operation.CompensationKey); err != nil {
		s.mu.Lock()
		index = s.toolOperationIndexLocked(callID, operation.Turn)
		if index >= 0 {
			s.toolOperations[index].Status = "compensation_unknown"
			s.toolOperations[index].Error = err.Error()
			s.toolOperations[index].UpdatedAt = time.Now().UTC()
			_ = s.persistLocked()
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index = s.toolOperationIndexLocked(callID, operation.Turn)
	if index < 0 {
		return errors.New("tool operation disappeared during compensation")
	}
	previous = s.toolOperations[index]
	s.toolOperations[index].Status = "compensated"
	s.toolOperations[index].Error = ""
	s.toolOperations[index].UpdatedAt = time.Now().UTC()
	if err := s.persistLocked(); err != nil {
		s.toolOperations[index] = previous
		return err
	}
	return nil
}

func (s *Session) executeTool(ctx context.Context, turn int, call ContentBlock, timeout time.Duration, maxResultBytes int) ContentBlock {
	tool, exists := s.resolveToolCall(call.Name, call.Input)
	started := time.Now()
	toolTrace, traceErr := tracecontext.Child(s.TraceContext)
	if traceErr != nil {
		toolTrace = s.TraceContext
	}
	emitToolEvent := func(event Event) {
		event.TraceID = toolTrace.TraceID
		event.SpanID = toolTrace.SpanID
		event.ParentSpanID = toolTrace.ParentSpanID
		s.emit(event)
	}
	emitToolEvent(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Args: eventToolArgs(call.Input), Status: "running"})
	if err := ctx.Err(); err != nil {
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Error: err.Error(), ResultOutput: err.Error(), Status: "failed"})
		return toolResult(call.ID, "Tool call cancelled before execution: "+err.Error(), true)
	}
	if !exists {
		message := "unknown tool"
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if !isSearchRouterCall(call.Name) && s.Tools != nil && !s.Tools.isActive(call.Name) {
		message := "tool is available in the catalog but not active; use search with scope=tools to find and activate it first"
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if tool.InternalOnly && call.Name != "read_tool_output" {
		message := "this operation is available only through the matching search/read route"
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if pendingSkills := s.pendingRequiredSkills(); !skillBootstrapCallAllowed(call.Name, call.Input, pendingSkills) {
		message := "load the matching repository Skill with read_skill before using repository tools"
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if !json.Valid(call.Input) {
		message := "tool arguments are not valid JSON"
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if tool.Validate != nil {
		if err := tool.Validate(call.Input); err != nil {
			emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: err.Error(), ResultOutput: err.Error(), Status: "failed"})
			return toolResult(call.ID, "invalid tool arguments: "+err.Error(), true)
		}
	}
	if call.Name == "read_file" && hasToolOutputArtifact(call.Input) {
		if s.ToolOutputs == nil {
			message := "tool output artifact storage is unavailable"
			emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
			return toolResult(call.ID, message, true)
		}
	}
	key := toolIdempotencyKey(s.ID, turn, call)
	previousOperation, hadPreviousOperation := s.existingToolOperation(call.ID)
	if hadPreviousOperation && previousOperation.Turn == turn && previousOperation.IdempotencyKey != "" {
		key = previousOperation.IdempotencyKey
	}
	if err := s.beginToolOperation(turn, call, key, tool.ReplayPolicy); err != nil {
		message := "could not persist tool operation intent: " + err.Error()
		emitToolEvent(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	toolCtx = tracecontext.WithContext(toolCtx, toolTrace)
	runContext := tracecontext.WithContext(ctx, toolTrace)
	metadata := RunMetadata{TraceContext: toolTrace, TaskID: s.TaskID, RunID: s.RunID, WorkerID: s.WorkerID, EventSink: s.OnEvent}
	runContext = WithRunMetadata(runContext, metadata)
	toolCtx = context.WithValue(toolCtx, sessionRunContextKey{}, runContext)
	toolCtx = WithRunMetadata(toolCtx, metadata)
	toolCtx = context.WithValue(toolCtx, toolTurnContextKey{}, turn)
	toolCtx = context.WithValue(toolCtx, toolCallIDContextKey{}, call.ID)
	toolCtx = context.WithValue(toolCtx, toolIdempotencyKeyContextKey{}, key)
	var output string
	var callErr error
	handlerInvoked := false
	if s.BeforeTool != nil {
		callErr = s.BeforeTool(toolCtx, tool, call.Input)
		if callErr != nil {
			emitToolEvent(Event{Type: "tool_permission_denied", Turn: turn, Tool: call.Name, Error: callErr.Error()})
		}
	}
	if callErr == nil {
		callErr = toolCtx.Err()
	}
	if callErr == nil {
		handlerInvoked = true
		output, callErr = tool.Handle(toolCtx, call.Input)
	}
	if callErr == nil && call.Name == "read_skill" {
		var args struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if json.Unmarshal(call.Input, &args) == nil && strings.TrimSpace(args.Path) == "" {
			s.markSkillLoaded(args.Name)
		}
	}
	if s.AfterTool != nil {
		s.AfterTool(toolCtx, tool, call.Input, output, callErr)
	}
	if callErr != nil {
		output = toolErrorOutput(call.Name, callErr.Error())
	}
	if output == "" && callErr == nil {
		output = "Tool completed with no output."
	}
	outputBytes := len(output)
	if len(output) > maxResultBytes || strings.Count(output, "\n") >= 2000 {
		if s.ToolOutputs != nil && call.Name != "read_tool_output" && !hasToolOutputArtifact(call.Input) {
			artifact, saveErr := s.ToolOutputs.Save(output)
			if saveErr == nil {
				output = externalizedToolOutput(artifact, output, maxResultBytes)
			} else {
				output = truncateToolOutput(truncateToolOutputLines(output, 2000), maxResultBytes) + "\n[full tool output was not saved: " + saveErr.Error() + "]"
			}
		} else {
			output = truncateToolOutput(truncateToolOutputLines(output, 2000), maxResultBytes)
		}
	}
	operationStatus, operationError := "completed", ""
	if callErr != nil {
		operationStatus, operationError = "failed", callErr.Error()
		if handlerInvoked {
			// Handler errors may happen after an external side effect, so only
			// the adapter's recovery hook can resolve whether it took effect.
			operationStatus = "unknown"
		} else if hadPreviousOperation && (previousOperation.Status == "running" || previousOperation.Status == "unknown" || previousOperation.Status == "not_found" || previousOperation.Status == "safe_to_replay") {
			// A denied or cancelled recovery attempt says nothing about whether the
			// previous process completed its side effect.
			operationStatus = "unknown"
		}
	}
	if err := s.finishToolOperation(call.ID, turn, operationStatus, output, operationError); err != nil {
		// Keep the durable operation in "running" so recovery can query its
		// external status instead of assuming the side effect failed.
		callErr = fmt.Errorf("tool ran but its outcome could not be persisted: %w", err)
		output = callErr.Error()
	}
	event := Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Bytes: outputBytes, ResultSummary: truncateToolOutput(strings.TrimSpace(output), 1024), ResultOutput: truncateToolOutput(truncateToolOutputLines(strings.TrimSpace(output), 2000), 50<<10), Status: "completed"}
	if callErr != nil {
		event.Error = callErr.Error()
		event.Status = "failed"
	}
	emitToolEvent(event)
	return toolResult(call.ID, output, callErr != nil)
}

func requiredSkillBootstrapTools(definitions []ToolDefinition, pendingSkills []string) []ToolDefinition {
	// Keep the request schema stable for prefix-based provider prompt caches.
	// executeTool still blocks repository actions until required Skills load.
	return definitions
}

func skillBootstrapCallAllowed(name string, input json.RawMessage, pendingSkills []string) bool {
	if len(pendingSkills) == 0 {
		return true
	}
	if name == sessionSearchToolName || name == toolSearchToolName {
		return true
	}
	if name != "read_skill" {
		return false
	}
	var args struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(input, &args) != nil {
		return false
	}
	for _, required := range pendingSkills {
		if args.Name == required {
			return true
		}
	}
	return false
}

func (s *Session) pendingRequiredSkills() []string {
	if len(s.RequiredSkills) == 0 {
		return nil
	}
	pending := make([]string, 0, len(s.RequiredSkills))
	for _, name := range s.RequiredSkills {
		if s.loadedSkills == nil || !s.loadedSkills[name] {
			pending = append(pending, name)
		}
	}
	return pending
}

func (s *Session) markSkillLoaded(name string) {
	if s.loadedSkills == nil {
		s.loadedSkills = make(map[string]bool)
	}
	s.loadedSkills[name] = true
}

func (s *Session) emit(event Event) {
	if s.OnEvent != nil {
		event.At = time.Now().UTC()
		event.Session = s.ID
		if event.TraceID == "" {
			event.TraceID = s.TraceContext.TraceID
		}
		if event.SpanID == "" {
			event.SpanID = s.TraceContext.SpanID
			event.ParentSpanID = s.TraceContext.ParentSpanID
		}
		if event.TaskID == "" {
			event.TaskID = s.TaskID
		}
		if event.RunID == "" {
			event.RunID = s.RunID
		}
		if event.WorkerID == "" {
			event.WorkerID = s.WorkerID
		}
		s.OnEvent(event)
	}
}

func shouldRecoverMissingStopReason(stopReason string) bool {
	return strings.TrimSpace(stopReason) == ""
}

func eventToolArgs(input json.RawMessage) json.RawMessage {
	const maxArgsBytes = 2048
	if len(input) <= maxArgsBytes {
		return append(json.RawMessage(nil), input...)
	}
	return json.RawMessage(`{"truncated":true}`)
}

func (s *Session) appendAssistant(content []ContentBlock, toolCalls []ContentBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingTools) != 0 {
		return errors.New("cannot append an assistant response while tools are pending")
	}
	previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
	previousDraft, previousDraftTurn := s.modelDraft, s.modelDraftTurn
	s.modelDraft, s.modelDraftTurn = "", 0
	s.appendMessagesLocked([]Message{{Role: "assistant", Content: append([]ContentBlock(nil), content...)}})
	s.pendingTools = append([]ContentBlock(nil), toolCalls...)
	if err := s.persistLocked(); err != nil {
		s.messages = s.messages[:previousMessages]
		s.entries = s.entries[:previousEntries]
		s.head = previousHead
		s.pendingTools = nil
		s.modelDraft, s.modelDraftTurn = previousDraft, previousDraftTurn
		return fmt.Errorf("persist assistant response: %w", err)
	}
	return nil
}

func (s *Session) setModelDraft(text string, turn int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousDraft, previousTurn := s.modelDraft, s.modelDraftTurn
	s.modelDraft, s.modelDraftTurn = text, turn
	if err := s.persistLocked(); err != nil {
		s.modelDraft, s.modelDraftTurn = previousDraft, previousTurn
		return err
	}
	return nil
}

func (s *Session) appendToolResults(results []ContentBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousPending := append([]ContentBlock(nil), s.pendingTools...)
	previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
	s.appendMessagesLocked([]Message{{Role: "user", Content: append([]ContentBlock(nil), results...)}})
	s.pendingTools = nil
	if err := s.persistLocked(); err != nil {
		s.messages = s.messages[:previousMessages]
		s.entries = s.entries[:previousEntries]
		s.head = previousHead
		s.pendingTools = previousPending
		return fmt.Errorf("persist tool results: %w", err)
	}
	return nil
}

func (s *Session) appendRecoveryPrompt(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
	s.appendMessagesLocked([]Message{userMessage(text)})
	if err := s.persistLocked(); err != nil {
		s.messages = s.messages[:previousMessages]
		s.entries = s.entries[:previousEntries]
		s.head = previousHead
		return fmt.Errorf("persist recovery prompt: %w", err)
	}
	return nil
}

func (s *Session) consumeSteering() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeQueueLocked(&s.steering)
}

func (s *Session) consumeFollowUps() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeQueueLocked(&s.followUps)
}

func (s *Session) consumeQueueLocked(queue *[]Message) (bool, error) {
	if len(*queue) == 0 {
		return false, nil
	}
	previous := *queue
	previousMessages, previousEntries, previousHead := len(s.messages), len(s.entries), s.head
	s.appendMessagesLocked(cloneMessages(previous))
	*queue = nil
	if err := s.persistLocked(); err != nil {
		s.messages = s.messages[:previousMessages]
		s.entries = s.entries[:previousEntries]
		s.head = previousHead
		*queue = previous
		return false, fmt.Errorf("persist queued messages: %w", err)
	}
	return true, nil
}

func userMessage(text string) Message {
	return Message{Role: "user", Content: []ContentBlock{{Type: "text", Text: text}}}
}

func userMessageWithID(id, text string) Message {
	return Message{ID: id, Role: "user", Content: []ContentBlock{{Type: "text", Text: text}}}
}

func toolResult(id, output string, isError bool) ContentBlock {
	return ContentBlock{Type: "tool_result", ToolUseID: id, Content: output, IsError: isError}
}

func textBlocks(blocks []ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
