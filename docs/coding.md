# AI usage

If you use AI tools on your machines, for coding or for anything else they're used for, Bield can show how much each one used: tokens, an estimate of what they would cost, and how close the tools say they are to their limits. It works for Claude Code, Codex, OpenCode, Pi, Gemini CLI, Grok, Muse and Antigravity, and it needs nothing from the tools themselves. The [agent](agent.md) reads the files they already write and sends Bield counts.

It's off until an admin turns it on for a machine, and it needs agent 1.4.0 or newer on that machine, or 1.5.0 for Gemini CLI, Grok, Muse and Antigravity. That includes the machine Bield runs on: Bield measures that one itself and has no agent there, but once you turn it on for that machine, it can have one that reads only the AI tools. Until that agent has sent its first numbers, the machine's card has an **Install the agent** button.

## Turning it on

Open the machine's page, find the **AI usage** card, and switch on **Watch**. Only admins can. To turn it on for every machine that has an agent at once, press **Watch all** on the **AI usage** page under **Devices**, which shows when some aren't watched, or run `bield ai watch`. `bield ai unwatch --yes` turns it off on every machine. The card is far down the page, so a link under the machine's name, **AI usage**, jumps to it, and says what it holds: the cost, and how many sessions are live. The card shows the last 24 hours, 7, 30 or 90 days, or a year, and your browser remembers the one you pick for every machine's card. The machine's next heartbeat tells its agent to start, and the first numbers arrive within a couple of minutes. Update the agent first if the machine runs an older one (**Update** on the [Agents page](agent.md#the-agents-page)).

The first time, the agent reads the files that are still on the machine, up to 90 days of them, so history shows at once instead of starting from zero. Turning it off stops the reading and keeps what Bield counted.

The **AI usage** page, under **Devices**, adds up every machine that's watched. A session that's going now says **Live**.

## What the agent reads

| Tool | Files it reads | Where |
| --- | --- | --- |
| Claude Code | Session files `*.jsonl`, including the sub-agents' | `~/.claude/projects`, or `$CLAUDE_CONFIG_DIR/projects` |
| Codex | Session files `*.jsonl` | `~/.codex/sessions`, or `$CODEX_HOME/sessions` |
| OpenCode | Its SQLite database | `~/.local/share/opencode`, or `$XDG_DATA_HOME/opencode` |
| Pi | Session files `*.jsonl` | `~/.pi/agent/sessions` |
| Gemini CLI | Chats `*.jsonl`, including the sub-agents' | `~/.gemini/tmp/*/chats`, or under `$GEMINI_CLI_HOME` |
| Grok | `updates.jsonl` of each session | `~/.grok/sessions`, or `$GROK_HOME/sessions` |
| Muse | `session.jsonl` of each session, including the sub-agents' | `~/.local/share/muse/sessions`, or `$XDG_DATA_HOME/muse/sessions` |
| Antigravity | Its conversations, in SQLite | `~/.gemini/antigravity`, `~/.gemini/antigravity-cli`, `~/.gemini/antigravity-ide` and `~/.config/antigravity` |

OpenCode keeps its history in SQLite, and a bash script can't read that alone. The agent uses the `sqlite3` command if the machine has it, and Python 3 if it doesn't. With neither, the machine's card says so, and the other tools still work. OpenCode versions from before its database kept a JSON file for each message in `storage/message`, and the agent reads those too. A message that OpenCode moved into its database since counts once.

Antigravity writes what each request used in protobuf, a binary format, inside its SQLite files, and only Python 3 reads it here, so the agent needs `python3` for Antigravity. Without it, the machine's card says to install it. A conversation can be in more than one of Antigravity's folders, and a response in two of them counts once.

The agent reads the home directory of the user it runs as. On a server where the agent runs as root, or where several people have their own tools, set `BIELD_SESSION_HOMES` in the agent's settings file to the home directories to read, separated by spaces. [The configuration reference](configuration.md#the-agent) lists the settings.

## What it sends

Only counts, names and times: for each session and hour, the tool, the model's name, the session's ID, how many tokens it used by kind, how many requests it made and the name of the project's folder, plus where a tool says its limits stand. It never sends a prompt, a reply, a file's content or a path beyond that one folder name. Viewers don't see the project names either.

To see what a machine would send, run the agent with `--sessions-print`. It reads the files and writes the report to the terminal, and sends nothing:

```sh
BIELD_AGENT_STATE=$(mktemp -d) bash ~/.local/share/bield-agent/agent.sh --sessions-print
```

## What the numbers mean

**Tokens** come in four kinds. *Input* is what the model read fresh. *Cache read* is what it read from the prompt cache, which costs a tenth as much, and is most of the tokens in a long session. *Cache write* is what it put into the cache. *Output* is what it wrote, with its reasoning. *Processed* adds them up.

