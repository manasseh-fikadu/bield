package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/api/apitest"
	"github.com/manasseh-fikadu/bield/internal/client"
)

// newClient returns a client for the test API that acts as the device at from.
func newClient(t *testing.T, s *apitest.Server, from netip.Addr) *client.Client {
	t.Helper()
	c, err := client.New(client.Options{URL: apitest.URL, Version: "9.9.9", HTTP: s.Client(from)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want, wantErr string }{
		{in: "bield.example.ts.net", want: "https://bield.example.ts.net"},
		{in: "https://bield.example.ts.net/", want: "https://bield.example.ts.net"},
		{in: "http://100.64.0.1:4700/some/path?x=1", want: "http://100.64.0.1:4700"},
		{in: "  bield  ", want: "https://bield"},
		{in: "", wantErr: "no Bield address"},
		{in: "ftp://bield", wantErr: "isn't a Bield address"},
		{in: "https://user:pw@bield", wantErr: "isn't a Bield address"},
		{in: "https://", wantErr: "isn't a Bield address"},
	} {
		u, err := client.ParseURL(tt.in)
		switch {
		case tt.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseURL(%q) error = %v, want one with %q", tt.in, err, tt.wantErr)
			}
		case err != nil || u.String() != tt.want:
			t.Errorf("ParseURL(%q) = %v, %v, want %s", tt.in, u, err, tt.want)
		}
	}
}

func TestClient_whoIsAsking(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	for _, tt := range []struct {
		name      string
		from      netip.Addr
		want      api.Me
		wantSnap  bool
		wantAdmin bool
	}{
		{"an admin", apitest.LaptopIP, api.Me{SignedIn: true, Login: apitest.Alice, Name: "", Device: "alice-laptop.example.ts.net", Role: api.RoleAdmin}, true, true},
		{"a viewer", apitest.PhoneIP, api.Me{SignedIn: true, Login: apitest.Bob, Device: "bob-phone.example.ts.net", Role: api.RoleViewer}, true, false},
		{"someone with no role", apitest.TabletIP, api.Me{SignedIn: true, Login: apitest.Carol, Device: "carol-tablet.example.ts.net"}, false, false},
		{"a tagged server", apitest.ServerIP, api.Me{Reason: api.ReasonTaggedDevice}, false, false},
		{"a stranger", apitest.StrangerIP, api.Me{Reason: api.ReasonUnknownDevice}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(t, s, tt.from)
			me, err := c.Me(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if me.SignedIn != tt.want.SignedIn || me.Login != tt.want.Login || me.Role != tt.want.Role || me.Reason != tt.want.Reason {
				t.Errorf("Me() = %+v, want %+v", me, tt.want)
			}
			snap, err := c.Snapshot(t.Context())
			if (err == nil) != tt.wantSnap {
				t.Fatalf("Snapshot() error = %v, want an error: %v", err, !tt.wantSnap)
			}
			if err == nil && len(snap.Devices) != 5 {
				t.Errorf("Snapshot() has %d devices, want 5", len(snap.Devices))
			}
			if err == nil && (snap.Hub.Tailnet != "") != tt.wantAdmin {
				t.Errorf("Snapshot().Hub.Tailnet = %q, want it only for an admin", snap.Hub.Tailnet)
			}
		})
	}
}

// A refusal says why the caller is refused, in words that tell them what to do.
func TestClient_explain(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	for _, tt := range []struct {
		name string
		from netip.Addr
		want string
	}{
		{"a viewer", apitest.PhoneIP, "bob@example.com, a viewer; this needs an admin"},
		{"someone with no role", apitest.TabletIP, "no role in Bield; ask one of its admins"},
		{"a tagged server", apitest.ServerIP, "tagged in Tailscale"},
		{"a stranger", apitest.StrangerIP, "Tailscale doesn't know the machine"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(t, s, tt.from)
			// Changing settings needs an admin.
			_, err := c.ChangeSettings(t.Context(), api.ChangeSettings{})
			var e *client.Error
			if !errors.As(err, &e) || e.Status != http.StatusForbidden || !e.Denied() {
				t.Fatalf("ChangeSettings() error = %v, want a 403", err)
			}
			err = c.Explain(t.Context(), err)
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("explained error = %q, want it to say %q", err, tt.want)
			}
			if got := e.Message; strings.Contains(got, tt.want) {
				t.Errorf("Bield's own message %q already has the hint", got)
			}
		})
	}
	admin := newClient(t, s, apitest.LaptopIP)
	if _, err := admin.ChangeSettings(t.Context(), api.ChangeSettings{}); err != nil {
		t.Errorf("an admin's ChangeSettings() = %v", err)
	}
	// An error that isn't a refusal comes back as it is.
	_, err := admin.Alert(t.Context(), 999)
	var e *client.Error
	if !errors.As(err, &e) || !e.NotFound() || admin.Explain(t.Context(), err) != err {
		t.Errorf("Alert(999) error = %v, want a 404 that Explain leaves as it is", err)
	}
}

