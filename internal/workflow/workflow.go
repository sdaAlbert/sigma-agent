// Package workflow defines the host surface available to Sigma workflow
// extensions. Events are snapshots and never expose mutable session state.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Turn      int    `json:"turn,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Command struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Tool is an Agent callable registered by a workflow extension.
type Tool struct {
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	InputSchema       json.RawMessage `json:"input_schema"`
	ExecutionMode     string          `json:"execution_mode,omitempty"`
	ModifiesWorkspace bool            `json:"modifies_workspace,omitempty"`
}

type Handler interface {
	Notify(message string)
	Confirm(ctx context.Context, message string) (bool, error)
}

type commandHandler func(context.Context, string) error
type eventHandler func(Event)

// Host is the registration and dispatch surface supplied to extensions.
type Host struct {
	mu       sync.RWMutex
	handler  Handler
	commands map[string]registeredCommand
	tools    map[string]registeredTool
	events   map[string][]eventHandler
}

type registeredCommand struct {
	Command
	handler commandHandler
}

type registeredTool struct {
	Tool
	handler func(context.Context, json.RawMessage) (string, error)
}

func NewHost(handler Handler) *Host {
	return &Host{handler: handler, commands: make(map[string]registeredCommand), tools: make(map[string]registeredTool), events: make(map[string][]eventHandler)}
}

// RegisterCommand adds a slash command. Names are lowercase ASCII words and
// cannot replace Sigma's built-in commands.
func (h *Host) RegisterCommand(command Command, run func(context.Context, string) error) error {
	name := strings.TrimSpace(command.Name)
	if !validCommandName(name) || run == nil {
		return fmt.Errorf("invalid workflow command %q", command.Name)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.commands[name]; exists || reservedCommand(name) {
		return fmt.Errorf("workflow command %q is already registered or reserved", name)
	}
	command.Name = name
	h.commands[name] = registeredCommand{Command: command, handler: run}
	return nil
}

// On registers a read-only lifecycle listener. Supported events are
// session_start, turn_start, turn_end, tool_complete, and session_end.
func (h *Host) On(eventType string, callback func(Event)) error {
	if !validEvent(eventType) || callback == nil {
		return fmt.Errorf("unsupported workflow event %q", eventType)
	}
	h.mu.Lock()
	h.events[eventType] = append(h.events[eventType], callback)
	h.mu.Unlock()
	return nil
}

// RegisterTool adds an Agent tool to the common workflow host. The schema
// must be a JSON object, and the tool runs sequentially unless a Go adapter
// supplies another execution mode.
func (h *Host) RegisterTool(tool Tool, run func(context.Context, json.RawMessage) (string, error)) error {
	name := strings.TrimSpace(tool.Name)
	if !validToolName(name) || run == nil {
		return fmt.Errorf("invalid workflow tool %q", tool.Name)
	}
	var schema map[string]any
	if len(tool.InputSchema) == 0 || json.Unmarshal(tool.InputSchema, &schema) != nil || schema == nil {
		return fmt.Errorf("workflow tool %q requires a JSON object input schema", name)
	}
	tool.Name = name
	if tool.ExecutionMode == "" {
		tool.ExecutionMode = "sequential"
	}
	if tool.ExecutionMode != "sequential" && tool.ExecutionMode != "parallel" && tool.ExecutionMode != "parallel_write" {
		return fmt.Errorf("workflow tool %q has unsupported execution mode %q", name, tool.ExecutionMode)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.tools[name]; exists {
		return fmt.Errorf("workflow tool %q is already registered", name)
	}
	h.tools[name] = registeredTool{Tool: tool, handler: run}
	return nil
}

// RegisterExtensionTool reserves MCP namespaces for host-managed MCP adapters.
func (h *Host) RegisterExtensionTool(tool Tool, run func(context.Context, json.RawMessage) (string, error)) error {
	if strings.HasPrefix(strings.TrimSpace(tool.Name), "mcp__") {
		return fmt.Errorf("workflow extensions cannot register reserved MCP tool name %q", tool.Name)
	}
	return h.RegisterTool(tool, run)
}

func (h *Host) Commands() []Command {
	h.mu.RLock()
	defer h.mu.RUnlock()
	commands := make([]Command, 0, len(h.commands))
	for _, command := range h.commands {
		commands = append(commands, command.Command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func (h *Host) Tools() []Tool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	tools := make([]Tool, 0, len(h.tools))
	for _, tool := range h.tools {
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		tools = append(tools, tool.Tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func (h *Host) RunCommand(ctx context.Context, name, args string) (bool, error) {
	h.mu.RLock()
	command, ok := h.commands[name]
	h.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return true, command.handler(ctx, args)
}

func (h *Host) RunTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	h.mu.RLock()
	tool, ok := h.tools[name]
	h.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("workflow tool %q is not registered", name)
	}
	return tool.handler(ctx, append(json.RawMessage(nil), arguments...))
}

func (h *Host) Dispatch(event Event) {
	h.mu.RLock()
	callbacks := append([]eventHandler(nil), h.events[event.Type]...)
	h.mu.RUnlock()
	for _, callback := range callbacks {
		func() {
			defer func() { _ = recover() }()
			callback(event)
		}()
	}
}

func (h *Host) Notify(message string) {
	if h.handler != nil {
		h.handler.Notify(message)
	}
}

func (h *Host) Confirm(ctx context.Context, message string) (bool, error) {
	if h.handler == nil {
		return false, errors.New("workflow confirmation is unavailable")
	}
	return h.handler.Confirm(ctx, message)
}

func validCommandName(name string) bool {
	if name == "" || len(name) > 48 {
		return false
	}
	for i, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && r == '-') {
			return false
		}
	}
	return true
}

func validToolName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func reservedCommand(name string) bool {
	switch name {
	case "help", "repo", "model", "login", "logout", "session", "resume", "network", "publish", "github-write", "start", "exit", "quit", "diff", "tree", "branch", "steer", "followup", "abort", "skill":
		return true
	default:
		return false
	}
}

func validEvent(name string) bool {
	switch name {
	case "session_start", "turn_start", "turn_end", "tool_complete", "session_end":
		return true
	default:
		return false
	}
}
