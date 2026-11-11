package store_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestStore_tokens(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	ctx := t.Context()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

	secret, tok, err := s.NewToken(ctx, "ci", store.Viewer, "alice@example.com", now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	if !strings.HasPrefix(secret, store.TokenPrefix) || len(secret) < len(store.TokenPrefix)+26 {
		t.Errorf("NewToken() token = %q, want %s and 26 random characters", secret, store.TokenPrefix)
	}
	if !strings.HasSuffix(secret, tok.Hint) || len(tok.Hint) != 4 {
		t.Errorf("Hint = %q, want the token's last 4 characters", tok.Hint)
	}
	if _, _, err := s.NewToken(ctx, "CI", store.Admin, "alice@example.com", now, now.Add(time.Hour)); !errors.Is(err, store.ErrTokenName) {
		t.Errorf("NewToken() with a name in use = %v, want ErrTokenName", err)
	}
	if _, _, err := s.NewToken(ctx, "x", "owner", "alice@example.com", now, now.Add(time.Hour)); err == nil {
		t.Error("NewToken() with an unknown role = nil error")
	}

	got, err := s.TokenBySecret(ctx, secret, now)
	if err != nil || got.ID != tok.ID || got.Role != store.Viewer || got.CreatedBy != "alice@example.com" {
		t.Fatalf("TokenBySecret() = %+v, %v; want token %d", got, err, tok.ID)
	}
	for _, wrong := range []string{"", "bield_", secret + "x", strings.TrimPrefix(secret, store.TokenPrefix)} {
		if _, err := s.TokenBySecret(ctx, wrong, now); !errors.Is(err, store.ErrUnknownToken) {
			t.Errorf("TokenBySecret(%q) = %v, want ErrUnknownToken", wrong, err)
		}
	}
	if _, err := s.TokenBySecret(ctx, secret, now.Add(24*time.Hour)); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("TokenBySecret() when it expires = %v, want ErrTokenExpired", err)
	}
	if _, err := s.Token(ctx, tok.ID, now.Add(25*time.Hour)); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("Token() after it expired = %v, want ErrTokenExpired", err)
	}

	// Uses are written down once a minute, whatever the machine.
	touch := func(node string, at time.Time) {
		t.Helper()
		if err := s.TouchToken(ctx, tok.ID, node, at); err != nil {
			t.Fatal(err)
		}
	}
	last := func() (time.Time, string) {
		t.Helper()
		got, err := s.Token(ctx, tok.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		return got.LastUsedAt, got.LastUsedNode
	}
	touch("server", now)
	touch("server", now.Add(30*time.Second))
	if at, node := last(); !at.Equal(now) || node != "server" {
		t.Errorf("last use = %v from %q, want %v from server", at, node, now)
	}
	touch("laptop", now.Add(40*time.Second))
	if at, node := last(); !at.Equal(now) || node != "server" {
		t.Errorf("last use from another machine within the minute = %v from %q, want it left", at, node)
	}
	touch("laptop", now.Add(2*time.Minute))
	if at, node := last(); !at.Equal(now.Add(2*time.Minute)) || node != "laptop" {
		t.Errorf("last use a minute later = %v from %q, want it noted", at, node)
	}

	if _, _, err := s.NewToken(ctx, "backup", store.Admin, "alice@example.com", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, err := s.Tokens(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "backup" || list[1].Name != "ci" {
		t.Fatalf("Tokens() = %+v, %v; want backup and ci", list, err)
	}

	gone, err := s.DeleteToken(ctx, tok.ID)
	if err != nil || gone.Name != "ci" {
		t.Fatalf("DeleteToken() = %+v, %v", gone, err)
	}
	if _, err := s.TokenBySecret(ctx, secret, now); !errors.Is(err, store.ErrUnknownToken) {
		t.Errorf("TokenBySecret() after DeleteToken = %v, want ErrUnknownToken", err)
	}
	if _, err := s.DeleteToken(ctx, tok.ID); !errors.Is(err, store.ErrUnknownToken) {
		t.Errorf("DeleteToken() twice = %v, want ErrUnknownToken", err)
	}
	// The name is free again.
	if _, _, err := s.NewToken(ctx, "ci", store.Viewer, "alice@example.com", now, now.Add(time.Hour)); err != nil {
		t.Errorf("NewToken() with a revoked token's name = %v", err)
	}
}

func TestStore_eventToken(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	ctx := store.WithToken(store.WithVia(t.Context(), "cli"), "ci")
	now := time.Now()
	err := s.AddEvents(ctx,
		store.Event{At: now, Level: "info", Kind: "check", Message: "Added a check", Actor: "alice@example.com"},
		store.Event{At: now, Level: "info", Kind: "device", Message: "A device joined"},
	)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(t.Context(), store.EventQuery{Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("Events() = %v, %v", events, err)
	}
	for _, e := range events {
		want := ""
		if e.Actor != "" {
			want = "ci"
		}
		if e.Token != want {
			t.Errorf("event %q has token %q, want %q", e.Message, e.Token, want)
		}
	}
}
