# Changelog

What changed in each release of Bield, newest first. The [landing page](https://manasseh-fikadu.github.io/bield/#changelog) shows the latest ones from this file. The release notes on [GitHub](https://github.com/manasseh-fikadu/bield/releases) also say what has been run and what hasn't.

## Unreleased

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
