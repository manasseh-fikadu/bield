-- What the AI coding tools on a device used (#73): Claude Code, Codex,
-- OpenCode and Pi. The device's agent reads the files those tools write and
-- reports counts: tokens, the model, when, and the name of the project folder.
-- Never a prompt or a reply. The tools delete their files (Claude Code's last
-- 30 days), so Bield keeps the numbers.
--
-- device_id is as in metrics. Dollars aren't stored for tokens: they're worked
-- out from the price list when someone looks, so a newer list prices old
-- history too. Only a cost that the tool itself reported is kept.

-- The devices an admin turned this on for. A device is on while it has a row.
CREATE TABLE coding_devices (
    device_id TEXT PRIMARY KEY
) STRICT, WITHOUT ROWID;

-- Tokens by the hour. The agent sends totals for a key, as far as its files
-- say, not what's new, and Bield keeps the larger of what it has and what it
-- gets, so a message that comes twice, or an agent that starts over, changes
-- nothing.
CREATE TABLE coding_usage (
    device_id   TEXT    NOT NULL,
    tool        TEXT    NOT NULL, -- claude, codex, opencode or pi
    session     TEXT    NOT NULL, -- the tool's own id for the session
    model       TEXT    NOT NULL,
    speed       TEXT    NOT NULL, -- standard or fast
    hour        INTEGER NOT NULL, -- Unix milliseconds, at the start of the UTC hour
    input       INTEGER NOT NULL, -- tokens the cache didn't serve
    cache_read  INTEGER NOT NULL,
    cache_write INTEGER NOT NULL,
    output      INTEGER NOT NULL, -- with the reasoning
    reasoning   INTEGER NOT NULL, -- of the output
    requests    INTEGER NOT NULL,
    cost_micro  INTEGER NOT NULL, -- millionths of a dollar the tool reported, or 0
    PRIMARY KEY (device_id, tool, session, model, speed, hour)
) STRICT, WITHOUT ROWID;

-- Looking at one device over a span of time, and deleting what's old.
CREATE INDEX coding_usage_device_hour ON coding_usage (device_id, hour);
CREATE INDEX coding_usage_hour ON coding_usage (hour);

-- The sessions, one row each, with when they started and last did anything.
CREATE TABLE coding_sessions (
    device_id TEXT    NOT NULL,
    tool      TEXT    NOT NULL,
    session   TEXT    NOT NULL,
    project   TEXT    NOT NULL, -- the project folder's name, which only admins see
    first_at  INTEGER NOT NULL, -- Unix milliseconds
    last_at   INTEGER NOT NULL,
    PRIMARY KEY (device_id, tool, session)
) STRICT, WITHOUT ROWID;

CREATE INDEX coding_sessions_device_last ON coding_sessions (device_id, last_at);
CREATE INDEX coding_sessions_last ON coding_sessions (last_at);

-- How much of a limit is used, as the tool last said. A tool says it while it
-- works, so this is as old as the last session that did.
CREATE TABLE coding_limits (
    device_id      TEXT    NOT NULL,
    tool           TEXT    NOT NULL,
    id             TEXT    NOT NULL, -- which limit: codex:primary, five_hour
    window_minutes INTEGER NOT NULL, -- how long the limit's window is, or 0
    used           REAL,             -- percent used, or NULL if the tool doesn't say
    resets_at      INTEGER,          -- Unix milliseconds, or NULL
    status         TEXT    NOT NULL, -- ok or rejected
    plan           TEXT    NOT NULL,
    seen_at        INTEGER NOT NULL, -- when the tool wrote it, in Unix milliseconds
    PRIMARY KEY (device_id, tool, id)
) STRICT, WITHOUT ROWID;

-- Whether the agent could read each tool's files, as of its last report.
CREATE TABLE coding_tools (
    device_id TEXT    NOT NULL,
    tool      TEXT    NOT NULL,
    status    TEXT    NOT NULL, -- ok, unreadable or needs-sqlite
    at        INTEGER NOT NULL, -- Unix milliseconds
    PRIMARY KEY (device_id, tool)
) STRICT, WITHOUT ROWID;
