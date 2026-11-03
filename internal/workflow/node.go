package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type wireMessage struct {
	Type        string          `json:"type"`
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Event       string          `json:"event,omitempty"`
	Args        string          `json:"args,omitempty"`
	Message     string          `json:"message,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	OK          bool            `json:"ok,omitempty"`
	Error       string          `json:"error,omitempty"`
	Payload     Event           `json:"payload,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	ExecutionMode string        `json:"execution_mode,omitempty"`
	ModifiesWorkspace bool      `json:"modifies_workspace,omitempty"`
}

// LoadJavaScript starts a Node.js host for compiled JavaScript extension
// modules. TypeScript must be compiled to JavaScript by the package author.
func LoadJavaScript(ctx context.Context, paths []string, handler Handler) (*Host, func() error, error) {
	if len(paths) == 0 {
		return NewHost(handler), func() error { return nil }, nil
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, errors.New("workflow extensions were found, but Node.js is not installed")
	}
	absolute := make([]string, 0, len(paths))
	for _, path := range paths {
		resolved, err := filepath.Abs(path)
		if err != nil {
			return nil, nil, err
		}
		if strings.EqualFold(filepath.Ext(path), ".ts") || strings.EqualFold(filepath.Ext(path), ".tsx") {
			continue
		}
		absolute = append(absolute, resolved)
	}
	if len(absolute) == 0 {
		return NewHost(handler), func() error { return nil }, nil
	}
	command := exec.CommandContext(ctx, node, "--input-type=module", "-e", nodeBootstrap)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	command.Stderr = os.Stderr
	command.Args = append(command.Args, absolute...)
	if err := command.Start(); err != nil {
		return nil, nil, fmt.Errorf("start workflow extension host: %w", err)
	}
	process := &nodeHost{stdin: stdin, output: make(chan wireMessage, 32), done: make(chan struct{})}
	go process.read(stdout)
	api := NewHost(handler)
	process.api = api
	initializeCtx, cancelInitialize := context.WithTimeout(ctx, 10*time.Second)
	defer cancelInitialize()
	if err := process.initialize(initializeCtx); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, nil, err
	}
	process.command = command
	return api, process.Close, nil
}

type nodeHost struct {
	mu      sync.Mutex
	stdin   io.WriteCloser
	output  chan wireMessage
	done    chan struct{}
	errMu   sync.Mutex
	err     error
	command *exec.Cmd
	api     *Host
	seq     uint64
}

func (p *nodeHost) read(reader io.Reader) {
	defer close(p.done)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var message wireMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			p.setError(fmt.Errorf("workflow extension host wrote invalid protocol data: %w", err))
			return
		}
		p.output <- message
	}
	if err := scanner.Err(); err != nil {
		p.setError(err)
	} else {
		p.setError(io.EOF)
	}
}

func (p *nodeHost) setError(err error) { p.errMu.Lock(); p.err = err; p.errMu.Unlock() }

func (p *nodeHost) terminalError() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *nodeHost) initialize(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return fmt.Errorf("initialize workflow extensions: %w", p.terminalError())
		case message := <-p.output:
			switch message.Type {
			case "ready":
				return nil
			case "register_command":
				name, description := message.Name, message.Description
				if err := p.api.RegisterCommand(Command{Name: name, Description: description}, func(ctx context.Context, args string) error {
					return p.request(ctx, wireMessage{Type: "command", Name: name, Args: args}, nil)
				}); err != nil {
					fmt.Fprintln(os.Stderr, "sigma-agent: warning: skip workflow command:", err)
				}
			case "register_tool":
				name := message.Name
				tool := Tool{
					Name: name, Description: message.Description, InputSchema: message.InputSchema,
					ExecutionMode: message.ExecutionMode, ModifiesWorkspace: message.ModifiesWorkspace,
				}
				if err := p.api.RegisterExtensionTool(tool, func(ctx context.Context, arguments json.RawMessage) (string, error) {
					var result json.RawMessage
					if err := p.request(ctx, wireMessage{Type: "tool", Name: name, Arguments: arguments}, &result); err != nil {
						return "", err
					}
					if len(result) == 0 || string(result) == "null" {
						return "", nil
					}
					var output string
					if err := json.Unmarshal(result, &output); err != nil {
						return "", fmt.Errorf("workflow tool %q must return text: %w", name, err)
					}
					return output, nil
				}); err != nil {
					fmt.Fprintln(os.Stderr, "sigma-agent: warning: skip workflow tool:", err)
				}
			case "subscribe":
				eventType := message.Event
				if err := p.api.On(eventType, func(event Event) {
					_ = p.request(context.Background(), wireMessage{Type: "event", Event: eventType, Payload: event}, nil)
				}); err != nil {
					fmt.Fprintln(os.Stderr, "sigma-agent: warning: skip workflow event:", err)
				}
			}
		}
	}
}

