package agent

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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const mcpProtocolVersion = "2025-11-25"
const mcpModernProtocolVersion = "2026-07-28"
const maxMCPFrameBytes = 4 * 1024 * 1024
const githubMCPImage = "ghcr.io/github/github-mcp-server@sha256:250bc6ce70788207608ef74cef4397230ff2e8481218da74d6093e4bd9101132"

// StartGitHubMCP starts the pinned official GitHub MCP server in Docker with
// only an explicit read-tool allowlist enabled. Credentials come from the environment
// first, then from .secrets/github-pat in the current working directory.
func StartGitHubMCP(ctx context.Context) (*GitHubMCP, error) {
	return StartGitHubMCPWithWrite(ctx, GitHubWriteOptions{})
}

// GitHubWriteOptions describes an explicitly authorized write scope. The zero
// value keeps the GitHub MCP server strictly read-only, which is the default for
// every run that does not opt in to GitHub write access.
type GitHubWriteOptions struct {
	// Repository is the checkout origin repository, in "owner/name" form. A
	// pull request can only be created against this repository.
	Repository string
	// HeadBranch is the feature branch the pull request head may point at. A
	// pull request can only be created for this branch.
	HeadBranch string
	// BaseBranch is the branch a pull request is opened against.
	BaseBranch string
}

// Enabled reports whether write access was explicitly authorized.
func (o GitHubWriteOptions) Enabled() bool {
	return strings.TrimSpace(o.Repository) != "" && strings.TrimSpace(o.HeadBranch) != ""
}

