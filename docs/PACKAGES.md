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
| `extensions` | Listed by `--install`; running extension code is not implemented yet |
| `prompts`, `themes` | Listed by `--install`; not wired into the runtime yet |

Skills contributed by a package are bounded by the package directory, so a
Skill can only read reference files that ship with its own package.

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
- Extension modules from a package are listed but not executed.
- The package registry records the source of each package; `--uninstall`
  removes the record and leaves the installed files on disk.
