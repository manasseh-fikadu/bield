-- Scoped API tokens (#49): what lets a caller that Tailscale can't name, such
-- as an agent on a tagged server or in CI, use the API. An admin makes each
-- one with a name, a role and an expiry. Only a hash of the token is kept, so
-- the database can't be used to call the API. A token never acts with more
-- than the role its maker has now, so created_by is kept as the login.
--
-- A revoked token keeps its row, and so its name: names are never reused, so
-- a name in the activity log always means one token.
CREATE TABLE api_tokens (
    id             INTEGER PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    token_hash     BLOB NOT NULL UNIQUE,
    -- The last characters of the token, so people can tell which one they hold.
    hint           TEXT NOT NULL,
    role           TEXT NOT NULL CHECK (role IN ('admin', 'viewer')),
    created_by     TEXT NOT NULL,
    created_at     INTEGER NOT NULL, -- Unix milliseconds
    expires_at     INTEGER NOT NULL, -- Unix milliseconds
    last_used_at   INTEGER,          -- Unix milliseconds, to the minute
    last_used_node TEXT,             -- the node ID the token was last used from
    revoked_at     INTEGER,          -- Unix milliseconds, if it was revoked
    revoked_by     TEXT              -- the login of the admin who revoked it
) STRICT;

-- The name of the API token a change was made with, or '' if it wasn't. The
-- actor is still the login of the token's maker.
ALTER TABLE events ADD COLUMN token TEXT NOT NULL DEFAULT '';
