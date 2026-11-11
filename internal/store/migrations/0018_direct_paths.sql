-- Path change alerts (#86): when Bield last reached each device directly. A
-- device that falls from direct to a relay raises an alert, even after Bield
-- restarts, and one that has never been direct, such as one behind a strict
-- NAT, doesn't.
CREATE TABLE direct_paths (
    device_id TEXT PRIMARY KEY,
    at        INTEGER NOT NULL -- Unix milliseconds of the latest direct reply
) STRICT, WITHOUT ROWID;

-- Whether to alert when the device falls from direct to a relay: 0 or 1, or
-- NULL for the default, which is on for servers.
ALTER TABLE device_settings ADD COLUMN alert_relayed INTEGER;
