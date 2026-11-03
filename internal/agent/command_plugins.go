package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CommandPlugin exposes a user-configured executable as a single tool. Plugins
// are trusted host programs and run with the user's permissions in the task
// workspace; they are not sandboxed by this adapter.
type CommandPlugin struct {
	Name              string
	Description       string
	InputSchema       json.RawMessage
	Command           string
	Args              []string
	ModifiesWorkspace bool
}

const pluginOutputLimit = 4 * 1024 * 1024
const pluginStderrLimit = 8 * 1024

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
	cut    bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buffer.Write(p[:remaining])
			b.cut = true
		} else {
			_, _ = b.buffer.Write(p)
		}
	} else if len(p) > 0 {
		b.cut = true
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	value := b.buffer.String()
	if b.cut {
		value += "\n[plugin output truncated at configured safety limit]"
	}
	return value
}

func CommandPluginTools(workspace string, plugins []CommandPlugin) []Tool {
	tools := make([]Tool, 0, len(plugins))
	for _, plugin := range plugins {
		plugin := plugin
		tools = append(tools, Tool{
			Definition:        ToolDefinition{Name: plugin.Name, Description: plugin.Description, InputSchema: append(json.RawMessage(nil), plugin.InputSchema...)},
			ExecutionMode:     "serialized",
			ModifiesWorkspace: plugin.ModifiesWorkspace,
			Validate: func(raw json.RawMessage) error {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(raw, &object); err != nil {
					return err
				}
				if object == nil {
					return errors.New("plugin input must be a JSON object")
				}
				return nil
			},
			Handle: func(ctx context.Context, raw json.RawMessage) (string, error) {
				command := exec.CommandContext(ctx, plugin.Command, plugin.Args...)
				command.Dir = workspace
				command.Stdin = bytes.NewReader(raw)
				stdout := &boundedBuffer{limit: pluginOutputLimit}
				stderr := &boundedBuffer{limit: pluginStderrLimit}
				command.Stdout, command.Stderr = stdout, stderr
				err := command.Run()
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				output := strings.TrimSpace(stdout.String())
				if err != nil {
					message := strings.TrimSpace(stderr.String())
					if message != "" {
						return output, fmt.Errorf("plugin %q failed: %w: %s", plugin.Name, err, message)
					}
					return output, fmt.Errorf("plugin %q failed: %w", plugin.Name, err)
				}
				if output == "" {
					return "", nil
				}
				return output, nil
			},
		})
	}
	return tools
}

var _ io.Writer = (*boundedBuffer)(nil)
