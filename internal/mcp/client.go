// Package mcp implements a Model Context Protocol client over the stdio
// transport of a child process. It is the shared protocol layer for every MCP
// server Sigma Agent connects to: the GitHub MCP server, and the servers
// declared in mcp.json or contributed by an installed package.
//
// The client owns JSON-RPC framing, protocol version discovery with a
// fallback, tool listing, and process cleanup. Callers own the policy around
// which tools are exposed and how their arguments are scoped.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// ProtocolVersion is the MCP revision used when the server does not
	// advertise the modern protocol.
	ProtocolVersion = "2025-11-25"
	// ModernProtocolVersion is the MCP revision Sigma prefers. Servers that
	// support it are discovered with server/discover instead of initialize.
	ModernProtocolVersion = "2026-07-28"
	// MaxFrameBytes bounds a single JSON-RPC frame read from a server.
	MaxFrameBytes = 4 * 1024 * 1024
	// ClientName identifies Sigma Agent in the MCP handshake.
	ClientName = "sigmaagent-go"
	// ClientVersion identifies Sigma Agent in the MCP handshake.
	ClientVersion = "1.1.3"
	// DefaultStartupTimeout bounds the initialize handshake.
	DefaultStartupTimeout = 15 * time.Second
)

// Config describes one MCP server process.
type Config struct {
	// Name identifies the server. It is used for tool namespacing and in
	// diagnostics.
	Name string
	// Command is the executable that starts the server.
	Command string
	// Args are the arguments passed to Command.
	Args []string
	// Env holds environment overrides applied on top of the parent
	// environment. An entry with an empty value removes the variable.
	Env map[string]string
	// Dir is the working directory of the server process.
	Dir string
	// StartupTimeout bounds the initialize handshake. Zero uses
	// DefaultStartupTimeout.
	StartupTimeout time.Duration
}

// ToolDescriptor describes one tool exposed by an MCP server.
type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type outcome struct {
	result json.RawMessage
	err    error
}

// Client is a connected MCP server. It owns the child process and the
// JSON-RPC framing over its stdio pipes.
type Client struct {
	name    string
	command *exec.Cmd
	stdin   io.WriteCloser

	writeMu      sync.Mutex
	mu           sync.Mutex
	pending      map[string]chan outcome
	closed       bool
	modern       bool
	closeOnce    sync.Once
	requestID    atomic.Uint64
	readDone     chan struct{}
	stderrDone   chan struct{}
	waitDone     chan struct{}
	stderr       *tailBuffer
	instructions string
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, data...)
	if len(b.data) > b.max {
		b.data = append([]byte(nil), b.data[len(b.data)-b.max:]...)
	}
	return len(data), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

// Start launches the server process and completes the MCP handshake.
func Start(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("MCP server needs a name")
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("MCP server %q needs a command", cfg.Name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	command := exec.Command(cfg.Command, cfg.Args...)
	if cfg.Dir != "" {
		command.Dir = cfg.Dir
	}
	if len(cfg.Env) > 0 {
		command.Env = ReplaceEnv(os.Environ(), cfg.Env)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open MCP stdin for %q: %w", cfg.Name, err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open MCP stdout for %q: %w", cfg.Name, err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open MCP stderr for %q: %w", cfg.Name, err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start MCP server %q: %w", cfg.Name, err)
	}
	client := &Client{
		name: cfg.Name, command: command, stdin: stdin,
		pending:  make(map[string]chan outcome),
		readDone: make(chan struct{}), stderrDone: make(chan struct{}), waitDone: make(chan struct{}),
		stderr: &tailBuffer{max: 8 * 1024},
	}
	go client.readResponses(stdout)
	go func() { _, _ = io.Copy(client.stderr, stderr); close(client.stderrDone) }()
	go func() {
		<-client.readDone
		<-client.stderrDone
		_ = command.Wait()
		client.failPending(errors.New("MCP server process exited"))
		close(client.waitDone)
	}()

	timeout := cfg.StartupTimeout
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Name returns the configured server name.
func (c *Client) Name() string {
	if c == nil {
		return ""
	}
	return c.name
}

// Instructions returns the server-provided usage instructions, if any.
func (c *Client) Instructions() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.instructions
}

