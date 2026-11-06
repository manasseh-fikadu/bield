package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CodingUsage is the tokens a coding tool used in an hour of one session, on
// one model at one speed. The numbers are totals as far as the tool's files
// say.
type CodingUsage struct {
	DeviceID, Tool, Session, Model, Speed string
	// Hour is the start of the UTC hour.
	Hour time.Time
	// Input is only the input that the cache didn't serve, and Output
	// includes Reasoning.
	Input, CacheRead, CacheWrite, Output, Reasoning, Requests int64
	// CostMicro is the cost in millionths of a dollar that the tool wrote
	// down, or 0.
	CostMicro int64
}

// A CodingSession is a session of a coding tool.
type CodingSession struct {
	DeviceID, Tool, ID string
	// Project is the name of the project's folder.
	Project     string
	First, Last time.Time
}

// A CodingLimit is how much of one of a tool's limits was used, when the tool
// last said.
type CodingLimit struct {
	DeviceID, Tool, ID string
	// WindowMinutes is how long the limit's window is, or 0 if it isn't known.
	WindowMinutes int
	// Used is the percent used, if the tool says.
	Used *float64
	// ResetsAt is when the window starts over, if the tool says.
	ResetsAt *time.Time
	// Status is "ok" or "rejected".
	Status, Plan string
	Seen         time.Time
}

// CodingToolStatus says whether the agent could read a tool's files.
type CodingToolStatus struct {
	DeviceID, Tool, Status string
	At                     time.Time
}

// CodingReport is what a device's agent sent about its coding tools.
type CodingReport struct {
	Usage    []CodingUsage
	Sessions []CodingSession
	Limits   []CodingLimit
	Tools    []CodingToolStatus
}

// CodingEnabled reports whether coding tools are watched on a device.
func (s *Store) CodingEnabled(ctx context.Context, deviceID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM coding_devices WHERE device_id = ?", deviceID).Scan(&one)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read whether coding tools are watched: %w", err)
	}
	return true, nil
}