// StartGitHubMCPWithWrite starts the pinned official GitHub MCP server. Without
// write options the server runs read-only, exactly as StartGitHubMCP does. With
// write options the server additionally allows only create_pull_request.
func StartGitHubMCPWithWrite(ctx context.Context, write GitHubWriteOptions) (*GitHubMCP, error) {
	token := strings.TrimSpace(os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"))
	if token == "" {
		data, err := os.ReadFile(filepath.Join(".secrets", "github-pat"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("set GITHUB_PERSONAL_ACCESS_TOKEN or save the token in .secrets/github-pat")
			}
			return nil, fmt.Errorf("read local GitHub token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return nil, errors.New("GitHub token is empty; set GITHUB_PERSONAL_ACCESS_TOKEN or fill .secrets/github-pat")
	}
	return startGitHubMCP(ctx, token, write)
}

// GitHubMCPConfigured reports whether a non-empty GitHub token is available
// from the environment or project-local secret file.
func GitHubMCPConfigured() bool {
	if strings.TrimSpace(os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")) != "" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(".secrets", "github-pat"))
	return err == nil && strings.TrimSpace(string(data)) != ""
}

type mcpOutcome struct {
	result json.RawMessage
	err    error
}

// GitHubMCP owns the Docker-backed stdio connection to GitHub's read-only MCP server.
type GitHubMCP struct {
	command *exec.Cmd
	stdin   io.WriteCloser

	writeMu    sync.Mutex
	mu         sync.Mutex
	pending    map[string]chan mcpOutcome
	closed     bool
	modern     bool
	closeOnce  sync.Once
	requestID  atomic.Uint64
	readDone   chan struct{}
	stderrDone chan struct{}
	waitDone   chan struct{}
	stderr     *tailBuffer
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

func startGitHubMCP(ctx context.Context, token string, write GitHubWriteOptions) (*GitHubMCP, error) {
	tools := githubReadTools
	readOnly := "1"
	if write.Enabled() {
		// The MCP server receives an exact tool allowlist. It can read repository
		// context and create one PR, but has no merge, issue-write, or other write
		// tool registered.
		tools += ",create_pull_request"
		readOnly = "0"
	}
	command := exec.Command("docker", "run", "--rm", "-i",
		"-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
		"-e", "GITHUB_READ_ONLY",
		"-e", "GITHUB_TOOLS",
		githubMCPImage,
	)
	command.Env = replaceEnv(os.Environ(), map[string]string{
		"GITHUB_PERSONAL_ACCESS_TOKEN": token,
		"GITHUB_READ_ONLY":             readOnly,
		"GITHUB_TOOLS":                 tools,
	})
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open MCP stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open MCP stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open MCP stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start GitHub MCP server: %w", err)
	}
	client := &GitHubMCP{
		command: command, stdin: stdin, pending: make(map[string]chan mcpOutcome),
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

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client.modern = true
	var discovery struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	discoverErr := client.request(initCtx, "server/discover", map[string]any{}, &discovery)
	if discoverErr == nil {
		supported := false
		for _, version := range discovery.SupportedVersions {
			if version == mcpModernProtocolVersion {
				supported = true
				break
			}
		}
		if !supported {
			_ = client.Close()
			return nil, fmt.Errorf("GitHub MCP server does not support protocol %s", mcpModernProtocolVersion)
		}
	} else {
		var rpcErr *mcpRPCError
		if !errors.As(discoverErr, &rpcErr) || rpcErr.Code != -32601 {
			_ = client.Close()
			return nil, fmt.Errorf("discover GitHub MCP server: %w%s", discoverErr, client.stderrSuffix())
		}
		client.modern = false
		var initializeResult struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := client.request(initCtx, "initialize", map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "sigmaagent-go", "version": "0.1.0"},
		}, &initializeResult); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("initialize GitHub MCP server: %w%s", err, client.stderrSuffix())
		}
		if initializeResult.ProtocolVersion == "" {
			_ = client.Close()
			return nil, fmt.Errorf("GitHub MCP server did not return a protocol version%s", client.stderrSuffix())
		}
		if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("finish GitHub MCP initialization: %w", err)
		}
	}
	return client, nil
}

func replaceEnv(environment []string, values map[string]string) []string {
	result := make([]string, 0, len(environment)+len(values))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			result = append(result, entry)
			continue
		}
		replaced := false
		for replacementName := range values {
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
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func (c *GitHubMCP) Tools(ctx context.Context) ([]Tool, error) {
	var all []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
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
		if err := c.request(ctx, "tools/list", params, &response); err != nil {
			return nil, fmt.Errorf("list GitHub MCP tools: %w%s", err, c.stderrSuffix())
		}
		all = append(all, response.Tools...)
		if response.NextCursor == "" {
			break
		}
		cursor = response.NextCursor
	}
	tools := make([]Tool, 0, len(all))
	for _, remote := range all {
		if strings.TrimSpace(remote.Name) == "" {
			continue
		}
		// Defense in depth: only the explicit read allowlist is exposed to the
		// model. The write tool is wrapped separately with a fixed repo/branch.
		if !isGitHubReadTool(remote.Name) {
			continue
		}
		if !json.Valid(remote.InputSchema) {
			remote.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		toolName := mcpToolName("github", remote.Name)
		definition := ToolDefinition{Name: toolName, Description: fmt.Sprintf("GitHub MCP tool %q: %s", remote.Name, remote.Description), InputSchema: remote.InputSchema}
		remoteName := remote.Name
		tools = append(tools, Tool{
			Definition:    definition,
			ExecutionMode: "sequential",
			Handle: func(callCtx context.Context, arguments json.RawMessage) (string, error) {
				return c.callTool(callCtx, remoteName, arguments)
			},
		})
	}
	return tools, nil
}

// CreatePullRequest opens a PR after the runtime has pushed the authorized
// feature branch. Keeping this as a runtime action guarantees the branch exists
// before GitHub receives the request.
func (c *GitHubMCP) CreatePullRequest(ctx context.Context, write GitHubWriteOptions, title, body string) (string, error) {
	arguments, err := json.Marshal(map[string]string{"title": title, "body": body})
	if err != nil {
		return "", fmt.Errorf("encode pull request details: %w", err)
	}
	return c.createPullRequest(ctx, write, arguments)
}

// createPullRequest validates the request against the authorized write scope
// before forwarding it to the GitHub MCP server.
func (c *GitHubMCP) createPullRequest(ctx context.Context, write GitHubWriteOptions, arguments json.RawMessage) (string, error) {
	if !write.Enabled() {
		return "", errors.New("GitHub write access is not enabled for this run")
	}
	var args struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", fmt.Errorf("decode create_pull_request arguments: %w", err)
	}
	args.Title = strings.TrimSpace(args.Title)
	if args.Title == "" {
		return "", errors.New("create_pull_request requires a non-empty title")
	}
	if len(args.Title) > 256 {
		return "", errors.New("create_pull_request title must be at most 256 characters")
	}
	if len(args.Body) > 65536 {
		return "", errors.New("create_pull_request body must be at most 65536 characters")
	}
	request := createPullRequestRequest(write, args.Title, args.Body)
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode create_pull_request request: %w", err)
	}
	return c.callTool(ctx, "create_pull_request", json.RawMessage(payload))
}

func createPullRequestRequest(write GitHubWriteOptions, title, body string) map[string]any {
	base := strings.TrimSpace(write.BaseBranch)
	if base == "" {
		base = "main"
	}
	return map[string]any{
		"owner": repositoryOwner(write.Repository),
		"repo":  repositoryName(write.Repository),
		"title": strings.TrimSpace(title),
		"body":  body,
		"head":  strings.TrimSpace(write.HeadBranch),
		"base":  base,
	}
}

