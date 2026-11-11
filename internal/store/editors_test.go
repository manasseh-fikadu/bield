package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestStore_Editors(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	ctx := t.Context()
	created := time.UnixMilli(1_700_000_000_000)
	laptop := store.Editor{DeviceID: "laptop", Port: 4710, Version: "4.145.2", CreatedAt: created, CreatedBy: "alice@example.com"}
	hub := store.Editor{DeviceID: store.HubID, Port: 4711, Version: "4.145.2", CreatedAt: created, CreatedBy: "bob@example.com"}
	for _, e := range []store.Editor{laptop, hub} {
		if err := s.SetEditor(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	get := func() map[string]store.Editor {
		t.Helper()
		got, err := s.Editors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := get(); len(got) != 2 || got["laptop"] != laptop || got[store.HubID] != hub {
		t.Fatalf("Editors = %+v, want the laptop's and the hub's", got)
	}

	// An update keeps the row, and another machine can't take a port.
	laptop.Version = "4.146.0"
	if err := s.SetEditor(ctx, laptop); err != nil {
		t.Fatal(err)
	}
	if got := get()["laptop"]; got != laptop {
		t.Errorf("after the update, %+v, want %+v", got, laptop)
	}
	vps := store.Editor{DeviceID: "vps", Port: 4710, Version: "4.145.2", CreatedAt: created}
	if err := s.SetEditor(ctx, vps); !errors.Is(err, store.ErrPortTaken) {
		t.Errorf("SetEditor on the laptop's port = %v, want ErrPortTaken", err)
	}

	if err := s.DeleteEditor(ctx, "laptop"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEditor(ctx, "laptop"); !errors.Is(err, store.ErrNoEditor) {
		t.Errorf("deleting it again = %v, want ErrNoEditor", err)
	}
	// Its port is free again.
	if err := s.SetEditor(ctx, vps); err != nil {
		t.Errorf("SetEditor on the freed port = %v", err)
	}
	if got := get(); len(got) != 2 || got["vps"] != vps || got[store.HubID] != hub {
		t.Errorf("Editors = %+v, want the hub's and the VPS's", got)
	}
}