func TestClient_unreachable(t *testing.T) {
	t.Parallel()
	c, err := client.New(client.Options{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Health(t.Context())
	var un *client.UnreachableError
	if !errors.As(err, &un) || !strings.Contains(err.Error(), "can't reach Bield at http://127.0.0.1:1") {
		t.Errorf("Health() error = %v, want it to say Bield is unreachable", err)
	}
}

// Changes made through the client show up in the activity log as coming from
// the command line, for the person who made them.
func TestClient_alerts(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	s.Alerts.Apply(t.Context(), []alerts.Condition{{
		Key: "disk:bield", Source: "metrics", DeviceID: "bield", Severity: alerts.Warning, Title: "Disk 90% full on bield",
	}}, func(string, string, string) bool { return true }, time.Now())
	id := s.Alerts.Open()[0].ID
	admin, viewer := newClient(t, s, apitest.LaptopIP), newClient(t, s, apitest.PhoneIP)
	ctx := t.Context()

	if _, err := viewer.AckAlert(ctx, id); err == nil {
		t.Fatal("a viewer acknowledged an alert")
	}
	a, err := admin.AckAlert(ctx, id)
	if err != nil || a.State != api.AlertStateAcknowledged || a.AckedBy != apitest.Alice {
		t.Fatalf("AckAlert() = %+v, %v, want it acknowledged by alice", a, err)
	}
	if a, err = admin.SnoozeAlert(ctx, id, 30); err != nil || a.State != api.AlertStateSnoozed {
		t.Fatalf("SnoozeAlert() = %+v, %v", a, err)
	}
	if a, err = admin.WakeAlert(ctx, id); err != nil || a.State == api.AlertStateSnoozed {
		t.Fatalf("WakeAlert() = %+v, %v", a, err)
	}
	if n, err := admin.NoteAlert(ctx, id, "On it"); err != nil || n.Text != "On it" || n.By != apitest.Alice {
		t.Fatalf("NoteAlert() = %+v, %v", n, err)
	}
	if a, err = admin.UnackAlert(ctx, id); err != nil || a.State != api.AlertStateOpen {
		t.Fatalf("UnackAlert() = %+v, %v", a, err)
	}
	got, err := viewer.Alert(ctx, id)
	if err != nil || got.Title != "Disk 90% full on bield" || len(got.Notes) != 1 || got.Notes[0].By != "" {
		t.Errorf("a viewer's Alert() = %+v, %v, want the alert and its note, without who wrote it", got, err)
	}

	events, err := admin.Events(ctx, client.EventsQuery{Kind: "alert", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var byCLI int
	for _, e := range events.Events {
		if e.Actor == apitest.Alice {
			if e.Via != api.ViaCLI {
				t.Errorf("event %q by alice has via %q, want %q", e.Message, e.Via, api.ViaCLI)
			}
			byCLI++
		}
	}
	if byCLI != 5 {
		t.Errorf("%d events by alice from the CLI, want 5: %+v", byCLI, events.Events)
	}
}

func TestClient_watch(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	var firstDevices int
	stop := errors.New("enough")
	err := newClient(t, s, apitest.PhoneIP).Watch(ctx, func(snap api.Snapshot) error {
		firstDevices = len(snap.Devices)
		return stop
	})
	if !errors.Is(err, stop) || firstDevices != 5 {
		t.Errorf("Watch() = %v with %d devices, want the first snapshot, and fn's error", err, firstDevices)
	}
	// A caller who can't view Bield is told so, not retried.
	err = newClient(t, s, apitest.ServerIP).Watch(ctx, func(api.Snapshot) error { return nil })
	var e *client.Error
	if !errors.As(err, &e) || e.Status != http.StatusForbidden {
		t.Errorf("a tagged server's Watch() = %v, want a 403", err)
	}
}