const githubReadTools = "get_file_contents,issue_read,list_issues,search_issues,pull_request_read,list_pull_requests,search_pull_requests,search_code,search_repositories,get_commit,list_commits,list_branches"

func isGitHubReadTool(name string) bool {
	for _, allowed := range strings.Split(githubReadTools, ",") {
		if name == allowed {
			return true
		}
	}
	return false
}

// repositoryOwner returns the owner segment of an "owner/name" repository.
func repositoryOwner(repository string) string {
	owner, _, _ := strings.Cut(strings.TrimSpace(repository), "/")
	return owner
}

// repositoryName returns the name segment of an "owner/name" repository.
func repositoryName(repository string) string {
	_, name, found := strings.Cut(strings.TrimSpace(repository), "/")
	if !found {
		return strings.TrimSpace(repository)
	}
	return name
}

func (c *GitHubMCP) callTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	var response struct {
		Content           []json.RawMessage `json:"content"`
		IsError           bool              `json:"isError"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
	}
	if err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &response); err != nil {
		return "", fmt.Errorf("MCP tool %q: %w%s", name, err, c.stderrSuffix())
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
		return text, errors.New("MCP tool returned isError=true")
	}
	return text, nil
}

func (c *GitHubMCP) request(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	id := c.requestID.Add(1)
	idJSON, _ := json.Marshal(id)
	key := string(idJSON)
	responseCh := make(chan mcpOutcome, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("MCP connection is closed")
	}
	c.pending[key] = responseCh
	c.mu.Unlock()
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": c.withEnvelope(params)}
	data, err := json.Marshal(request)
	if err != nil {
		c.removePending(key)
		return err
	}
	c.writeMu.Lock()
	writeErr := writeMCPFrame(c.stdin, data)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(key)
		return fmt.Errorf("write MCP request: %w", writeErr)
	}
	select {
	case outcome := <-responseCh:
		if outcome.err != nil {
			return outcome.err
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(outcome.result, result); err != nil {
			return fmt.Errorf("decode MCP %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		if !c.modern {
			_ = c.notify("notifications/cancelled", map[string]any{"requestId": id, "reason": ctx.Err().Error()})
		}
		return ctx.Err()
	}
}

func (c *GitHubMCP) notify(method string, params any) error {
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
	return writeMCPFrame(c.stdin, data)
}

func writeMCPFrame(writer io.Writer, data []byte) error {
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

func (c *GitHubMCP) withEnvelope(params any) any {
	if !c.modern {
		return params
	}
	var mapped map[string]json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err == nil {
			_ = json.Unmarshal(encoded, &mapped)
		}
	}
	if mapped == nil {
		mapped = make(map[string]json.RawMessage)
	}
	meta, _ := json.Marshal(map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcpModernProtocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "sigmaagent-go", "version": "0.1.0"},
	})
	mapped["_meta"] = meta
	return mapped
}

func (c *GitHubMCP) readResponses(stdout io.Reader) {
	defer close(c.readDone)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 32*1024), maxMCPFrameBytes)
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
		outcome := mcpOutcome{result: envelope.Result}
		if envelope.Error != nil {
			outcome.err = &mcpRPCError{Code: envelope.Error.Code, Message: envelope.Error.Message}
		} else if len(envelope.Result) == 0 {
			outcome.err = errors.New("JSON-RPC response omitted result")
		}
		responseCh <- outcome
	}
	if err := scanner.Err(); err != nil {
		c.failPending(fmt.Errorf("read MCP server output: %w", err))
	} else {
		c.failPending(errors.New("MCP server closed stdout"))
	}
}

type mcpRPCError struct {
	Code    int
	Message string
}

func (e *mcpRPCError) Error() string { return fmt.Sprintf("JSON-RPC %d: %s", e.Code, e.Message) }

func (c *GitHubMCP) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *GitHubMCP) failPending(err error) {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[string]chan mcpOutcome)
	c.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- mcpOutcome{err: err}
	}
}

func (c *GitHubMCP) Close() error {
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

func (c *GitHubMCP) stderrSuffix() string {
	if stderr := c.stderr.String(); stderr != "" {
		return "; server stderr: " + stderr
	}
	return ""
}

func safeToolPart(value string) string {
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

func mcpToolName(server, tool string) string {
	name := "mcp__" + safeToolPart(server) + "__" + safeToolPart(tool)
	if len(name) <= 64 {
		return name
	}
	hash := sha256.Sum256([]byte(server + "\x00" + tool))
	suffix := "_" + hex.EncodeToString(hash[:4])
	return name[:64-len(suffix)] + suffix
}