**Cost** is an estimate of what the same tokens would cost on the model maker's API. It's labelled **API estimate** because a subscription doesn't bill per token, so it says what the use is worth, and not what you were charged. The prices come from [LiteLLM's price list](https://github.com/BerriAI/litellm) (MIT license), which is built into Bield and says on the page which day it's from. A newer Bield has a newer list, and it prices your history again. Some details:

- A model's **fast** tier costs more, and the page shows how much more under **Cost by speed**. Codex's **ultrafast** tier is priced at its own rates, where the list has them, and shown as fast. A model with no ultrafast rates is priced as fast.
- **Cache savings** is what the cache saved: the cached tokens at the input price, less what they cost at the cache price. It isn't part of the cost.
- A tool that knows what it billed, such as Pi or Grok, beats the list. A tool that writes 0 for a cost it doesn't know, such as OpenCode on a free model, doesn't.
- A model that isn't on the list counts for nothing, and the page says how many tokens that is.

**Days** start at midnight where you are. Bield counts by the hour, in UTC, and your browser's time zone puts the hours into days.

### Where it can be off

The files are what the tools wrote, so the numbers are as good as they are:

- Claude Code writes each reply several times, and the agent counts it once. A resumed session, and a sub-agent that starts from its parent's conversation, copy old replies into a new file, and the agent counts each reply once across files too, in the file it read first. Numbers that an agent older than 1.5.0 sent stay as they were, a little high: on the machine this was tested on, the copies were 0.7% of the replies.
- Claude Code's own calls to a smaller model in the background don't appear in its session files, so they aren't counted. Claude Code's total for a session is a little higher than Bield's for that reason. The calls a session makes to an advisor, a second model it asks for advice, are counted, under the advisor's model.
- Codex writes a request's tokens when it finishes. When a request was interrupted before that, the agent counts it from Codex's record of each request, but only if those records add up to what Codex's running total says. When they don't, the request isn't counted.
- Gemini CLI before version 0.39 kept each session as one JSON document, which the agent doesn't read. Sessions from 0.39 on, and old ones that you resume in it, are read.
- Grok and Muse were read from their file formats as T3 Code reads them, and from made-up files. Neither was tried on a real install, and Muse's files say nothing about the project, so its sessions have none.
- The tools delete their files. Claude Code's last 30 days. Bield keeps the numbers it already has, so a machine's history is as long as Bield has watched it, plus whatever files were still there.

## Limits

The **Limits** tab shows where the tools say you stand, as of the last time a session of theirs wrote it down, so it's as old as that session.

- **Codex** writes how much of each of its windows, such as the weekly one, you've used, and when it resets. Bield shows a meter. Once the window has reset, the number is out of date and says so.
- **Claude Code** only writes a limit down when it stops you at one, with when it resets. Bield shows **Limit reached** until then, and nothing while you're under it, because Claude Code doesn't record how close you are.
- **OpenCode**, **Pi**, **Gemini CLI**, **Grok**, **Muse** and **Antigravity** don't record limits in their files.

## Who sees what

Viewers see the totals, the charts and the limits. Admins also see the names of the projects. Only admins can turn watching on or off, and each change is in the [activity log](pages.md#activity).

## How long it's kept

Bield keeps the hourly numbers for 365 days. **Settings**, **Retention**, **AI tools' usage** changes that, up to three years. The numbers are a few kilobytes a day for each machine.

## Tools Bield doesn't read

Some tools don't write their token counts on the machine, so there's nothing for the agent to read:

- **Cursor** keeps its chats on the machine, but the token counts are only on Cursor's servers, which want your Cursor account.
- **Kiro** writes what a session cost in credits, not tokens.
- **Aider** writes its chats as Markdown, with no counts. It only writes them to a log you turn on with `--analytics-log`, which the agent doesn't read.

Each tool is a small reader in the agent, so a new one is a matter of its file format. If you use one that isn't read, open an issue and say where it keeps its sessions.

## When nothing shows up

- Check the machine's agent is 1.4.0 or newer and reporting, on the [Agents page](agent.md#the-agents-page).
- Check **Watch** is on for the machine.
- Run `bash ~/.local/share/bield-agent/agent.sh --sessions-print` on the machine. If it prints a report with sessions, the files are being read and the problem is the way to Bield, which the agent's log says.
- The tool may keep its files somewhere else. Say where in `BIELD_SESSION_HOMES`, or with the tool's own variable.
- For OpenCode on a machine that has neither `sqlite3` nor Python 3, install one of them. Antigravity needs Python 3.