func (c *Client) initialize(ctx context.Context) error {
	c.modern = true
	var discovery struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	discoverRaw, discoverErr := c.request(ctx, "server/discover", map[string]any{})
	if discoverErr == nil {
		if err := json.Unmarshal(discoverRaw, &discovery); err != nil {
			return fmt.Errorf("decode MCP server discovery for %q: %w", c.name, err)
		}
		supported := false
		for _, version := range discovery.SupportedVersions {
			if version == ModernProtocolVersion {
				supported = true
				break
			}
		}
		if !supported {
			return fmt.Errorf("MCP server %q does not support protocol %s", c.name, ModernProtocolVersion)
		}
		return nil
	}
	var rpcErr *RPCError
	if !errors.As(discoverErr, &rpcErr) || rpcErr.Code != -32601 {
		return fmt.Errorf("discover MCP server %q: %w%s", c.name, discoverErr, c.stderrSuffix())
	}
	c.modern = false
	var initializeResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		Instructions    string `json:"instructions"`
	}
	initializeRaw, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": ClientName, "version": ClientVersion},
	})
	if err != nil {
		return fmt.Errorf("initialize MCP server %q: %w%s", c.name, err, c.stderrSuffix())
	}
	if err := json.Unmarshal(initializeRaw, &initializeResult); err != nil {
		return fmt.Errorf("decode MCP initialize result for %q: %w", c.name, err)
	}
	if initializeResult.ProtocolVersion == "" {
		return fmt.Errorf("MCP server %q did not return a protocol version%s", c.name, c.stderrSuffix())
	}
	c.mu.Lock()
	c.instructions = initializeResult.Instructions
	c.mu.Unlock()
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("finish MCP initialization for %q: %w", c.name, err)
	}
	return nil
}

// Tools lists the tools exposed by the server.
func (c *Client) Tools(ctx context.Context) ([]ToolDescriptor, error) {
	var all []ToolDescriptor
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var response struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		raw, err := c.request(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("list MCP tools for %q: %w%s", c.name, err, c.stderrSuffix())
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, fmt.Errorf("decode MCP tools/list result for %q: %w", c.name, err)
		}
		for _, remote := range response.Tools {
			if strings.TrimSpace(remote.Name) == "" {
				continue
			}
			if !json.Valid(remote.InputSchema) {
				remote.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			all = append(all, ToolDescriptor{Name: remote.Name, Description: remote.Description, InputSchema: remote.InputSchema})
		}
		if response.NextCursor == "" {
			break
		}
		cursor = response.NextCursor
	}
	return all, nil
}

