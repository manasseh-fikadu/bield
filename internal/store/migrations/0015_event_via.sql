-- How a person's change reached Bield, when it wasn't the web app: 'cli' for
-- the command line (#49). '' is the web app, or something Bield saw happen.
-- It's a label the client gives itself, in its User-Agent, and not something
-- Bield checks: who did it is still the actor, which Tailscale vouches for.
ALTER TABLE events ADD COLUMN via TEXT NOT NULL DEFAULT '';
