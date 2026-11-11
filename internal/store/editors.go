package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoEditor means VS Code isn't installed on the machine, as far as Bield
// knows.
var ErrNoEditor = errors.New("VS Code isn't installed on this machine")

// ErrPortTaken means another machine's editor has the port.
var ErrPortTaken = errors.New("another machine's editor has this port")

// Editor is a machine an admin installed code-server on, and the port of
// Bield's node its editor is served on.
type Editor struct {
	// DeviceID is the ID its SSH access is kept under: the device's ID, or
	// HubID for the machine Bield runs on.
	DeviceID string
	Port     int
	// Version is the code-server version Bield installed last.
	Version string
	// CreatedAt is when it was first installed, and CreatedBy the login of
	// the admin who did.
	CreatedAt time.Time
	CreatedBy string
}

// Editors returns every machine that has an editor installed, by device ID.
func (s *Store) Editors(ctx context.Context) (map[string]Editor, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT device_id, port, version, created_at, created_by FROM editors")
	if err != nil {
		return nil, fmt.Errorf("read editors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Editor{}
	for rows.Next() {
		var (
			e       Editor
			created int64
		)
		if err := rows.Scan(&e.DeviceID, &e.Port, &e.Version, &created, &e.CreatedBy); err != nil {
			return nil, fmt.Errorf("read editors: %w", err)
		}
		e.CreatedAt = time.UnixMilli(created)
		out[e.DeviceID] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read editors: %w", err)
	}
	return out, nil
}

// SetEditor saves a machine's editor, replacing what was saved for it. It
// returns ErrPortTaken if another machine's editor has e.Port.
func (s *Store) SetEditor(ctx context.Context, e Editor) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var other string
		err := tx.QueryRowContext(ctx, "SELECT device_id FROM editors WHERE port = ? AND device_id != ?",
			e.Port, e.DeviceID).Scan(&other)
		switch {
		case err == nil:
			return ErrPortTaken
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO editors (device_id, port, version, created_at, created_by) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (device_id) DO UPDATE SET port = excluded.port, version = excluded.version,
				created_at = excluded.created_at, created_by = excluded.created_by`,
			e.DeviceID, e.Port, e.Version, e.CreatedAt.UnixMilli(), e.CreatedBy)
		return err
	})
	if err != nil && !errors.Is(err, ErrPortTaken) {
		return fmt.Errorf("save an editor: %w", err)
	}
	return err
}

// DeleteEditor forgets a machine's editor. It returns ErrNoEditor if none
// was saved for it.
func (s *Store) DeleteEditor(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM editors WHERE device_id = ?", deviceID)
	if err != nil {
		return fmt.Errorf("remove an editor: %w", err)
	}
	return noRow(res, ErrNoEditor)
}
