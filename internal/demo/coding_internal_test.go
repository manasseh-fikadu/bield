package demo

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// The demo's coding history has sessions on the machines that have the tools,
// one of them going, every model priced, and the same history every time.
func TestSeed_coding(t *testing.T) {
	t.Parallel()
	d := New(Options{Devices: 12, Listen: "127.0.0.1:0"})
	t.Cleanup(d.Close)
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "bield.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const span = 7 * 24 * time.Hour
	if err := d.seedCoding(t.Context(), st, span); err != nil {
		t.Fatal(err)
	}

	on, err := st.CodingDevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"macbook", "work-mac", "desktop", "vps-fra"} {
		found := false
		for _, m := range d.machines {
			if m.host == host {
				found = true
				if !on[historyID(m)] {
					t.Errorf("coding tools aren't watched on %s", host)
				}
			}
		}
		if !found {
			t.Errorf("the cast has no %s", host)
		}
	}
	if on[store.HubID] {
		t.Error("coding tools are watched on the hub")
	}

	rows, err := st.CodingUsageBetween(t.Context(), "", d.start.Add(-span-time.Hour), d.start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 50 {
		t.Fatalf("%d hours of usage in a week, want many more", len(rows))
	}
	prices := pricing.Default()
	tools := map[string]bool{}
	for _, u := range rows {
		tools[u.Tool] = true
		if _, ok := prices.Lookup(u.Model); !ok {
			t.Fatalf("the demo uses %s, which has no price", u.Model)
		}
		if u.Hour.After(d.start) || u.Reasoning > u.Output || u.Input <= 0 || u.Requests <= 0 {
			t.Fatalf("a made-up hour of usage is wrong: %+v", u)
		}
	}
	for _, tool := range []string{"claude", "codex", "opencode", "pi"} {
		if !tools[tool] {
			t.Errorf("no usage by %s", tool)
		}
	}

	sessions, err := st.CodingSessionsSince(t.Context(), "", d.start.Add(-span), 10000)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, s := range sessions {
		if s.Last.Before(s.First) || s.Last.After(d.start) {
			t.Errorf("a made-up session is wrong: %+v", s)
		}
		if d.start.Sub(s.Last) <= 5*time.Minute {
			active++
		}
	}
	if active != 1 {
		t.Errorf("%d sessions are active at the start, want 1", active)
	}

	limits, _ := st.CodingLimits(t.Context(), "")
	if len(limits) < 3 {
		t.Errorf("limits = %+v", limits)
	}
}
