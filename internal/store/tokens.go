package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TokenPrefix starts every API token, so Bield can tell one from an agent's
// secret, and so can a secret scanner.
const TokenPrefix = "bield_"

// tokenHintLen is how many of a token's last characters the store keeps.
const tokenHintLen = 4

// tokenTouchEvery is how often a token's last use is written down: each use
// would be a write per request.
const tokenTouchEvery = time.Minute

// Why the store can't find or make an API token.
var (
	// ErrUnknownToken means Bield didn't make the token, or it was revoked.
	ErrUnknownToken = errors.New("unknown API token")
	// ErrTokenExpired means the token expired.
	ErrTokenExpired = errors.New("the API token expired")
	// ErrTokenName means another token has the name.
	ErrTokenName = errors.New("another API token has this name")
)

// An APIToken is a token that lets a caller use the API with a role, without
// Tailscale saying who they are. The token itself is shown once, when it's
// made, and the store keeps only its hash.
type APIToken struct {
	ID   int64
	Name string
	// Hint is the token's last few characters.
	Hint string
	// Role is the most the token may do. It does less if its maker's own role
	// is lower now.
	Role Role
	// CreatedBy is the login of the admin who made it.
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	// LastUsedAt is when the token was last used, to the minute, or zero.
	LastUsedAt time.Time
	// LastUsedNode is the node ID of the machine it was last used from, if
	// Tailscale knew it.
	LastUsedNode string
}

// Expired reports whether t has expired at now.
func (t APIToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

// NewToken makes an API token called name with role, for the admin with login
// by, that works until expires. It returns the token, which the store doesn't
// keep, and what it does keep. It returns ErrTokenName if another token has
// the name, whatever its case.
func (s *Store) NewToken(ctx context.Context, name string, role Role, by string, now, expires time.Time) (string, APIToken, error) {
	if role != Admin && role != Viewer {
		return "", APIToken{}, fmt.Errorf("unknown role %q", role)
	}
	secret := TokenPrefix + rand.Text()
	t := APIToken{
		Name: name, Hint: secret[len(secret)-tokenHintLen:], Role: role, CreatedBy: by,
		CreatedAt: time.UnixMilli(now.UnixMilli()), ExpiresAt: time.UnixMilli(expires.UnixMilli()),
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO api_tokens (name, token_hash, hint, role, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		t.Name, hash(secret), t.Hint, string(t.Role), t.CreatedBy, t.CreatedAt.UnixMilli(), t.ExpiresAt.UnixMilli()).Scan(&t.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: api_tokens.name") {
			return "", APIToken{}, ErrTokenName
		}
		return "", APIToken{}, fmt.Errorf("make an API token: %w", err)
	}
	return secret, t, nil
}

// tokenColumns are the columns scanToken reads, in order.
//
//nolint:gosec // G101: column names, not a credential.
const tokenColumns = "id, name, hint, role, created_by, created_at, expires_at, last_used_at, last_used_node"

type scanner interface{ Scan(dest ...any) error }

func scanToken(row scanner) (APIToken, error) {
	var (
		t                APIToken
		created, expires int64
		lastUsed         sql.NullInt64
		lastNode         sql.NullString
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Hint, &t.Role, &t.CreatedBy, &created, &expires, &lastUsed, &lastNode); err != nil {
		return APIToken{}, err
	}
	t.CreatedAt, t.ExpiresAt = time.UnixMilli(created), time.UnixMilli(expires)
	if lastUsed.Valid {
		t.LastUsedAt = time.UnixMilli(lastUsed.Int64)
	}
	t.LastUsedNode = lastNode.String
	return t, nil
}

// Tokens returns every API token, expired ones too, by name.
func (s *Store) Tokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+tokenColumns+" FROM api_tokens ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("list API tokens: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list API tokens: %w", err)
	}
	return out, nil
}

// TokenBySecret returns the API token secret is, if it works at now.
// Otherwise it returns ErrUnknownToken, or ErrTokenExpired with the token.
func (s *Store) TokenBySecret(ctx context.Context, secret string, now time.Time) (APIToken, error) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return APIToken{}, ErrUnknownToken
	}
	return s.tokenWhere(ctx, "token_hash = ?", hash(secret), now)
}

// Token returns the API token with id, if it works at now. It returns the
// same errors as TokenBySecret.
func (s *Store) Token(ctx context.Context, id int64, now time.Time) (APIToken, error) {
	return s.tokenWhere(ctx, "id = ?", id, now)
}

func (s *Store) tokenWhere(ctx context.Context, where string, arg any, now time.Time) (APIToken, error) {
	t, err := scanToken(s.db.QueryRowContext(ctx, "SELECT "+tokenColumns+" FROM api_tokens WHERE "+where, arg))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return APIToken{}, ErrUnknownToken
	case err != nil:
		return APIToken{}, fmt.Errorf("look up an API token: %w", err)
	case t.Expired(now):
		return t, ErrTokenExpired
	}
	return t, nil
}

// TouchToken notes that the token with id was used at now, from the machine
// with node ID node, if it hasn't been noted in the last minute. A token used
// from two machines at once shows one of them.
func (s *Store) TouchToken(ctx context.Context, id int64, node string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET last_used_at = ?, last_used_node = ?
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now.UnixMilli(), nullString(node), id, now.Add(-tokenTouchEvery).UnixMilli())
	if err != nil {
		return fmt.Errorf("note an API token's use: %w", err)
	}
	return nil
}

// DeleteToken deletes the API token with id, and returns it, or
// ErrUnknownToken if there's none.
func (s *Store) DeleteToken(ctx context.Context, id int64) (APIToken, error) {
	t, err := scanToken(s.db.QueryRowContext(ctx, "DELETE FROM api_tokens WHERE id = ? RETURNING "+tokenColumns, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return APIToken{}, ErrUnknownToken
	case err != nil:
		return APIToken{}, fmt.Errorf("revoke an API token: %w", err)
	}
	return t, nil
}
