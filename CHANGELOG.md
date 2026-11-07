# Changelog

What changed in each release of Bield, newest first. The [landing page](https://manasseh-fikadu.github.io/bield/#changelog) shows the latest ones from this file. The release notes on [GitHub](https://github.com/manasseh-fikadu/bield/releases) also say what has been run and what hasn't.

## Unreleased

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
