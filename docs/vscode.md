# VS Code in the browser

Bield can open a machine in VS Code in your browser. It installs [code-server](https://github.com/coder/code-server), the open-source build of VS Code that runs in a browser, on the machine itself, and runs it as the user Bield logs in as over SSH. Bield's own machine does no work for it, and you don't need anything new to sign in. It works like the [terminal](ssh.md): only admins can use it, and every open and close goes in the activity log.

## What it needs

- **SSH access** to the machine: the **SSH access** card on its page says **Connected** (see [SSH access, terminals and files](ssh.md)). Bield installs and runs code-server over that login, and reaches the editor through it.
- **Linux or macOS on x86-64 or ARM64**, with glibc. code-server has no build for musl, so Alpine is out, and none for FreeBSD or 32-bit ARM. Bield can't log in to Windows, iOS or Android.
- **About 1 GB free in the user's home folder** while it installs: the download is about 230 MB and code-server takes about 700 MB once unpacked.
- **Internet access on the machine**, to download code-server from GitHub, and `curl` or `wget`, `tar`, and `sha256sum` or `shasum`.
- **HTTPS on your tailnet**, ideally. Without it, browsers turn off parts of VS Code, such as webviews (Markdown preview and many extensions' panels) and the clipboard. See [HTTPS](tailscale.md#https).

## Install it

Open the machine's page and find the **VS Code** card. It says which version Bield installs, how big the download is and how much room it takes. Press **Install VS Code**.

The machine downloads the build of code-server for its system from GitHub, and Bield checks it against the SHA-256 it has pinned for that version before it unpacks it, so a download that was changed on the way, or replaced on GitHub, is refused. Then Bield runs the new code-server once to check that it works on the machine. The install goes on in the background, so you can leave the page. The card shows how it went, and the activity log has a line for it.

Each Bield release pins one version of code-server. When a newer Bield pins a newer one, the card offers **Update**, which installs it the same way and stops the editor first. From the command line, `bield editor install <device>` does the same and waits until it's done.

## Open it

Press **Open VS Code** on the machine's page, or **Open in VS Code** in the file manager to open the folder you're in. VS Code opens in a new tab. If code-server isn't running, Bield starts it first, which takes a few seconds.

code-server stops by itself 30 minutes after the last window on it closed. If you come back to a tab after that, or open its address from a bookmark, the page says so and has a **Start VS Code** button. **Stop** in the card's menu stops it at once, and closes it for everyone who has it open. Several admins can have one machine's editor open at a time. They work on the same files as the same user, and VS Code doesn't merge their changes to a file: the last one saved wins.

`bield editor open <device> [folder]` starts it and prints the address to open.

## Where it's served

Each machine's editor gets a port of Bield's own, from 4710 up, such as `https://bield.example.ts.net:4710`. The port is the machine's for as long as VS Code is installed there, and the card shows it. A different port is a different origin to a browser, and that's the point: everything VS Code runs in your browser comes from the machine (code-server's files and any extension you install), so it mustn't be able to call Bield's API as you, or reach another machine's editor. On a port of its own it can do neither.

That has two consequences:

- **Your tailnet policy has to let you reach those ports.** The default policy allows everything. If yours only allows 443 to Bield, add the range: `"ip": ["tcp:443", "tcp:80", "tcp:4710-4799"]` in the grant that lets people open Bield (see [Tailnet policy](tailscale.md#tailnet-policy)).
- **The editor is only on the tailnet**, at Bield's tailnet name. A [Public URL](settings.md) in front of Bield doesn't serve it.

`BIELD_EDITOR_PORTS` (or `--editor-ports`) changes the range, such as `9000-9009`, one port for each machine with VS Code (see the [configuration reference](configuration.md)). In host mode, a port that another program has is skipped at install, and Bield tries again every minute for an editor whose port is taken.

## On the machine

Everything goes in `~/.local/share/bield/code-server` of the user Bield logs in as, which only that user can open:

| Path | What it is |
| --- | --- |
| `4.145.2/` | code-server, one folder per version, with its own Node.js |
| `current` | A link to the version in use |
| `data/` | VS Code's settings, state and logs (`--user-data-dir`) |
| `extensions/` | The extensions you installed |
| `config.yaml` | code-server's config, which only says `auth: none` |
| `run/` | The socket, the process ID and code-server's log |

code-server also keeps a heartbeat file and two logs in `~/.local/share/code-server`, whatever folder it's given, as a code-server of your own would. Removing VS Code deletes that folder too, unless it holds anything else.

code-server listens on a Unix socket in `run/` (mode 0600, in a folder of mode 0700), not on a TCP port, so nothing new listens on the machine's network and no other user on it can reach it. Bield reaches the socket by forwarding it over its SSH login, as `ssh -L` does, the same way it reaches Docker. With [Tailscale SSH](ssh.md#two-ways-to-log-in), that works when the tailnet's control server allows port forwarding for the rule that lets Bield in. Headscale allows it for `accept` rules. It hasn't been tried with Tailscale's own control server yet.

code-server runs with `--auth none`, because the socket is the boundary, and with telemetry, the update check and its own port proxy turned off. It runs apart from the SSH session that started it, so it keeps running when the session ends. On a machine where systemd-logind ends a user's processes when their session ends (`KillUserProcesses=yes` in `/etc/systemd/logind.conf`, which most distributions turn off), it stops as soon as it starts. Turn that off, or add the user to `KillExcludeUsers=`.

## Extensions

code-server installs extensions from [Open VSX](https://open-vsx.org), the Eclipse Foundation's open registry, not from Microsoft's Visual Studio Marketplace, whose terms only allow Microsoft's own products to use it. Most popular extensions are on Open VSX, but Microsoft's own aren't: **GitHub Copilot, Remote - SSH, Live Share, Pylance and the C# Dev Kit won't install.** Open VSX is free to use, each extension keeps its own license, and the rate limits it began adding in 2026 are for heavy automated traffic, not for people using an editor. You can install a `.vsix` file you downloaded yourself with **Extensions: Install from VSIX** in the command palette.

## Who can use it

- **Admins only.** VS Code reaches the same files and runs the same commands as a shell on the machine, so it gets the same bar as the terminal. Viewers don't see the card, and the editor's port turns them away, as it does anyone Tailscale can't name, tagged devices and Bield's own machine.
- **People only.** The editor's port refuses requests with an API token, and so does starting it. An admin token can install, update, stop and remove code-server, as the CLI and the MCP server do, but can't open it.
- **It fails closed.** Without SSH access to the machine, or while Bield can't log in, there's nothing to reach, and the page says why.
- **Only the editor's own pages can use it.** Its port answers only to Bield's tailnet name with that port, its WebSockets must come from its own pages, and requests that another page makes, even one on Bield or on another machine of the tailnet, are refused (browsers say where a request comes from). Another site can only link to it.
- **A role that changes takes effect within seconds.** An admin who's demoted or removed loses the editor's open connections within 10 seconds, as with the terminal.

The activity log says who installed, updated, removed and stopped VS Code on which machine, and who opened it and closed it. It doesn't record what anyone did inside the editor.

## Remove it

**Remove** in the card's menu stops code-server and deletes it from the machine. By default it keeps the settings and extensions in `data/` and `extensions/`, for the next install. Uncheck **Keep its settings and extensions** to delete the whole folder. `bield editor remove <device> --yes` does the same, and keeps them with `--keep-data`.

## When something goes wrong

| What you see | What to do |
| --- | --- |
| The card says to set up SSH access | Set up SSH access on the same page first |
| The install fails with "doesn't run on this machine" | The machine's C library or system isn't one code-server is built for. The message has what code-server said |
| The install fails with the SHA-256 | The download wasn't the file Bield pinned. Try again; if it fails again, something on the way changes downloads |
| The new tab can't connect | Your tailnet policy doesn't let your device reach the editor's port. See [Where it's served](#where-its-served) |
| The card says Bield can't listen on the port | Another program on Bield's machine has it (host mode). Free it, or change `BIELD_EDITOR_PORTS` and remove and install VS Code again on that machine |
| VS Code says it can't reconnect | code-server stopped, as it does after 30 minutes with no window open, or the machine went offline. Reload the tab |
| Markdown preview and some extensions' panels are blank | Bield is served over plain HTTP. Turn on HTTPS in your tailnet |
| code-server stops right after it starts | Look in `~/.local/share/bield/code-server/run/log` on the machine. If logind ends the user's processes when a session ends, see [On the machine](#on-the-machine) |