// CodingDevices returns the devices that coding tools are watched on, by
// device ID.
func (s *Store) CodingDevices(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT device_id FROM coding_devices")
	if err != nil {
		return nil, fmt.Errorf("read the devices that coding tools are watched on: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read the devices that coding tools are watched on: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetCoding turns watching a device's coding tools on or off. What was
// counted stays.
func (s *Store) SetCoding(ctx context.Context, deviceID string, on bool) error {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO coding_devices (device_id) VALUES (?)", deviceID)
	} else {
		_, err = s.db.ExecContext(ctx, "DELETE FROM coding_devices WHERE device_id = ?", deviceID)
	}
	if err != nil {
		return fmt.Errorf("change whether coding tools are watched: %w", err)
	}
	return nil
}

// SaveCoding adds what a device's agent reported. Tokens and sessions are
// totals, so for each key it keeps the larger number of the one it has and the
// one it's given: a report that comes twice, or from an agent that started over,
// changes nothing. Anything from before oldest is left out, since it would be
// deleted at the next prune.
func (s *Store) SaveCoding(ctx context.Context, deviceID string, r CodingReport, oldest time.Time) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		usage, err := tx.PrepareContext(ctx, `
			INSERT INTO coding_usage (device_id, tool, session, model, speed, hour,
				input, cache_read, cache_write, output, reasoning, requests, cost_micro)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (device_id, tool, session, model, speed, hour) DO UPDATE SET
				input = MAX(input, excluded.input),
				cache_read = MAX(cache_read, excluded.cache_read),
				cache_write = MAX(cache_write, excluded.cache_write),
				output = MAX(output, excluded.output),
				reasoning = MAX(reasoning, excluded.reasoning),
				requests = MAX(requests, excluded.requests),
				cost_micro = MAX(cost_micro, excluded.cost_micro)`)
		if err != nil {
			return err
		}
		defer func() { _ = usage.Close() }()
		for _, u := range r.Usage {
			if u.Hour.Before(oldest) {
				continue
			}
			if _, err := usage.ExecContext(ctx, deviceID, u.Tool, u.Session, u.Model, u.Speed, u.Hour.UnixMilli(),
				u.Input, u.CacheRead, u.CacheWrite, u.Output, u.Reasoning, u.Requests, u.CostMicro); err != nil {
				return err
			}
		}

		for _, se := range r.Sessions {
			if se.Last.Before(oldest) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO coding_sessions (device_id, tool, session, project, first_at, last_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (device_id, tool, session) DO UPDATE SET
					project = CASE WHEN excluded.project != '' THEN excluded.project ELSE project END,
					first_at = MIN(first_at, excluded.first_at),
					last_at = MAX(last_at, excluded.last_at)`,
				deviceID, se.Tool, se.ID, se.Project, se.First.UnixMilli(), se.Last.UnixMilli()); err != nil {
				return err
			}
		}

		for _, l := range r.Limits {
			var resets sql.NullInt64
			if l.ResetsAt != nil {
				resets = sql.NullInt64{Int64: l.ResetsAt.UnixMilli(), Valid: true}
			}
			// A limit that was written earlier than the one Bield has is old news.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO coding_limits (device_id, tool, id, window_minutes, used, resets_at, status, plan, seen_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (device_id, tool, id) DO UPDATE SET
					window_minutes = excluded.window_minutes, used = excluded.used,
					resets_at = excluded.resets_at, status = excluded.status,
					plan = excluded.plan, seen_at = excluded.seen_at
				WHERE excluded.seen_at >= coding_limits.seen_at`,
				deviceID, l.Tool, l.ID, l.WindowMinutes, nullFloat(l.Used), resets, l.Status, l.Plan, l.Seen.UnixMilli()); err != nil {
				return err
			}
		}

		for _, t := range r.Tools {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO coding_tools (device_id, tool, status, at) VALUES (?, ?, ?, ?)
				ON CONFLICT (device_id, tool) DO UPDATE SET status = excluded.status, at = excluded.at`,
				deviceID, t.Tool, t.Status, t.At.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save what a device's coding tools used: %w", err)
	}
	return nil
}

// CodingUsageBetween returns the hours of tokens from..to, of one device or,
// if deviceID is "", of every device.
func (s *Store) CodingUsageBetween(ctx context.Context, deviceID string, from, to time.Time) ([]CodingUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, tool, session, model, speed, hour,
			input, cache_read, cache_write, output, reasoning, requests, cost_micro
		FROM coding_usage WHERE hour >= ?1 AND hour < ?2 AND (?3 = '' OR device_id = ?3)
		ORDER BY hour, device_id, tool, session, model, speed`,
		from.UnixMilli(), to.UnixMilli(), deviceID)
	if err != nil {
		return nil, fmt.Errorf("read what coding tools used: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CodingUsage
	for rows.Next() {
		var (
			u    CodingUsage
			hour int64
		)
		if err := rows.Scan(&u.DeviceID, &u.Tool, &u.Session, &u.Model, &u.Speed, &hour,
			&u.Input, &u.CacheRead, &u.CacheWrite, &u.Output, &u.Reasoning, &u.Requests, &u.CostMicro); err != nil {
			return nil, fmt.Errorf("read what coding tools used: %w", err)
		}
		u.Hour = time.UnixMilli(hour).UTC()
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read what coding tools used: %w", err)
	}
	return out, nil
}

// CodingSessionsSince returns the sessions of one device, or of every device
// if deviceID is "", that did anything since a time, the latest first, at
// most limit of them.
func (s *Store) CodingSessionsSince(ctx context.Context, deviceID string, since time.Time, limit int) ([]CodingSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, tool, session, project, first_at, last_at FROM coding_sessions
		WHERE last_at >= ?1 AND (?2 = '' OR device_id = ?2)
		ORDER BY last_at DESC, device_id, tool, session LIMIT ?3`,
		since.UnixMilli(), deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("read coding sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CodingSession
	for rows.Next() {
		var (
			se          CodingSession
			first, last int64
		)
		if err := rows.Scan(&se.DeviceID, &se.Tool, &se.ID, &se.Project, &first, &last); err != nil {
			return nil, fmt.Errorf("read coding sessions: %w", err)
		}
		se.First, se.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		out = append(out, se)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read coding sessions: %w", err)
	}
	return out, nil
}

// CodingLimits returns the limits of one device, or of every device if
// deviceID is "".
func (s *Store) CodingLimits(ctx context.Context, deviceID string) ([]CodingLimit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, tool, id, window_minutes, used, resets_at, status, plan, seen_at
		FROM coding_limits WHERE ?1 = '' OR device_id = ?1 ORDER BY device_id, tool, id`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("read coding limits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CodingLimit
	for rows.Next() {
		var (
			l      CodingLimit
			used   sql.NullFloat64
			resets sql.NullInt64
			seen   int64
		)
		if err := rows.Scan(&l.DeviceID, &l.Tool, &l.ID, &l.WindowMinutes, &used, &resets, &l.Status, &l.Plan, &seen); err != nil {
			return nil, fmt.Errorf("read coding limits: %w", err)
		}
		l.Used = floatPtr(used)
		if resets.Valid {
			l.ResetsAt = new(time.UnixMilli(resets.Int64).UTC())
		}
		l.Seen = time.UnixMilli(seen).UTC()
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read coding limits: %w", err)
	}
	return out, nil
}

// CodingTools returns whether the agent could read each tool's files, on one
// device or, if deviceID is "", on every device.
func (s *Store) CodingTools(ctx context.Context, deviceID string) ([]CodingToolStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, tool, status, at FROM coding_tools
		WHERE ?1 = '' OR device_id = ?1 ORDER BY device_id, tool`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("read which coding tools can be read: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CodingToolStatus
	for rows.Next() {
		var (
			t  CodingToolStatus
			at int64
		)
		if err := rows.Scan(&t.DeviceID, &t.Tool, &t.Status, &at); err != nil {
			return nil, fmt.Errorf("read which coding tools can be read: %w", err)
		}
		t.At = time.UnixMilli(at).UTC()
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read which coding tools can be read: %w", err)
	}
	return out, nil
}
