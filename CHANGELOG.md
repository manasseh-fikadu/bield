# Changelog

What changed in each release of Bield, newest first. The [landing page](https://manasseh-fikadu.github.io/bield/#changelog) shows the latest ones from this file. The release notes on [GitHub](https://github.com/manasseh-fikadu/bield/releases) also say what has been run and what hasn't.

## Unreleased

### Added

- AI usage reads Gemini CLI, Grok, Muse and Antigravity too, with agent 1.5.0. Antigravity needs Python 3 on the machine, since its conversations are in a binary format; the machine's card says so when it's missing. See [What the agent reads](docs/coding.md#what-the-agent-reads).
- AI usage reads OpenCode's JSON files from before it kept a database, and counts the calls a Claude Code session makes to an advisor model, under that model.
- Codex's ultrafast tier is priced at its own rates. It was priced as fast.
- **Watch all** on the **AI usage** page turns on watching the AI tools for every machine that has an agent, and `bield ai watch` does the same. `bield ai unwatch --yes` turns it off on every machine.

### Changed

- A machine's **AI usage** card has every range the AI usage page has, from 24 hours to a year, in a list next to **Watch**, and your browser remembers the one you pick.

### Fixed

- A Claude Code reply that a resumed session or a forked sub-agent copied into another file counts once. It counted once in each file, about 0.7% too much on the machine it was tried on. Agent 1.5.0 reads each machine's files again once, to know which file counted what, and what an older agent sent stays as it was.
- A Codex request that was interrupted before Codex wrote its tokens down counts, when Codex's record of each request accounts for it.

## 0.7.0 - 2026-11-12

This release is about how traffic reaches your machines: Tailscale tags, a filter by route, an alert when a machine that was direct falls back to a relay, and a suggestion for a peer relay. It also measures routes better. The agent doesn't change, and Bield adds a table and a column to its database when it starts, which needs no action from you.

### Added

- Devices show their Tailscale tags, all of them: on the Devices page, on a device's page, and in `bield devices` and `bield devices show`. Pick one tag in the Devices page's **Tag** list or with `bield devices --tag server`. The command palette finds a device by its tags too. Bield only shows tags. You set them in Tailscale.
- Pick the devices Bield reaches one way, directly, through a DERP relay or through a peer relay, in the Devices page's **Route** list or with `bield devices --path derp`.
- An alert when a device that was direct is reached through a relay: after five pings in a row through a DERP server or a peer relay, for a device that was direct in the last week. It resolves once the device is direct again. It's on for servers and off for everything else, which the device's **Alert when relayed** switch and `bield devices set --alert-relayed` change, and **Relayed connection** in Settings turns it off for every device. A device's page says when Bield last reached it directly. Bield adds a table for that when it starts, which needs no action from you.
- A **Peer relay** page for the devices Bield reaches through DERP. It suggests a machine of yours to be a [peer relay](https://tailscale.com/kb/1591/peer-relays) for them, which is often faster than Tailscale's shared DERP servers: a computer Bield reaches directly, best a server at a public address. It gives the command to run on that machine and the grant to add to your tailnet policy file, which you can't do from Bield, and says that Bield only sees its own connections, so it can't tell whether the relay will help the devices reach each other. The Devices page and a device's page link to it when devices are on DERP, and `bield relay` prints the same. It doesn't show on Headscale, which has no released support for peer relays yet.

### Fixed

- A device that Bield reaches directly or through a peer relay no longer shows as relayed through DERP. Bield pings each device once a minute, and a connection that has been quiet since answers the first ping through DERP while Tailscale finds the direct or peer relay path again. Bield now pings again for a few seconds while the answers come through DERP, as `tailscale ping` does.

## 0.6.1 - 2026-11-11

This release fixes how VS Code in the browser looks while it loads the first time. The agent doesn't change, and there is no database change.

### Fixed

- VS Code no longer says "The editor failed to load" while it's still loading. Its first load over a relayed connection takes longer than the 10 seconds VS Code waits, so Bield's editor port has it say it's loading, and shows the failure only after two minutes. The activity log also names the machine Bield runs on one way for its editor, and the editor's port sends each of its security headers once.

## 0.6.0 - 2026-11-11

This release adds VS Code in the browser: Bield installs code-server on a machine over SSH and opens it in a tab of its own. The agent doesn't change, so there's nothing to update on your machines. Bield adds a table for the editors when it starts, which needs no action from you. If your tailnet policy only lets people reach port 443 of Bield, allow ports 4710 to 4799 too, where the editors are served.

### Added

- VS Code in the browser on a machine. An admin installs code-server on it from the machine's page, over SSH, and opens it in a tab of its own, or opens the folder they're in from the file manager. Bield checks the download against the SHA-256 it pins, code-server listens on a private socket on the machine, and it stops by itself after 30 minutes with no one connected. Each machine's editor is served on a port of Bield's own, from 4710 (`BIELD_EDITOR_PORTS`), so your tailnet policy has to let you reach those ports. Opening and closing it is in the activity log, and `bield editor` does it from the command line. See [VS Code in the browser](docs/vscode.md).

## 0.5.0 - 2026-11-11

This release adds scoped API tokens, so a tagged server, a CI job or an AI agent can use Bield's API, command line and MCP server, and lets `bield` join the tailnet by itself on machines without Tailscale. The agent doesn't change, so there's nothing to update on your machines. Bield adds a table for the tokens and a column to the activity log when it starts, which needs no action from you.

### Added

- Scoped API tokens, for machines no person is logged in to, such as a tagged server, a CI job or an AI agent that should have less access than you. An admin makes one in Settings, under API tokens, or with `bield tokens create`, with a role and an expiry of up to a year. Use it with `BIELD_TOKEN` or `bield connect <url> --with-token`, and `bield mcp` takes it too. A token never does more than the admin who made it can now, and it can't open terminals, make tokens or change who has access. Changes made with one are in the activity log with its name. See [API tokens](docs/cli.md#api-tokens).
- `bield` runs on machines without Tailscale, such as containers and CI runners: with an auth key in `BIELD_TS_AUTHKEY`, it joins the tailnet itself for the length of a command. See [Machines without Tailscale](docs/cli.md#machines-without-tailscale).

## 0.4.0 - 2026-11-10

This release adds an MCP server for AI agents and fixes installing the agent on machines whose login has no systemd session. The agent itself doesn't change, so there's nothing to update on machines where it already runs. There is no database change.

### Added

- `bield mcp`, an MCP server for AI agents such as Claude Code, Codex and Cursor. Its tools are made from the command line's commands, with the same roles. It is read-only unless you start it with `--allow-changes`, and the activity log says *with MCP* for the changes it makes. See [MCP server for AI agents](docs/mcp.md).

### Fixed

- Installing the agent failed with "Failed to connect to user scope bus" on machines where the login has no systemd session, such as DietPi over Dropbear, leaving a machine that sent one heartbeat and then went quiet. As root the agent now installs as a system service, which also keeps running after you log out. Other users get their running systemd found for them, or a message that says what to do before anything has changed on the machine.
- The agent installer stopped with an error at the very end when `USER` wasn't set.

## 0.3.0 - 2026-11-07

This release adds a command line. The agent doesn't change, so there's nothing to update on your machines. Bield adds a column to the activity log when it starts, which needs no action from you.

### Added

- A command line for people, scripts and AI agents. `bield connect` saves where Bield is, and then `bield status`, `bield devices`, `bield alerts ack 5`, `bield docker restart nas web --yes`, `bield files put` and the like do what the web app does, with the same roles. There is nothing to log in to: Bield sees who you are through Tailscale. Every command takes `--json`, never asks a question, and exits with a code that says what went wrong. See [The command line](docs/cli.md).
- The activity log says when a change came from the command line: *by alice@example.com with the CLI*.
- `GET /api/snapshot`, the first event of the stream as plain JSON, for callers that don't hold a stream open.

### Changed

- A snapshot, and the first one a stream sends, now includes a change made a moment ago, instead of the one from up to a second before.

### Fixed

- A page that connected while Bield was still starting could be sent its first snapshot twice, since Bield built it once for the request and again when it finished starting.

## 0.2.1 - 2026-11-07

This release renames coding usage to AI usage and fixes how its card looks. The agent doesn't change, so there's nothing to update on your machines.

### Changed

- Coding usage is now called AI usage, since Claude Code, Codex and the others get used for more than code. The machine's page has a link under its name, with the cost and how many sessions are live, that jumps down to the card, which sat out of sight at the bottom.
- A session that is going now says Live on the right of its row. It had a white lamp next to the tool's own dot, and Codex's dot was the same white. A tool's color is a square now and a lamp always means a status, and Codex is green.

### Fixed

- On the machine Bield runs on, the AI usage card kept saying Install the agent after the agent was installed and reporting. It asks for the agent only until the first numbers arrive.

## 0.2.0 - 2026-11-07

This release adds what your coding tools use on each machine. Update the agent from its page in Bield, to get the new one (1.4.0), and turn coding usage on for the machines you want it from.

### Added

- Coding usage: what Claude Code, Codex, OpenCode and Pi used on each machine, as tokens, an estimate of what the same tokens would cost on the model makers' APIs, and the limits the tools report. It shows on a machine's page and on a new Coding page under Devices. An admin turns it on for each machine, and it needs the agent updated to 1.4.0, which reads the tools' session files and sends only counts, model names, times and project folder names.
- The machine Bield runs on can have an agent that reads only the coding tools, once an admin turns coding usage on for it.

### Changed

- Settings has a retention for coding usage, 365 days by default.

- Updated the Tailscale library (tailscale.com) to v1.104.4.

### Security

- Updated klauspost/compress to v1.20.2, for GO-2026-6865, a panic on two-block frames.

## 0.1.1 - 2026-11-05

This release fixes what an outside review of v0.1.0 found. Update the agent from its page in Bield, to get its fix.

### Security

- An open terminal closes within 10 seconds of its admin losing the role. It used to stay open until the tab closed.
- Viewers no longer see Docker's error from starting a container, which can quote its command and paths on the machine.

### Fixed

- Saving in the file editor can't empty the file when the save fails. The editor has the whole text before it touches the file.
- The agent (1.3.1) copes with a hostname that has a control character in it. Before, Bield refused every heartbeat from that machine.
- The Public URL setting counts as one of Bield's own names, so agent install commands and terminals work through it.

## 0.1.0 - 2026-11-04

The first release. Bield is a self-hosted control plane for the machines on your tailnet, in one binary or one container, with no passwords, because your tailnet is the login.

### Added

- Every machine on the tailnet on a live map, with how traffic reaches it and the round-trip time.
- CPU, memory, disk and network for each machine, measured by Bield on Linux or by a small bash agent, with charts for up to 90 days.
- Website, port and DNS checks, a ping of every device, and a health score.
- Alerts, with notifications to ntfy, Telegram, email, Discord, Slack and webhooks, and an activity log of who did what.
- Docker: containers, logs, stats, images, volumes and networks, and start, stop and restart.
- A terminal in the browser and a file manager, over SSH or Tailscale SSH.
- Admin and read-only viewer roles, decided by tailnet identity.
- Headscale support.
- `bield serve --demo`, a made-up tailnet to try it without one.
- Releases for Linux and macOS on amd64 and arm64, a Docker image and an install script.
