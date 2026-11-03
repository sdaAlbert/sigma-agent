# Packages and MCP servers

Sigma Agent can install packages and connect to MCP servers. Packages
contribute Agent Skills and, through their MCP server, new tools.

## Install a package

```bash
# user scope: ~/.sigma-agent/packages/
sigma-agent --install npm:pi-web-access

# project scope: <repo>/.sigma-agent/packages/
sigma-agent --install npm:pi-web-access --local --repo .

sigma-agent --list-packages
sigma-agent --uninstall npm:pi-web-access
```

Supported sources:

| Source | Example | Notes |
| --- | --- | --- |
| npm | `npm:pi-web-access` | A version can be pinned: `npm:pi-web-access@0.46.0` |
| git | `git:github.com/owner/repo` | A ref can be pinned: `git:github.com/owner/repo@v1.2.0` |
| local | `./my-package` | Used in place, never copied |

Npm packages are installed with `npm install --prefix <scope>/packages/npm`, so
the package manager resolves the dependency tree. Git packages are cloned and
then get their dependencies installed. The install root is a dedicated npm
project, so the repository's own `package.json` is never touched.

## Package format

A package is an ordinary npm or git package. It declares its resources either
in the `pi` field of its `package.json`:

```json
{
  "name": "my-package",
  "pi": {
    "extensions": ["./dist"],
    "skills": ["./skills"]
  }
}
```

or through the conventional `extensions/`, `skills/`, `prompts/`, and `themes/`
directories. Manifest entries accept glob patterns and `!` exclusions.

| Resource | What Sigma Agent does with it |
| --- | --- |
| `skills` | `SKILL.md` files are discovered and loaded on demand with `read_skill`, the same way project Skills are |
| `extensions` | JavaScript modules are loaded into the workflow host; TypeScript must be compiled to JavaScript first |
| `prompts`, `themes` | Listed by `--install`; not wired into the runtime yet |

Skills contributed by a package are bounded by the package directory, so a
Skill can only read reference files that ship with its own package.

## Workflow extensions

Sigma loads `.js`, `.mjs`, and `.cjs` files discovered under each installed
package's `extensions/` directory (or the `pi.extensions` manifest paths) at
the start of each run. Slash commands are available in interactive sessions.
Node.js must be installed. Modules can export a default activation function or
a named `activate` function:

```js
export default (sigma) => {
  sigma.registerCommand("review", { description: "Review the current task" }, async (args, ui) => {
    ui.notify(`Review requested: ${args || "current changes"}`);
    if (await ui.confirm("Start the review workflow?")) {
      ui.notify("Review approved.");
    }
  });

  sigma.on("tool_complete", (event) => {
    if (event.status === "failed") sigma.notify(`${event.tool} failed`);
  });
};
```

Available lifecycle events are `session_start`, `turn_start`, `turn_end`,
`tool_complete`, and `session_end`. Their frozen payload contains only
`type`, `session_id`, `turn`, `tool`, `status`, and `error`. Commands are
invoked as `/review [arguments]`, appear in `/help`, and are available in TUI
command completion. Command names must be lowercase letters, digits, and
hyphens; Sigma's built-in command names are reserved. `ui.confirm` is currently
available in TUI mode; `notify` also works in the text interface.

Extensions can also register Agent tools with a JSON Schema and a handler that
returns text:

```js
sigma.registerTool(
  "issue_summary",
  {
    description: "Summarize a GitHub issue",
    inputSchema: {
      type: "object",
      properties: { issue: { type: "string" } },
      required: ["issue"],
      additionalProperties: false,
    },
  },
  async ({ issue }, ui) => `Issue requested: ${issue}`,
);
```

Registered tools go through Sigma's normal Agent tool policy and execution
loop. They run sequentially by default. Set `modifiesWorkspace: true` when a
handler changes the task worktree so Sigma applies its normal test and diff
review gate. Tool names in the `mcp__` namespace are reserved for MCP adapters.
The built-in GitHub MCP tools and configured `mcp.json` server tools are
registered through this same host tool path; GitHub's server startup and
repository/branch write restrictions remain enforced by Sigma.

Extensions run as the current user with the same operating-system permissions
as Sigma and can import packages installed alongside their module. Install and
run only extensions you trust. Sigma does not expose mutable session objects,
tool registration, or custom TUI rendering through this first workflow host.

## Configure an MCP server

MCP servers are configured in `mcp.json`. The file uses the `mcpServers` shape
shared by other MCP clients, so an existing configuration can be reused as is.

```json
{
  "mcpServers": {
    "web": {
      "command": "node",
      "args": ["./dist/mcp-cli.js"],
      "package": "pi-web-access"
    },
    "docs": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]
    }
  }
}
```

| Field | Purpose |
| --- | --- |
| `command` | The executable that starts the server |
| `args` | Arguments passed to the command |
| `env` | Environment overrides for the server process |
| `cwd` | Working directory, resolved against the configuration file |
| `package` | Name of an installed package the server is started from |
| `enabled` | `false` skips the server without deleting it |

Sigma Agent reads `~/.sigma-agent/mcp.json` for the user scope and
`<repo>/.sigma-agent/mcp.json` for the project scope. A project entry replaces
the user entry with the same server name. The Pi Agent location
`<repo>/.pi/mcp.json` is read as well, so a Pi configuration can be reused
unchanged.

When a server names a `package`, the server process starts with the package
directory as its working directory and `./`-prefixed arguments are resolved
against the package root. That is how a package that ships an MCP server is
wired up without hardcoding an install path:

```bash
sigma-agent --install npm:pi-web-access
```

```json
{
  "mcpServers": {
    "web": {
      "command": "node",
      "args": ["./dist/mcp-cli.js"],
      "package": "pi-web-access"
    }
  }
}
```

## Check the connection

```bash
sigma-agent --list-mcp
```

The command connects to every enabled server, prints the tools each one
contributes, and disconnects again.

## Tool names and safety

Server tools are exposed as `mcp__<server>__<tool>`, so several servers can
contribute tools without colliding. They run in sequence, because a remote
tool usually reaches the network and may change remote state.

An MCP server is a program that runs with the permissions of the current
user. Sigma Agent does not sandbox it. The same applies to an installed
package: installing one runs the package manager's install scripts, so only
install packages you trust. Package-provided tools still pass through the
configured tool policy, so a tool can be denied with `tool_permissions`.

## Current limits

- Only stdio MCP servers are supported. HTTP servers and OAuth sign-in are not.
- Workflow extensions require Node.js and run with Sigma's operating-system permissions. TypeScript sources must be compiled to JavaScript; custom TUI rendering and mutable session APIs are not exposed.
- The package registry records the source of each package; `--uninstall`
  removes the record and leaves the installed files on disk.
