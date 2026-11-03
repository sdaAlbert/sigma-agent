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
	Definition    ToolDefinition
	ExecutionMode string // "parallel" for independent reads; "parallel_write" for path-locked mutations; otherwise serialized.
	Validate      func(json.RawMessage) error
	Handle        ToolHandler
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
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	InputTokens  int            `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
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
	At                    time.Time `json:"at"`
	Type                  string    `json:"type"`
	Session               string    `json:"session_id,omitempty"`
	Turn                  int       `json:"turn,omitempty"`
	Tool                  string    `json:"tool,omitempty"`
	Duration              int64     `json:"duration_ms,omitempty"`
	Bytes                 int       `json:"bytes,omitempty"`
	MessagesDropped       int       `json:"messages_dropped,omitempty"`
	EstimatedTokensBefore int       `json:"estimated_tokens_before,omitempty"`
	EstimatedTokensAfter  int       `json:"estimated_tokens_after,omitempty"`
	Error                 string    `json:"error,omitempty"`
	Text                  string    `json:"text,omitempty"`
}

type EventSink func(Event)
type ToolPolicy func(context.Context, Tool, json.RawMessage) error
type ToolObserver func(context.Context, Tool, json.RawMessage, string, error)

type Limits struct {
	MaxTurns           int
	MaxToolCalls       int
	MaxToolResultBytes int
	ToolTimeout        time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxTurns: 128, MaxToolCalls: 256, MaxToolResultBytes: 16 * 1024, ToolTimeout: 60 * time.Second}
}

type Result struct {
	Text         string
	Turns        int
	ToolCalls    int
	InputTokens  int
	OutputTokens int
}

type Session struct {
	ID          string
	System      string
	Model       Model
	Tools       *ToolRegistry
	Limits      Limits
	OnEvent     EventSink
	BeforeTool  ToolPolicy
	AfterTool   ToolObserver
	Store       SessionStore
	Context     ContextPreparer
	ToolOutputs ToolOutputStore

	mu           sync.Mutex
	messages     []Message
	entries      []SessionEntry
	head         string
	modelID      string
	steering     []Message
	followUps    []Message
	pendingTools []ContentBlock
	running      bool
	cancel       context.CancelFunc
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
	reservedTurns := limits.MaxTurns / 2
	if reservedTurns < 1 {
		reservedTurns = 1
	}
	checkpointTurn := limits.MaxTurns - reservedTurns
	if checkpointTurn < 1 {
		checkpointTurn = 1
	}
	reservedToolCalls := limits.MaxToolCalls / 2
	if reservedToolCalls < 1 {
		reservedToolCalls = 1
	}
	checkpointToolCall := limits.MaxToolCalls - reservedToolCalls
	if checkpointToolCall < 1 {
		checkpointToolCall = 1
	}
	toolOrder := 0
	lastWriteOrder, lastTestOrder, lastDiffOrder := 0, 0, 0
	lastTestPassed := false
	reviewOnly := false
	outputContinuationRetries := 0
	const maxOutputContinuationRetries = 2
	for turn := 1; turn <= limits.MaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			s.emit(Event{Type: "agent_aborted", Turn: turn, Error: err.Error()})
			return result, err
		}
		result.Turns = turn
		s.emit(Event{Type: "turn_start", Turn: turn})
		s.emit(Event{Type: "model_request", Turn: turn})
		systemPrompt := s.System
		finalizationTurn := checkpointTurn
		if result.ToolCalls >= checkpointToolCall && turn < finalizationTurn {
			finalizationTurn = turn
		}
		finalizationPhase := turn >= finalizationTurn
		testsNeeded := lastWriteOrder > 0 && lastTestOrder <= lastWriteOrder
		diffNeeded := lastWriteOrder > 0 && lastDiffOrder <= lastWriteOrder
		reviewNeeded := testsNeeded || diffNeeded
		verificationComplete := lastWriteOrder > 0 && lastTestOrder > lastWriteOrder && lastDiffOrder > lastWriteOrder && lastTestPassed
		reviewOnly = reviewNeeded && !(lastWriteOrder > 0 && lastTestOrder > lastWriteOrder && !lastTestPassed)
		if lastWriteOrder > 0 && lastTestOrder > lastWriteOrder && !lastTestPassed {
			systemPrompt += "\n\nLATEST TEST RESULT FAILED: inspect the first actionable compiler or test failure, make a targeted correction, then rerun the relevant test and inspect git_diff again. Do not finish with a newly introduced compile failure. If the failure is demonstrably pre-existing or environmental, preserve the evidence and report that limitation."
		}
		if reviewNeeded {
			systemPrompt += "\n\nREQUIRED REVIEW: Source changes were made in this run. Complete the checks still outstanding after the latest successful edit. The runtime exposes only missing checks; do not repeat a completed check. Use run_tests for Go profiles or run_command with purpose=test for repository-native checks; inspect the result status and git_diff. If a test cannot run, keep its output as evidence and report why."
		}
		if verificationComplete {
			systemPrompt += "\n\nVERIFICATION COMPLETE: The current source revision has passed its required test command and its diff has been inspected. Do not rerun tests or git_diff without a new source edit. If the inspection revealed a concrete defect, make a targeted edit; otherwise provide the final summary now."
		}
		if finalizationPhase {
			remaining := limits.MaxTurns - turn + 1
			systemPrompt += fmt.Sprintf("\n\nFINALIZATION PHASE: %d turns remain. Stop broad exploration. Only make a targeted correction if a test or diff review shows it is needed; then rerun the checks. If a write was rejected because the file changed, read that file again and rebase the edit.", remaining)
		}
		requestMessages := s.Messages()
		if s.Context != nil {
			prepared, prepareErr := s.Context.Prepare(systemPrompt, requestMessages)
			if prepareErr != nil {
				return result, fmt.Errorf("prepare model context: %w", prepareErr)
			}
			systemPrompt, requestMessages = prepared.System, prepared.Messages
			if prepared.DroppedMessages > 0 {
				s.emit(Event{Type: "context_compacted", Turn: turn, MessagesDropped: prepared.DroppedMessages, EstimatedTokensBefore: prepared.EstimatedBefore, EstimatedTokensAfter: prepared.EstimatedAfter})
			}
		}
		toolDefinitions := s.Tools.Definitions()
		if finalizationPhase || reviewOnly || verificationComplete {
			toolDefinitions = finalizationToolDefinitions(toolDefinitions, reviewOnly, testsNeeded, diffNeeded, verificationComplete)
		}
		response, err := s.Model.Stream(ctx, systemPrompt, requestMessages, toolDefinitions, func(modelEvent ModelEvent) {
			s.emit(Event{Type: "model_" + modelEvent.Type, Turn: turn, Text: modelEvent.Text, Tool: modelEvent.Name})
		})
		if err != nil {
			s.emit(Event{Type: "model_error", Turn: turn, Error: err.Error()})
			return result, err
		}
		result.InputTokens += response.InputTokens
		result.OutputTokens += response.OutputTokens
		result.Text += textBlocks(response.Content)
		toolCalls := make([]ContentBlock, 0)
		for _, block := range response.Content {
			if block.Type == "tool_use" {
				toolCalls = append(toolCalls, block)
			}
		}
		if response.StopReason == "max_tokens" || response.StopReason == "length" {
			if len(toolCalls) > 0 {
				if err := s.appendRecoveryPrompt("The previous tool-call output was truncated by the output-token limit. No partial tool call was executed. Re-issue the required tool call with complete arguments; do not assume the partial call succeeded."); err != nil {
					return result, err
				}
			} else {
				if err := s.appendAssistant(response.Content, nil); err != nil {
					return result, err
				}
				if err := s.appendRecoveryPrompt("Your previous response ended because it reached the output-token limit. Continue from exactly where it stopped, without repeating the completed part. If more output is needed, continue in another section."); err != nil {
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
			toolResults, toolErr := s.executeTools(ctx, turn, finalizationTurn, reviewOnly, testsNeeded, diffNeeded, verificationComplete, toolCalls, limits.ToolTimeout, limits.MaxToolResultBytes)
			for index, call := range toolCalls {
				if index >= len(toolResults) {
					continue
				}
				if isVerificationGateRejection(toolResults[index]) {
					continue
				}
				toolOrder++
				if call.Name == "run_tests" {
					lastTestOrder = toolOrder
					lastTestPassed = !toolResults[index].IsError && strings.Contains(toolResults[index].Content, "run_tests status=passed exit_code=0")
					continue
				}
				if call.Name == "run_command" {
					content := toolResults[index].Content
					statusLine := ""
					if marker := strings.Index(content, "SIGMA_RUN_COMMAND "); marker >= 0 {
						statusLine = strings.SplitN(content[marker:], "\n", 2)[0]
					}
					workspaceChanged := strings.Contains(statusLine, "workspace_changed=true")
					if workspaceChanged {
						lastWriteOrder = toolOrder
						reviewOnly = true
					}
					var commandInput struct {
						Purpose string `json:"purpose"`
						Action  string `json:"action"`
					}
					_ = json.Unmarshal(call.Input, &commandInput)
					status := ""
					responsePurpose, responseAction := "", ""
					if marker := strings.Index(content, "SIGMA_RUN_COMMAND "); marker >= 0 {
						for _, field := range strings.Fields(strings.SplitN(content[marker:], "\n", 2)[0]) {
							if strings.HasPrefix(field, "status=") {
								status = strings.TrimPrefix(field, "status=")
							} else if strings.HasPrefix(field, "purpose=") {
								responsePurpose = strings.TrimPrefix(field, "purpose=")
							} else if strings.HasPrefix(field, "action=") {
								responseAction = strings.TrimPrefix(field, "action=")
							}
						}
					}
					if commandInput.Purpose == "" {
						commandInput.Purpose = responsePurpose
					}
					if commandInput.Action == "" {
						commandInput.Action = responseAction
						if commandInput.Action == "" {
							commandInput.Action = "run"
						}
					}
					if commandInput.Purpose == "test" && commandInput.Action != "start" && status != "running" {
						lastTestOrder = toolOrder
						lastTestPassed = !toolResults[index].IsError && !workspaceChanged && strings.Contains(statusLine, "status=passed") && strings.Contains(statusLine, "exit_code=0")
					}
					continue
				}
				if toolResults[index].IsError {
					continue
				}
				switch call.Name {
				case "create_file", "edit_file", "apply_patch":
					lastWriteOrder = toolOrder
					reviewOnly = true
				case "git_diff":
					lastDiffOrder = toolOrder
				}
			}
			if lastWriteOrder > 0 && lastTestOrder > lastWriteOrder && lastDiffOrder > lastWriteOrder {
				// A completed verification pass can reveal a defect that needs a
				// targeted fix. Permit that fix even near the run limit; the next
				// turn will require tests and a diff again after the new write.
				reviewOnly = false
			}
			if len(toolResults) > 0 {
				if err := s.appendToolResults(toolResults); err != nil {
					return result, err
				}
			}
			s.emit(Event{Type: "turn_end", Turn: turn})
			if toolErr != nil {
				return result, toolErr
			}
			if _, err := s.consumeSteering(); err != nil {
				return result, err
			}
			continue
		}
		if err := s.appendAssistant(response.Content, nil); err != nil {
			return result, err
		}
		s.emit(Event{Type: "turn_end", Turn: turn})
		if lastWriteOrder > 0 && (lastTestOrder <= lastWriteOrder || lastDiffOrder <= lastWriteOrder) {
			if turn == limits.MaxTurns {
				return result, fmt.Errorf("implementation changes were not finalized: %s must run after the latest successful edit", strings.Join(missingReviewChecks(lastWriteOrder, lastTestOrder, lastDiffOrder), " and "))
			}
			reviewOnly = true
			continue
		}
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
			return result, nil
		}
	}
	if lastWriteOrder > 0 && (lastTestOrder <= lastWriteOrder || lastDiffOrder <= lastWriteOrder) {
		return result, fmt.Errorf("agent turn limit (%d) reached with unverified changes: %s did not run after the latest edit", limits.MaxTurns, strings.Join(missingReviewChecks(lastWriteOrder, lastTestOrder, lastDiffOrder), " and "))
	}
	return result, fmt.Errorf("agent turn limit (%d) reached", limits.MaxTurns)
}

func missingReviewChecks(lastWriteOrder, lastTestOrder, lastDiffOrder int) []string {
	var missing []string
	if lastTestOrder <= lastWriteOrder {
		missing = append(missing, "a relevant test command (run_tests or run_command with purpose=test)")
	}
	if lastDiffOrder <= lastWriteOrder {
		missing = append(missing, "git_diff")
	}
	return missing
}

func finalizationToolDefinitions(definitions []ToolDefinition, reviewOnly, testsNeeded, diffNeeded, verificationComplete bool) []ToolDefinition {
	allowed := map[string]bool{
		"read_file": true, "record_change_contract": true, "create_file": true, "edit_file": true, "apply_patch": true,
		"run_tests": true, "run_command": true, "git_diff": true,
	}
	if reviewOnly {
		allowed = map[string]bool{
			"run_tests":   testsNeeded,
			"run_command": testsNeeded,
			"git_diff":    diffNeeded,
		}
	} else if verificationComplete {
		allowed = map[string]bool{
			"read_file":   true,
			"create_file": true,
			"edit_file":   true,
			"apply_patch": true,
		}
	}
	filtered := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if allowed[definition.Name] {
			filtered = append(filtered, definition)
		}
	}
	return filtered
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

func (s *Session) executeTools(ctx context.Context, turn, finalizationTurn int, reviewOnly, testsNeeded, diffNeeded, verificationComplete bool, calls []ContentBlock, timeout time.Duration, maxResultBytes int) ([]ContentBlock, error) {
	parallel := len(calls) > 1
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
				results[i] = s.executeTool(ctx, turn, finalizationTurn, reviewOnly, testsNeeded, diffNeeded, verificationComplete, call, timeout, maxResultBytes)
			}(i, call)
		}
		wg.Wait()
	} else {
		for i, call := range calls {
			results[i] = s.executeTool(ctx, turn, finalizationTurn, reviewOnly, testsNeeded, diffNeeded, verificationComplete, call, timeout, maxResultBytes)
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

func (s *Session) executeTool(ctx context.Context, turn, finalizationTurn int, reviewOnly, testsNeeded, diffNeeded, verificationComplete bool, call ContentBlock, timeout time.Duration, maxResultBytes int) ContentBlock {
	tool, exists := s.Tools.byName[call.Name]
	started := time.Now()
	s.emit(Event{Type: "tool_execution_start", Turn: turn, Tool: call.Name})
	if err := ctx.Err(); err != nil {
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Duration: time.Since(started).Milliseconds(), Error: err.Error()})
		return toolResult(call.ID, "Tool call cancelled before execution: "+err.Error(), true)
	}
	if !exists {
		message := "unknown tool"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Error: message})
		return toolResult(call.ID, message, true)
	}
	if turn >= finalizationTurn && !isFinalizationTool(call.Name) {
		message := "finalization phase only allows read_file, targeted file edits, run_tests, run_command, and git_diff"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Duration: time.Since(started).Milliseconds(), Error: message})
		return toolResult(call.ID, message, true)
	}
	if reviewOnly && !reviewToolNeeded(call.Name, testsNeeded, diffNeeded) {
		message := "verification phase only allows checks still outstanding after the latest edit; do not repeat completed checks"
		if !testsNeeded && !diffNeeded {
			message = "verification is complete; no further tools are allowed in the finalization phase"
		}
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Duration: time.Since(started).Milliseconds(), Error: message})
		return toolResult(call.ID, message, true)
	}
	if verificationComplete && isVerificationCheck(call.Name, call.Input) {
		message := "verification is complete for the unchanged source; make a targeted edit if needed or provide the final summary"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Duration: time.Since(started).Milliseconds(), Error: message})
		return toolResult(call.ID, message, true)
	}
	if !json.Valid(call.Input) {
		message := "tool arguments are not valid JSON"
		s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Error: message})
		return toolResult(call.ID, message, true)
	}
	if tool.Validate != nil {
		if err := tool.Validate(call.Input); err != nil {
			s.emit(Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Error: err.Error()})
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
	if s.AfterTool != nil {
		s.AfterTool(toolCtx, tool, call.Input, output, callErr)
	}
	if callErr != nil {
		output = "Tool error: " + callErr.Error()
	}
	if output == "" && callErr == nil {
		output = "Tool completed with no output."
	}
	outputBytes := len(output)
	if len(output) > maxResultBytes {
		if s.ToolOutputs != nil && call.Name != "read_tool_output" {
			artifact, saveErr := s.ToolOutputs.Save(output)
			if saveErr == nil {
				output = externalizedToolOutput(artifact, output, maxResultBytes)
			} else {
				output = truncateToolOutput(output, maxResultBytes) + "\n[full tool output was not saved: " + saveErr.Error() + "]"
			}
		} else {
			output = truncateToolOutput(output, maxResultBytes)
		}
	}
	event := Event{Type: "tool_execution_end", Turn: turn, Tool: call.Name, Duration: time.Since(started).Milliseconds(), Bytes: outputBytes}
	if callErr != nil {
		event.Error = callErr.Error()
	}
	s.emit(event)
	return toolResult(call.ID, output, callErr != nil)
}

func reviewToolNeeded(name string, testsNeeded, diffNeeded bool) bool {
	return name == "run_tests" && testsNeeded || name == "run_command" && testsNeeded || name == "git_diff" && diffNeeded
}

func isVerificationCheck(name string, input json.RawMessage) bool {
	switch name {
	case "run_tests", "git_diff":
		return true
	case "run_command":
		var command struct {
			Purpose string `json:"purpose"`
		}
		return json.Unmarshal(input, &command) == nil && command.Purpose == "test"
	default:
		return false
	}
}

func isVerificationGateRejection(result ContentBlock) bool {
	if !result.IsError {
		return false
	}
	return strings.Contains(result.Content, "verification phase only allows checks still outstanding") ||
		strings.Contains(result.Content, "verification is complete for the unchanged source")
}

func isFinalizationTool(name string) bool {
	switch name {
	case "read_file", "record_change_contract", "create_file", "edit_file", "apply_patch", "run_tests", "run_command", "git_diff":
		return true
	default:
		return false
	}
}

func (s *Session) emit(event Event) {
	if s.OnEvent != nil {
		event.At = time.Now().UTC()
		event.Session = s.ID
		s.OnEvent(event)
	}
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
