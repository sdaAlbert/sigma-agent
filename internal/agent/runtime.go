package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ToolHandler func(context.Context, json.RawMessage) (string, error)

type toolTurnContextKey struct{}

// ToolTurn returns the assistant turn in which the current tool call was made.
func ToolTurn(ctx context.Context) (int, bool) {
	turn, ok := ctx.Value(toolTurnContextKey{}).(int)
	return turn, ok
}

type Tool struct {
	Definition        ToolDefinition
	ExecutionMode     string // "parallel" for independent reads; "parallel_write" for path-locked mutations; otherwise serialized.
	ModifiesWorkspace bool   // Successful calls force the normal test-and-diff review gate.
	Validate          func(json.RawMessage) error
	Handle            ToolHandler
}

type ToolRegistry struct{ byName map[string]Tool }

func NewToolRegistry(tools []Tool) (*ToolRegistry, error) {
	registry := &ToolRegistry{byName: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		if tool.Definition.Name == "" || tool.Handle == nil {
			return nil, errors.New("every tool needs a name and handler")
		}
		if _, exists := registry.byName[tool.Definition.Name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", tool.Definition.Name)
		}
		registry.byName[tool.Definition.Name] = tool
	}
	return registry, nil
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]ToolDefinition, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, r.byName[name].Definition)
	}
	return definitions
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
	At                       time.Time       `json:"at"`
	Type                     string          `json:"type"`
	Session                  string          `json:"session_id,omitempty"`
	Turn                     int             `json:"turn,omitempty"`
	Tool                     string          `json:"tool,omitempty"`
	ToolCallID               string          `json:"tool_call_id,omitempty"`
	Duration                 int64           `json:"duration_ms,omitempty"`
	Bytes                    int             `json:"bytes,omitempty"`
	MessagesDropped          int             `json:"messages_dropped,omitempty"`
	EstimatedTokensBefore    int             `json:"estimated_tokens_before,omitempty"`
	EstimatedTokensAfter     int             `json:"estimated_tokens_after,omitempty"`
	InputTokens              int             `json:"input_tokens,omitempty"`
	OutputTokens             int             `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int             `json:"cache_read_input_tokens,omitempty"`
	CacheMissInputTokens     int             `json:"cache_miss_input_tokens,omitempty"`
	CacheCreationInputTokens int             `json:"cache_creation_input_tokens,omitempty"`
	SummaryTokens            int             `json:"summary_tokens,omitempty"`
	CompactionSummary        string          `json:"compaction_summary,omitempty"`
	Error                    string          `json:"error,omitempty"`
	Text                     string          `json:"text,omitempty"`
	Args                     json.RawMessage `json:"args,omitempty"`
	ResultSummary            string          `json:"result_summary,omitempty"`
	ResultOutput             string          `json:"result_output,omitempty"`
	Status                   string          `json:"status,omitempty"`
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
	ID             string
	Name           string
	System         string
	Model          Model
	Tools          *ToolRegistry
	Limits         Limits
	RequiredSkills []string
	OnEvent        EventSink
	BeforeTool     ToolPolicy
	AfterTool      ToolObserver
	Store          SessionStore
	Context        ContextPreparer
	ToolOutputs    ToolOutputStore

	mu                 sync.Mutex
	messages           []Message
	compactionSummary  string
	compactionBoundary int
	entries            []SessionEntry
	head               string
	modelID            string
	loadedSkills       map[string]bool
	steering           []Message
	followUps          []Message
	pendingTools       []ContentBlock
	running            bool
	cancel             context.CancelFunc
}

func (s *Session) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMessages(s.messages)
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
	if continuation && len(s.messages) > 0 && s.messages[len(s.messages)-1].Role == "assistant" {
		if len(s.steering) > 0 {
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
		if len(s.messages) == 0 || s.messages[len(s.messages)-1].Role == "assistant" {
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
		if err := ctx.Err(); err != nil {
			s.emit(Event{Type: "agent_aborted", Turn: turn, Error: err.Error()})
			return result, err
		}
		result.Turns = turn
		s.emit(Event{Type: "turn_start", Turn: turn})
		s.emit(Event{Type: "model_request", Turn: turn})
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
		response, err := s.Model.Stream(ctx, systemPrompt, requestMessages, toolDefinitions, func(modelEvent ModelEvent) {
			s.emit(Event{Type: "model_" + modelEvent.Type, Turn: turn, Text: modelEvent.Text, Tool: modelEvent.Name})
		})
		if err != nil {
			s.emit(Event{Type: "model_error", Turn: turn, Error: err.Error()})
			return result, err
		}
		result.InputTokens += response.InputTokens
		result.OutputTokens += response.OutputTokens
		result.CacheReadInputTokens += response.CacheReadInputTokens
		result.CacheMissInputTokens += response.CacheMissInputTokens
		result.CacheCreationInputTokens += response.CacheCreationInputTokens
		if !shouldRecoverMissingStopReason(response.StopReason) {
			emptyStopReasonRetries = 0
		}
		s.emit(Event{Type: "model_usage", Turn: turn, InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, CacheReadInputTokens: response.CacheReadInputTokens, CacheMissInputTokens: response.CacheMissInputTokens, CacheCreationInputTokens: response.CacheCreationInputTokens})
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
					s.emit(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Args: eventToolArgs(call.Input), Status: "running"})
					s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultSummary: message, ResultOutput: message, Status: "failed"})
					toolResults[i] = toolResult(call.ID, message, true)
				}
			} else {
				toolResults, toolErr = s.executeTools(ctx, turn, toolCalls, limits.ToolTimeout, limits.MaxToolResultBytes)
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
					appendErr = s.appendToolResultsAndCompact(toolResults, compaction.Summary)
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

func (s *Session) executeTools(ctx context.Context, turn int, calls []ContentBlock, timeout time.Duration, maxResultBytes int) ([]ContentBlock, error) {
	parallel := len(calls) > 1
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
	if parallel {
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Add(1)
			go func(i int, call ContentBlock) {
				defer wg.Done()
				results[i] = s.executeTool(ctx, turn, call, timeout, maxResultBytes)
			}(i, call)
		}
		wg.Wait()
	} else {
		for i, call := range calls {
			results[i] = s.executeTool(ctx, turn, call, timeout, maxResultBytes)
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
	case "create_file", "edit_file", "apply_patch", "prepare_environment":
		return true
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

func (s *Session) executeTool(ctx context.Context, turn int, call ContentBlock, timeout time.Duration, maxResultBytes int) ContentBlock {
	tool, exists := s.Tools.byName[call.Name]
	started := time.Now()
	s.emit(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Args: eventToolArgs(call.Input), Status: "running"})
	if err := ctx.Err(); err != nil {
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Error: err.Error(), ResultOutput: err.Error(), Status: "failed"})
		return toolResult(call.ID, "Tool call cancelled before execution: "+err.Error(), true)
	}
	if !exists {
		message := "unknown tool"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if pendingSkills := s.pendingRequiredSkills(); !skillBootstrapCallAllowed(call.Name, call.Input, pendingSkills) {
		message := "load the matching repository Skill with read_skill before using repository tools"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if !json.Valid(call.Input) {
		message := "tool arguments are not valid JSON"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: message, ResultOutput: message, Status: "failed"})
		return toolResult(call.ID, message, true)
	}
	if tool.Validate != nil {
		if err := tool.Validate(call.Input); err != nil {
			s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Error: err.Error(), ResultOutput: err.Error(), Status: "failed"})
			return toolResult(call.ID, "invalid tool arguments: "+err.Error(), true)
		}
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	toolCtx = context.WithValue(toolCtx, toolTurnContextKey{}, turn)
	var output string
	var callErr error
	if s.BeforeTool != nil {
		callErr = s.BeforeTool(toolCtx, tool, call.Input)
		if callErr != nil {
			s.emit(Event{Type: "tool_permission_denied", Turn: turn, Tool: call.Name, Error: callErr.Error()})
		}
	}
	if callErr == nil {
		callErr = toolCtx.Err()
	}
	if callErr == nil {
		output, callErr = tool.Handle(toolCtx, call.Input)
	}
	if callErr == nil && call.Name == "read_skill" {
		var args struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(call.Input, &args) == nil {
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
		if s.ToolOutputs != nil && call.Name != "read_tool_output" {
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
	event := Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, ToolCallID: call.ID, Duration: time.Since(started).Milliseconds(), Bytes: outputBytes, ResultSummary: truncateToolOutput(strings.TrimSpace(output), 1024), ResultOutput: truncateToolOutput(truncateToolOutputLines(strings.TrimSpace(output), 2000), 50<<10), Status: "completed"}
	if callErr != nil {
		event.Error = callErr.Error()
		event.Status = "failed"
	}
	s.emit(event)
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
	s.appendMessagesLocked([]Message{{Role: "assistant", Content: append([]ContentBlock(nil), content...)}})
	s.pendingTools = append([]ContentBlock(nil), toolCalls...)
	if err := s.persistLocked(); err != nil {
		s.messages = s.messages[:previousMessages]
		s.entries = s.entries[:previousEntries]
		s.head = previousHead
		s.pendingTools = nil
		return fmt.Errorf("persist assistant response: %w", err)
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
