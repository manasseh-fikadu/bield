# The command line

`bield` isn't only the server. The same file looks at a running Bield and changes things in it from a terminal: `bield devices`, `bield alerts ack 5`, `bield docker restart nas jellyfin --yes`. It's for people, and for scripts and AI agents such as Claude Code and Codex, which already run shell commands and so get Bield without a new protocol.

It has the roles the web app has. There is nothing to log in to: Bield asks Tailscale who is on the other end of the connection, so a command run on your laptop is you, with your role. A viewer can read, an admin can change things, and a command that needs more than you have says so and exits with code 4.

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

For shell completion, `bield completion bash` prints a script for bash (`source <(bield completion bash)`), and `zsh` and `fish` print theirs.

## What it does

| Command | What it does | Needs |
| --- | --- | --- |
| `bield status` | Health, how many devices are online and how many alerts are open | viewer |
| `bield devices` | The devices and how they're doing. `--status` and `--kind` pick some | viewer |
| `bield devices show <device>` | Everything Bield knows about one | viewer |
| `bield devices history <device>` | CPU, memory, network and latency over `--range` 1h, 6h, 24h or 7d | viewer |
| `bield devices usage <device>` | What uses a device's CPU and memory now | viewer |
| `bield devices set <device>` | Change a device's name, kind, notes, offline alert or thresholds | admin |
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
| `bield enroll` | Make an install command for the [agent](agent.md) | admin |
| `bield settings`, `settings set <name=value>...` | Show or change [the settings](settings.md) | admin |
| `bield access`, `access set`, `remove`, `who-can-view` | Who has a role in Bield | admin |

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

Bield's own machine is anonymous too, since any process there could send the request. So an agent on a tagged server, in CI, or on the machine Bield runs on can't use the commands that need a role. Run bield from a machine a person is logged in to. An admin's agent on their laptop acts with the admin's full role, which includes opening files and restarting containers on any device Bield can reach over SSH. Give an agent a viewer's laptop, or tell it not to change anything, if that is too much.

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

## Who did it

A change made with the command line is in the activity log with the person's login, and says it came from the command line: *Acknowledged: Disk 90% full on NAS, by alice@example.com with the CLI*. The command line says so in its `User-Agent`, `bield-cli/<version>`. Bield takes the client's word for that, since it only labels the line: who did it is Tailscale's answer, and no client can change that.

## What it doesn't do yet

- **API tokens.** Everything above acts as a person on the tailnet. A server, a CI job or an agent that should have less access than its owner has no way in yet. Scoped tokens are the next step.
- **Running a command on a device**, and an interactive terminal. The browser terminal does that, and giving an agent the same is a decision that needs its own approval step and its own record.
- **An MCP server**, for agents that take typed tools and not shell commands. It will use the same client as this.
- **Reaching Bield from a machine that isn't on the tailnet**, such as a container with no Tailscale.

Bield and `bield` should be the same version. A newer command line talking to an older Bield says so when the older one lacks what it needs.
