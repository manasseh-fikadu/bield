-- VS Code in the browser (#46): the machines an admin installed code-server
-- on. Each one's editor is served on a port of Bield's own, which gives it an
-- origin apart from Bield's and from every other machine's editor, so the
-- port stays the same for as long as the editor is installed: the browser
-- keeps what the editor stores under its origin.
CREATE TABLE editors (
    -- The ID SSH access is kept under: the device's ID, or HubID for the
    -- machine Bield runs on.
    device_id  TEXT PRIMARY KEY,
    port       INTEGER NOT NULL UNIQUE,
    -- The code-server version Bield installed last.
    version    TEXT NOT NULL,
    created_at INTEGER NOT NULL, -- Unix milliseconds
    created_by TEXT NOT NULL     -- the admin's login
) STRICT;