// CallTool invokes one server tool and returns its textual output.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	var response struct {
		Content           []json.RawMessage `json:"content"`
		IsError           bool              `json:"isError"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
	}
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", fmt.Errorf("MCP tool %q: %w%s", name, err, c.stderrSuffix())
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode MCP tools/call result for %q: %w", name, err)
	}
	var output []string
	for _, raw := range response.Content {
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &content) == nil && content.Type == "text" {
			output = append(output, content.Text)
		} else {
			output = append(output, string(raw))
		}
	}
	if len(response.StructuredContent) > 0 {
		output = append(output, string(response.StructuredContent))
	}
	text := strings.Join(output, "\n")
	if response.IsError {
		if text == "" {
			return text, errors.New("MCP tool returned isError=true")
		}
		return text, fmt.Errorf("MCP tool returned isError=true: %s", text)
	}
	return text, nil
}

func (c *Client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil || c.pending == nil || c.stdin == nil {
		return nil, errors.New("MCP client is not connected")
	}
	id := c.requestID.Add(1)
	idJSON, _ := json.Marshal(id)
	key := string(idJSON)
	responseCh := make(chan outcome, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("MCP connection is closed")
	}
	c.pending[key] = responseCh
	c.mu.Unlock()
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": c.withEnvelope(params)}
	data, err := json.Marshal(request)
	if err != nil {
		c.removePending(key)
		return nil, err
	}
	c.writeMu.Lock()
	writeErr := writeFrame(c.stdin, data)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(key)
		return nil, fmt.Errorf("write MCP request: %w", writeErr)
	}
	select {
	case result := <-responseCh:
		if result.err != nil {
			return nil, result.err
		}
		return result.result, nil
	case <-ctx.Done():
		c.removePending(key)
		if !c.modern {
			_ = c.notify("notifications/cancelled", map[string]any{"requestId": id, "reason": ctx.Err().Error()})
		}
		return nil, ctx.Err()
	}
}

func (c *Client) notify(method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": c.withEnvelope(params)})
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errors.New("MCP connection is closed")
	}
	return writeFrame(c.stdin, data)
}

func writeFrame(writer io.Writer, data []byte) error {
	frame := append(data, '\n')
	for len(frame) > 0 {
		written, err := writer.Write(frame)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		frame = frame[written:]
	}
	return nil
}

func (c *Client) withEnvelope(params any) any {
	if !c.modern {
		return params
	}
	mapped := map[string]any{}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err == nil {
			_ = json.Unmarshal(encoded, &mapped)
		}
	}
	if mapped == nil {
		mapped = make(map[string]any)
	}
	// The envelope must be a JSON object. Building it as a map keeps it from
	// being encoded as a base64 string, which MCP servers reject.
	mapped["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ModernProtocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo":         map[string]string{"name": ClientName, "version": ClientVersion},
	}
	return mapped
}

func (c *Client) readResponses(stdout io.Reader) {
	defer close(c.readDone)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 32*1024), MaxFrameBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if len(envelope.ID) == 0 {
			continue
		}
		key := string(bytes.TrimSpace(envelope.ID))
		c.mu.Lock()
		responseCh := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if responseCh == nil {
			continue
		}
		result := outcome{result: envelope.Result}
		if envelope.Error != nil {
			result.err = &RPCError{Code: envelope.Error.Code, Message: envelope.Error.Message}
		} else if len(envelope.Result) == 0 {
			result.err = errors.New("JSON-RPC response omitted result")
		}
		responseCh <- result
	}
	if err := scanner.Err(); err != nil {
		c.failPending(fmt.Errorf("read MCP server output: %w", err))
	} else {
		c.failPending(errors.New("MCP server closed stdout"))
	}
}

// RPCError is a JSON-RPC error returned by an MCP server.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("JSON-RPC %d: %s", e.Code, e.Message)
}

func (c *Client) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[string]chan outcome)
	c.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- outcome{err: err}
	}
}

// Close shuts the server process down and waits for it to exit.
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		_ = c.stdin.Close()
		if c.command.Process != nil {
			_ = c.command.Process.Kill()
		}
		select {
		case <-c.waitDone:
		case <-time.After(3 * time.Second):
			closeErr = errors.New("timed out waiting for MCP server shutdown")
		}
	})
	return closeErr
}

func (c *Client) stderrSuffix() string {
	if c == nil || c.stderr == nil {
		return ""
	}
	if stderr := c.stderr.String(); stderr != "" {
		return "; server stderr: " + stderr
	}
	return ""
}

// ReplaceEnv returns the environment with the given overrides applied. An
// override with an empty value removes the variable. Names are compared
// case-insensitively and the result keeps a stable order.
func ReplaceEnv(environment []string, values map[string]string) []string {
	result := make([]string, 0, len(environment)+len(values))
	replacementNames := make([]string, 0, len(values))
	for name := range values {
		replacementNames = append(replacementNames, name)
	}
	sort.Strings(replacementNames)
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			result = append(result, entry)
			continue
		}
		replaced := false
		for _, replacementName := range replacementNames {
			if strings.EqualFold(name, replacementName) {
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		result = append(result, entry)
	}
	for _, name := range replacementNames {
		result = append(result, name+"="+values[name])
	}
	return result
}

// ToolName returns the namespaced name an MCP tool is exposed under.
func ToolName(server, tool string) string {
	safeServer := safePart(server)
	safeTool := safePart(tool)
	name := "mcp__" + safeServer + "__" + safeTool
	if len(name) <= 64 && safeServer == server && safeTool == tool {
		return name
	}
	hash := sha256.Sum256([]byte(server + "\x00" + tool))
	suffix := "_" + hex.EncodeToString(hash[:4])
	if len(name)+len(suffix) > 64 {
		name = name[:64-len(suffix)]
	}
	return name + suffix
}

func safePart(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	if result.Len() == 0 {
		return "server"
	}
	return result.String()
}
