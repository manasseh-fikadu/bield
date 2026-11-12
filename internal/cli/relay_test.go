package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/cli"
)

// relayBield is a Bield that answers GET /api/snapshot with snap.
func relayBield(t *testing.T, snap api.Snapshot) func(args ...string) result {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/snapshot" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			t.Errorf("encode the snapshot: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return func(args ...string) result {
		var stdout, stderr syncBuffer
		e := cli.Env{
			Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Version: "9.9.9",
			Getenv: func(k string) string { return map[string]string{"BIELD_URL": srv.URL}[k] },
		}
		code := cli.Run(t.Context(), e, args)
		return result{stdout.String(), stderr.String(), code}
	}
}

func TestRelay(t *testing.T) {
	t.Parallel()
	rtt := func(ms float64) *float64 { return &ms }
	devices := []api.Device{
		{ID: "hub", Name: "Bield", OS: "linux", Kind: api.DeviceKindServer, Self: true, IPs: []string{"100.64.0.1"}},
		{
			ID: "fra", Name: "Frankfurt VPS", OS: "linux", Kind: api.DeviceKindServer, IPs: []string{"100.64.0.2", "fd7a:115c:a1e0::2"},
			Path: api.Path{Kind: api.PathKindDirect, RTTMs: rtt(12)}, Metrics: &api.Metrics{Uptime: 41 * 86400},
		},
		{ID: "mac", Name: "MacBook", OS: "macOS", Kind: api.DeviceKindLaptop, IPs: []string{"100.64.0.3"}, Path: api.Path{Kind: api.PathKindDirect, RTTMs: rtt(30)}},
		{ID: "nyc", Name: "New York VPS", OS: "linux", Kind: api.DeviceKindServer, IPs: []string{"100.64.0.4"}, Path: api.Path{Kind: api.PathKindDERP, DERP: "nyc", RTTMs: rtt(84)}},
		{ID: "phone", Name: "Phone", OS: "android", Kind: api.DeviceKindPhone, IPs: []string{"100.64.0.5"}, Path: api.Path{Kind: api.PathKindDERP, DERP: "fra", RTTMs: rtt(150)}},
	}
	stuck := api.Snapshot{Devices: devices, Relay: api.Relay{
		Supported: true, DERP: []string{"nyc", "phone"},
		Candidates: []api.RelayCandidate{{DeviceID: "fra", PublicAddress: true}, {DeviceID: "mac"}},
	}}
	run := relayBield(t, stuck)

	r := run("relay")
	r.want(t, 0, "Bield reaches 2 devices through DERP", "")
	for _, want := range []string{
		"New York VPS  relay nyc 84ms",
		"Suggested: Frankfurt VPS (server, public address, direct 12ms, up 41d)",
		"Also possible: MacBook",
		"1. On Frankfurt VPS, open UDP port 40000",
		"sudo tailscale set --relay-server-port=40000",
		`"src": ["100.64.0.4", "100.64.0.5"],`,
		`"dst": ["100.64.0.2"],`,
		"Bield only sees its own connections",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("bield relay printed\n%s\nwant it to have %q", r.stdout, want)
		}
	}

	r = run("relay", "--json", "--via", "MacBook", "--port", "41000")
	r.want(t, 0, "", "")
	var advice cli.RelayAdvice
	if err := json.Unmarshal([]byte(r.stdout), &advice); err != nil {
		t.Fatalf("bield relay --json printed %q: %v", r.stdout, err)
	}
	wantGrant := "{\n  \"src\": [\"100.64.0.4\", \"100.64.0.5\"],\n  \"dst\": [\"100.64.0.3\"],\n  \"app\": { \"tailscale.com/cap/relay\": [] }\n}"
	if advice.Relay == nil || advice.Relay.ID != "mac" || advice.Port != 41000 ||
		advice.Command != "tailscale set --relay-server-port=41000" || advice.Grant != wantGrant ||
		len(advice.DERP) != 2 || len(advice.Candidates) != 2 {
		t.Errorf("bield relay --json --via MacBook --port 41000 = %+v, want the MacBook, without sudo, on 41000", advice)
	}

	// A device on DERP can be the relay, but not one for itself.
	r = run("relay", "--json", "--via", "nyc")
	r.want(t, 0, `[\"100.64.0.5\"]`, "")

	run("relay", "--via", "Phone").want(t, 2, "", "Phone can't be a peer relay")
	run("relay", "--via", "Bield").want(t, 2, "", "Bield's own node")
	run("relay", "--via", "nowhere").want(t, 3, "", `no device is called "nowhere"`)
	run("relay", "--port", "0").want(t, 2, "", "--port is a UDP port from 1 to 65535")
}

func TestRelay_nothingToDo(t *testing.T) {
	t.Parallel()
	rtt := 12.0
	fra := api.Device{ID: "fra", Name: "Frankfurt VPS", OS: "linux", Kind: api.DeviceKindServer, IPs: []string{"100.64.0.2"}, Path: api.Path{Kind: api.PathKindDirect, RTTMs: &rtt}}
	for _, tc := range []struct {
		name  string
		relay api.Relay
		want  string
	}{
		{"every device is direct", api.Relay{Supported: true, Candidates: []api.RelayCandidate{{DeviceID: "fra"}}}, "the best machine to relay for it is Frankfurt VPS"},
		{"no device fits", api.Relay{Supported: true, DERP: []string{"fra"}}, "No device fits"},
		{"Headscale", api.Relay{DERP: []string{"fra"}}, "peer relays need Tailscale's"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := relayBield(t, api.Snapshot{Devices: []api.Device{fra}, Relay: tc.relay})
			r := run("relay")
			r.want(t, 0, tc.want, "")
			if strings.Contains(r.stdout, "tailscale set") {
				t.Errorf("bield relay printed the steps when there's nothing to set up:\n%s", r.stdout)
			}
		})
	}
}