func (p *nodeHost) request(ctx context.Context, request wireMessage, result *json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	request.ID = fmt.Sprintf("%d", p.seq)
	if err := json.NewEncoder(p.stdin).Encode(request); err != nil {
		return fmt.Errorf("send workflow extension request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return fmt.Errorf("workflow extension host stopped: %w", p.terminalError())
		case message := <-p.output:
			switch message.Type {
			case "host_call":
				value, err := p.handleHostCall(ctx, message)
				response := wireMessage{Type: "host_result", ID: message.ID, OK: err == nil, Result: value}
				if err != nil {
					response.Error = err.Error()
				}
				_ = json.NewEncoder(p.stdin).Encode(response)
			case "result":
				if message.ID != request.ID {
					continue
				}
				if !message.OK {
					return errors.New(message.Error)
				}
				if result != nil {
					*result = message.Result
				}
				return nil
			}
		}
	}
}

func (p *nodeHost) handleHostCall(ctx context.Context, message wireMessage) (json.RawMessage, error) {
	switch message.Name {
	case "notify":
		p.api.Notify(message.Message)
		return json.RawMessage(`null`), nil
	case "confirm":
		confirmed, err := p.api.Confirm(ctx, message.Message)
		if err != nil {
			return nil, err
		}
		value, _ := json.Marshal(confirmed)
		return value, nil
	default:
		return nil, fmt.Errorf("unknown workflow host method %q", message.Name)
	}
}

func (p *nodeHost) Close() error {
	if p == nil || p.command == nil || p.command.Process == nil {
		return nil
	}
	_ = p.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- p.command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		_ = p.command.Process.Kill()
		return <-done
	}
}

const nodeBootstrap = `
import { createInterface } from 'node:readline';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
const send = (value) => process.stdout.write(JSON.stringify(value) + '\n');
for (const key of ['log','info','warn','error','debug']) console[key] = (...args) => process.stderr.write(args.join(' ') + '\n');
const commands = new Map(), tools = new Map(), events = new Map(), pending = new Map();
let seq = 0;
const hostCall = (name, message) => new Promise((resolve, reject) => {
  const id = 'host-' + (++seq); pending.set(id, {resolve, reject}); send({type:'host_call', id, name, message});
});
const api = {
  registerCommand(name, options, handler) {
    if (typeof options === 'function') { handler = options; options = {}; }
    if (typeof handler !== 'function') throw new Error('command handler is required');
    commands.set(name, handler); send({type:'register_command', name, description: options?.description || ''});
  },
  registerTool(name, options, handler) {
    if (typeof handler !== 'function') throw new Error('tool handler is required');
    const schema = options?.inputSchema || options?.input_schema;
    if (!schema || typeof schema !== 'object' || Array.isArray(schema)) throw new Error('tool inputSchema must be a JSON schema object');
    tools.set(name, handler);
    send({type:'register_tool', name, description: options?.description || '', input_schema:schema,
      execution_mode:options?.executionMode || 'sequential', modifies_workspace:!!options?.modifiesWorkspace});
  },
  on(event, handler) {
    if (typeof handler !== 'function') throw new Error('event handler is required');
    if (!events.has(event)) events.set(event, []); events.get(event).push(handler); send({type:'subscribe', event});
  },
  ui: { notify(message) { void hostCall('notify', String(message)); }, confirm(message) { return hostCall('confirm', String(message)); } },
  notify(message) { void hostCall('notify', String(message)); },
  confirm(message) { return hostCall('confirm', String(message)); },
};
const files = process.argv.slice(1);
for (const file of files) {
  try {
    const mod = await import(pathToFileURL(resolve(file)).href);
    const activate = typeof mod.default === 'function' ? mod.default : mod.activate;
    if (typeof activate === 'function') await activate(api);
  } catch (error) { process.stderr.write('extension load failed: ' + file + ': ' + error + '\n'); }
}
send({type:'ready'});
const rl = createInterface({input:process.stdin, crlfDelay:Infinity});
rl.on('line', async line => {
  let message; try { message = JSON.parse(line); } catch { return; }
  if (message.type === 'host_result') {
    const waiter = pending.get(message.id); if (!waiter) return; pending.delete(message.id);
    if (message.error) waiter.reject(new Error(message.error)); else waiter.resolve(message.result ?? true); return;
  }
  try {
    if (message.type === 'command') {
      const handler = commands.get(message.name); if (!handler) throw new Error('command handler not found');
      const result = await handler(message.args || '', api.ui); send({type:'result', id:message.id, ok:true, result});
    } else if (message.type === 'tool') {
      const handler = tools.get(message.name); if (!handler) throw new Error('tool handler not found');
      const result = await handler(JSON.parse(message.arguments || '{}'), api.ui);
      send({type:'result', id:message.id, ok:true, result:result === undefined ? '' : String(result)});
    } else if (message.type === 'event') {
      for (const handler of events.get(message.event) || []) await handler(Object.freeze({...message.payload}));
      send({type:'result', id:message.id, ok:true});
    }
  } catch (error) { send({type:'result', id:message.id, ok:false, error:String(error?.stack || error)}); }
});
`
