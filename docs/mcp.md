# MCP server for AI agents

`bield mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server. It gives an AI agent typed tools for Bield, so the agent can see what it can do without reading `--help`, and its client can ask you before a tool that changes something.

An agent that can run shell commands can already use [the command line](cli.md). Use MCP when you want the agent to have Bield's tools and no shell.

The server runs next to the agent, over stdio. The agent's client starts it as a subprocess. It speaks MCP `2026-07-28`, the newest revision, and clients that only know an older one (back to `2024-11-05`) can still connect.

## Set up

The machine running the agent has to be on the tailnet, with `bield` installed on it. Tell `bield` where Bield is, once:

```sh
bield connect https://bield.example.ts.net
```

Then add the server to your client. These commands add it read-only.

**Claude Code**

```sh
claude mcp add bield -- bield mcp
```

**Codex**

```sh
codex mcp add bield -- bield mcp
```

**Cursor**: in `.cursor/mcp.json` or `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "bield": { "command": "bield", "args": ["mcp"] }
  }
}
```

To let the agent make changes, add `--allow-changes` after `mcp`. The server takes `--url` and `--timeout` as every command does, and reads `BIELD_URL`. `--timeout` applies to each tool call.

## Who the agent is

The agent acts as the Tailscale user logged in on its machine, the same as the command line, so Bield's roles apply as they do in the browser. There is nothing to log in to. A viewer who runs `bield mcp --allow-changes` sees the admin tools, and Bield refuses them when they are called.

A machine with a tag in Tailscale belongs to no one, so Bield doesn't know who is asking and refuses. So does Bield's own machine. There, give the agent an [API token](cli.md#api-tokens): set `BIELD_TOKEN` in the server's environment in the agent's MCP config, or save one with `bield connect <url> --with-token`. The agent then acts as the token, with its role, and never with more than the admin who made it. A token is also the way to give an agent on your own laptop less than your role: a viewer token keeps it read-only whatever `--allow-changes` says. Making a token isn't a tool, since an agent shouldn't hand itself credentials.

## What read-only means

By default the server has only the tools a viewer could use: looking at devices, checks, alerts, events and Docker. The other tools aren't hidden behind a prompt. They aren't there, and a client that calls one by name gets an error.

`--allow-changes` adds every tool that needs an admin. That includes some that only read, because what they read is admin-only: `docker_logs`, `files_ls`, `files_cat`, `settings` and `access`. Logs and config files often hold secrets, so an agent that only needs to look doesn't get them.

Each tool says what it does in its annotations:

| Annotation | Set on |
| --- | --- |
| `readOnlyHint` | Tools that change nothing |
| `destructiveHint` | Tools that stop, delete or overwrite something: `docker_stop`, `docker_restart`, `checks_delete`, `files_write`, `access_set`, `access_remove` |

The command line needs `--yes` for the destructive commands. Tools have no such argument: `--allow-changes` is the first check, and your client's approval prompt is the second. Annotations are advice to the client, and what it does with them is up to it, so check what yours asks before it runs a tool. Leave `--allow-changes` off unless you want the agent to make changes.

## The tools

The tools are made from the same table as the command line, so a new command becomes a tool. A tool's name is the command's with underscores: `bield alerts ack` is `alerts_ack`, and `bield access who-can-view` is `access_who_can_view`. Arguments are named as in the usage, and flags in camelCase: `--alert-offline` is `alertOffline`.

| Read-only | Added by `--allow-changes` |
| --- | --- |
| `whoami`, `status` | `enroll`, `settings`, `settings_set` |
| `devices`, `devices_show`, `devices_history`, `devices_usage` | `devices_set` |
| `alerts`, `alerts_show` | `alerts_ack`, `alerts_unack`, `alerts_snooze`, `alerts_wake`, `alerts_note` |
| `events` | `access`, `access_set`, `access_remove`, `access_who_can_view` |
| `checks`, `checks_show`, `checks_history` | `checks_run`, `checks_add`, `checks_pause`, `checks_resume`, `checks_delete` |
| `docker_ps`, `docker_images`, `docker_volumes`, `docker_networks` | `docker_start`, `docker_stop`, `docker_restart`, `docker_logs` |
| | `files_ls`, `files_cat`, `files_write` |

These commands aren't tools:

| Command | Why not |
| --- | --- |
| `bield connect` | It saves an address on the machine, once, when you set up. |
| `bield completion` | It prints a script for a person's shell. |
| `bield files get` | It saves to a path on the machine running the server. `files_cat` returns a text file's contents. |
| `bield files put` | It would let a tool call send any local file to a device. `files_write` takes the text it writes. |
| `--follow` | A tool call returns once, so `alerts`, `events` and `docker_logs` don't follow. |

A result is the JSON that the same command prints with `--json`, with the API's field names. `files_cat` returns the file's text and `docker_logs` returns one JSON object per line. A result can be up to 2 MiB. Above that, or for a file that isn't text, the tool fails and says so.

## When a call fails

A failure is a tool error with the same JSON as a command's `--json` error, so the model can read it and act:

```json
{"error":"only Bield's admins can do this","hint":"you are signed in as bob@example.com, a viewer; this needs an admin","status":403,"code":4}
```

`code` is the [exit code](cli.md) the command would have ended with. The hint says when the machine is tagged, when the user has no role, and when there's a viewer asking for something only an admin can do.

## Who did it

A change made through MCP is in the activity log with the person's login, and says it came from MCP: *Acknowledged: Disk 90% full on NAS, by alice@example.com with MCP*. The server says so in its `User-Agent`, `bield-mcp/<version>`. As with the command line, that only labels the line: who did it is Tailscale's answer.

## What has been run

- Claude Code 2.1.322 with Haiku, connected to `bield mcp` at protocol `2026-07-28`. Against [demo mode](demo.md), read-only, it listed the devices, found the failing check and read its history with no shell command, and saw only read-only tools. With `--allow-changes` it acknowledged an alert, and the event said `via: mcp`.
- The same agent, read-only, against a real Bield on a real tailnet, as a real Tailscale user.
- Codex and Cursor have not been run against it. The setup above follows their documented config.
- Tools that need a real machine (`docker_*` and `files_*`) are tested against a stub server. They haven't been called by an agent.

## What it doesn't do

- **Running a command on a device.** That's a decision that needs its own approval step and its own record.
- **A remote MCP endpoint** served by Bield over HTTP. `bield mcp` runs next to the agent.
