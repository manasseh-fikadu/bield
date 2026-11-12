# The command line

`bield` isn't only the server. The same file looks at a running Bield and changes things in it from a terminal: `bield devices`, `bield alerts ack 5`, `bield docker restart nas jellyfin --yes`. It's for people, and for scripts and AI agents such as Claude Code and Codex, which already run shell commands and so get Bield without a new protocol.

It has the roles the web app has. There is nothing to log in to: Bield asks Tailscale who is on the other end of the connection, so a command run on your laptop is you, with your role. A viewer can read, an admin can change things, and a command that needs more than you have says so and exits with code 4. A machine no person is logged in to, such as a tagged server or a CI runner, uses an [API token](#api-tokens) instead.

## Setting it up

Run it on a machine that is on the same tailnet as Bield and has Tailscale connected. A laptop is the usual one. Get `bield` from a [release archive](install.md#release-archives) (it is the same binary as the server) and put it on your `PATH`. Then tell it where Bield is:

```sh
bield connect https://bield.example.ts.net
```

That is the MagicDNS name of Bield's node, the same address you open in the browser. It checks that Bield answers, says who Bield thinks you are, and saves the address in `~/.config/bield/cli.json` (on macOS, `~/Library/Application Support/bield/cli.json`). Two things beat the saved address: the `BIELD_URL` environment variable, and `--url` on one command. `bield connect` with no address shows the one in use.

```sh
bield whoami
```

```
You are alice@example.com on laptop.example.ts.net, with the role admin
```

For shell completion, `bield completion bash` prints a script for bash (`source <(bield completion bash)`), `bield completion zsh` one for zsh (`source <(bield completion zsh)`), and `bield completion fish` one for fish (`bield completion fish | source`). Put the line in the shell's startup file. They've been tried in bash 5.2, zsh 5.9 and fish 3.6, and CI completes commands and flags in bash and fish on every change.

## What it does

| Command | What it does | Needs |
| --- | --- | --- |
| `bield status` | Health, how many devices are online and how many alerts are open | viewer |
| `bield devices` | The devices and how they're doing. `--status`, `--kind`, `--tag` and `--path` (direct, derp or peer-relay) pick some | viewer |
| `bield devices show <device>` | Everything Bield knows about one | viewer |
| `bield devices history <device>` | CPU, memory, network and latency over `--range` 1h, 6h, 24h or 7d | viewer |
| `bield devices usage <device>` | What uses a device's CPU and memory now | viewer |
| `bield devices set <device>` | Change a device's name, kind, notes, alerts or thresholds | admin |
| `bield relay` | Suggest a [peer relay](https://tailscale.com/kb/1591/peer-relays) for the devices Bield reaches through DERP, with the command to run on it and the grant for the policy file. `--via <device>` picks another machine, and `--port` the UDP port (40000) | viewer |
| `bield alerts` | The alerts that are open. `--resolved` lists the ones that cleared, `--follow` keeps going | viewer |
| `bield alerts show <id>` | An alert, with its notes | viewer |
| `bield alerts ack`, `unack`, `snooze`, `wake` and `note` `<id>` | The same as the buttons on the Alerts page | admin |
| `bield events` | The activity log: what happened and who did it. `--follow` keeps going | viewer |
| `bield checks` | The checks and whether they pass. `--failing` and `--group` pick some | viewer |
| `bield checks show`, `history <check>` | A check, and its uptime and latency over time | viewer |
| `bield checks run`, `pause`, `resume`, `delete <check>` | Run a check now, stop or restart it, or delete it | admin |
| `bield checks add` | Add a check on a URL, a TCP port or a host name | admin |
| `bield docker ps`, `images`, `volumes`, `networks` `[device]` | Docker on one device, or on every device that has it | viewer |
| `bield docker start`, `stop`, `restart <device> <container>` | Control a container | admin |
| `bield docker logs <device> <container>` | A container's logs. `--follow` keeps going | admin |
| `bield files ls`, `cat`, `get`, `put`, `write` | List, read, download, upload and replace files on a device | admin |
| `bield editor <device>` | Whether [VS Code](vscode.md) is installed and running on a device, and its address | admin |
| `bield editor install`, `stop`, `remove <device>` | Install or update code-server on a device and wait for it, stop it, or delete it. `remove --keep-data` keeps its settings and extensions | admin |
| `bield editor open <device> [folder]` | Start VS Code on a device and print its address, to open in a browser | admin, in person |
| `bield enroll` | Make an install command for the [agent](agent.md) | admin |
| `bield settings`, `settings set <name=value>...` | Show or change [the settings](settings.md) | admin |
| `bield access`, `access set`, `remove`, `who-can-view` | Who has a role in Bield | admin |
| `bield tokens`, `tokens create <name>`, `tokens revoke <token>` | List, make and revoke [API tokens](#api-tokens) | admin, in person |

A device is its name, its host name, its MagicDNS name, or its ID, in any case, and a container is its name or the start of its ID. A name that fits several devices is refused, with the list.

`bield help` lists every command, and `bield <command> -h` has its flags. Those two are made from the same table the commands run from, so they are the full list.

## In scripts and for AI agents

- **`--json`** prints what the API answers with, with its field names (the same ones as in `web/src/lib/api/types.ts`), instead of a table. The commands that follow output (`alerts --follow`, `events --follow`, `docker logs --follow`) print one JSON object per line. With `--json`, an error is a JSON object on stderr too: `{"error": "…", "hint": "…", "status": 403, "code": 4}`.
- **Exit codes:** 0 done, 1 failed, 2 wrong usage, 3 not found, 4 not allowed.
- **Nothing waits for input.** A command that stops, restarts, deletes or overwrites something, or gives out a role, does nothing without `--yes`, and says so at once with exit code 2. It never asks. Reads and the changes that can be undone, such as `alerts ack`, don't need it.
- Output goes to stdout, and notes for a person, such as the time an install command expires, to stderr. Colors show only on a terminal, and not with `--no-color` or `NO_COLOR`.
- Commands give up after `--timeout` (30 seconds). The ones that follow have none and stop with Ctrl-C.
- A change shows in the very next list, even a moment after you made it.

A refusal says why. From a tagged server, `bield alerts ack 5` fails with this, since Bield doesn't know who is asking, instead of a bare 403:

```
bield: only Bield's admins can do this, and Tailscale didn't say who you are
  This machine is tagged in Tailscale, so it belongs to no one and Bield doesn't know who you are; run bield from a machine you are logged in to
```

Bield's own machine is anonymous too, since any process there could send the request. So an agent on a tagged server, in CI, or on the machine Bield runs on can't use the commands that need a role, unless it has an [API token](#api-tokens). An admin's agent on their laptop acts with the admin's full role, which includes opening files and restarting containers on any device Bield can reach over SSH. Give it a viewer token, or tell it not to change anything, if that is too much: with `BIELD_TOKEN` set, the command line acts as the token and not as you.

## API tokens

An API token lets something that isn't a person at a laptop use Bield: a script on a tagged server, a CI job, or an AI agent that should have less access than you. An admin makes one with a name, a role and an expiry, in Settings, under API tokens, or from the command line:

```sh
bield tokens create ci-deploy --role viewer --days 90
```

The token, `bield_` and 26 letters and digits, goes alone to stdout, and this is the only time anyone sees it: Bield keeps only its hash. Hand it to the machine that needs it, either for one command or saved with the address:

```sh
BIELD_TOKEN=bield_… bield devices --json
bield connect https://bield.example.ts.net --with-token < token.txt
```

`--with-token` reads the token from standard input, checks it, and saves it in `cli.json` (readable only by you) with the address. A saved token goes only to that address, so `--url` somewhere else doesn't send it. `BIELD_TOKEN` beats the saved one. `bield mcp` uses the same token.

What a token can do:

- **Its role, and never more than its maker's.** A viewer token reads, an admin token changes things. If the admin who made it becomes a viewer, their admin tokens act as viewers, and if they lose their role, their tokens do nothing, even when anyone on the tailnet can view Bield. Viewer is the default.
- **Not everything an admin can.** Tokens can't open terminals, make or revoke tokens, change who has a role or who can view Bield, or change the Public URL. Those need a person, so a token can't make more of itself. An admin token can still write files and restart containers on every machine Bield reaches, which is as good as a shell there, so give one out like an admin's laptop, and prefer a viewer token.
- **It runs out.** Each token works for 1 to 365 days, 30 by default. An expired one is refused with a message that says when it expired. `bield tokens revoke <name>` stops it at once, and a stream it opened, such as `alerts --follow`, ends within a minute. A revoked token stays in `bield tokens`, with who revoked it and when, and its name is never used again, so a name in the activity log always means one token.
- **It works from any machine that can reach Bield.** That's the tailnet, plus whatever reaches a proxy you put in front of Bield with a Public URL. A token isn't tied to one machine. `bield tokens` shows when each was last used (to the minute) and from which device.

A request that sends a token Bield doesn't know, or one that expired, is refused with a 401 even where no role is needed. It never quietly falls back to who Tailscale says you are.

### A block for AGENTS.md or CLAUDE.md

```md
## Bield

Our machines are watched by Bield. Use the `bield` command to look at them and to
fix what it finds.

- Start with `bield status`, then `bield alerts` and `bield checks --failing`.
- Always pass `--json` and read the output before you act.
- Read before you change: `bield devices show <device>`, `bield alerts show <id>`,
  `bield docker logs <device> <container> -n 200`.
- Ask me before anything that changes a machine: `docker stop`, `docker restart`,
  `files write`, `settings set`, `access ...`. They need `--yes`; don't add it
  until I've said yes.
- `bield alerts ack <id>` and `bield alerts note <id> <text>` are fine to run on
  your own.
- Exit code 4 means you aren't allowed. Don't look for a way around it; tell me.
```

## Machines without Tailscale

A container, a CI runner or any machine without Tailscale can still use `bield`: give it an [auth key](https://tailscale.com/kb/1085/auth-keys) (or a Headscale pre-auth key) in `BIELD_TS_AUTHKEY`, and it joins the tailnet itself, inside its own process, for as long as the command runs. Nothing is installed and nothing needs root. Pair it with an [API token](#api-tokens), since a node made from a tagged key belongs to no one:

```sh
export BIELD_URL=https://bield.example.ts.net
export BIELD_TS_AUTHKEY=tskey-auth-…   # tagged, ephemeral, reusable: a CI key
export BIELD_TOKEN=bield_…            # a viewer token
bield checks --failing --json
```

- Each run is a new, ephemeral node that logs out when the command ends, so the machine list stays clean. Joining adds about 3 seconds to a command, which is why it only happens when the command needs Bield: `help`, `completion` and a mistyped command don't join.
- `BIELD_TS_STATE_DIR` keeps the node's state in a directory, so later runs are the same node and don't log in again (about 2 seconds). Such a node stays in the machine list, offline when nothing runs, like any other machine.
- `BIELD_TS_HOSTNAME` names the node (`bield-cli` by default), and `BIELD_CONTROL_URL` points it at Headscale, as it does for `bield serve`.
- `bield mcp` joins the same way, on its first tool call, and stays joined while it runs.
- The tailnet's access rules decide what the node reaches, so let its tag reach Bield's port. A node that sees no machine at all says so.

Tried against Headscale 0.29.7 from a Debian container with no Tailscale in it. It hasn't been tried with a Tailscale auth key yet.

## Who did it

A change made with the command line is in the activity log with the person's login, and says it came from the command line: *Acknowledged: Disk 90% full on NAS, by alice@example.com with the CLI*. The command line says so in its `User-Agent`, `bield-cli/<version>`. Bield takes the client's word for that, since it only labels the line: who did it is Tailscale's answer, and no client can change that. A change made with an API token is under the login of the admin who made the token, with the token's name: *by alice@example.com with the token ci-deploy, with the CLI*. Making and revoking tokens is in the log too.

## MCP

`bield mcp` is a server for AI agents that take typed tools and not shell commands. Its tools are made from these commands, and it is read-only unless you start it with `--allow-changes`. See [MCP server for AI agents](mcp.md).

## What it doesn't do yet

- **Running a command on a device**, and an interactive terminal. The browser terminal does that, and giving an agent the same is a decision that needs its own approval step and its own record.

Bield and `bield` should be the same version. A newer command line talking to an older Bield says so when the older one lacks what it needs.
