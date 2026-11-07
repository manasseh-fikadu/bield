// Package apitest runs Bield's API on a fake tailnet, so tests of its clients
// (the command line, the MCP server) can talk to the real handlers, with the
// real checks on who is asking, and never need a tailnet.
//
// The tailnet has Bield's node, alice's laptop (alice is an admin), bob's phone
// (bob is a viewer), carol's tablet (carol has no role) and a tagged server.
package apitest

import (
	"context"
	"net/http"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/agents"
	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/checks"
	"github.com/manasseh-fikadu/bield/internal/docker"
	"github.com/manasseh-fikadu/bield/internal/notify"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
	"github.com/manasseh-fikadu/bield/internal/usage"
)

// The logins on the test tailnet.
const (
	Alice = "alice@example.com" // an admin
	Bob   = "bob@example.com"   // a viewer
	Carol = "carol@example.com" // no role
)

// URL is where the app is, as clients should call it.
const URL = tailnettest.URL

// The addresses of the devices on the test tailnet. Client(from) sends
// requests as the device at that address.
var (
	BieldIP    = netip.MustParseAddr("100.64.0.1")
	LaptopIP   = netip.MustParseAddr("100.64.0.2") // alice's
	PhoneIP    = netip.MustParseAddr("100.64.0.3") // bob's
	ServerIP   = netip.MustParseAddr("100.64.0.4") // tagged
	TabletIP   = netip.MustParseAddr("100.64.0.5") // carol's
	StrangerIP = netip.MustParseAddr("100.64.0.99")
)

// A Server is the API on the test tailnet.
type Server struct {
	Tailnet *tailnettest.Tailnet
	Store   *store.Store
	Alerts  *alerts.Engine
	Feed    *api.Feed
	Checks  *checks.Runner
	Options api.Options
}

// New starts the API for the length of the test. Each function in change can
// change the options before it starts, such as to fake Docker.
func New(t testing.TB, change ...func(*api.Options)) *Server {
	t.Helper()
	ctx := t.Context()
	tn := tailnettest.New(tailnet.Status{
		State:   tailnet.StateRunning,
		Tailnet: "example.com",
		Self: tailnet.Device{
			ID: "bield", HostName: "bield", DNSName: "bield.example.ts.net", IPs: []netip.Addr{BieldIP}, Owner: Alice, Online: true, OS: "linux",
		},
		Peers: []tailnet.Device{
			{ID: "laptop", HostName: "alice-laptop", DNSName: "alice-laptop.example.ts.net", IPs: []netip.Addr{LaptopIP}, Owner: Alice, Online: true, OS: "macOS"},
			{ID: "phone", HostName: "bob-phone", DNSName: "bob-phone.example.ts.net", IPs: []netip.Addr{PhoneIP}, Owner: Bob, Online: true, OS: "android"},
			{ID: "server", HostName: "web-1", DNSName: "web-1.example.ts.net", IPs: []netip.Addr{ServerIP}, Tags: []string{"tag:server"}, Online: true, OS: "linux"},
			{ID: "tablet", HostName: "carol-tablet", DNSName: "carol-tablet.example.ts.net", IPs: []netip.Addr{TabletIP}, Owner: Carol, Online: true, OS: "iOS"},
		},
	})
	t.Cleanup(func() { _ = tn.Close() })

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "bield.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for login, role := range map[string]store.Role{Alice: store.Admin, Bob: store.Viewer} {
		if _, err := st.SetRole(ctx, login, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetWhoCanView(ctx, store.Listed); err != nil {
		t.Fatal(err)
	}

	snapshot := func() tailnet.Snapshot {
		status, err := tn.Status(context.Background())
		return tailnet.Snapshot{Status: status, StatusErr: err}
	}
	prefs := settings.NewCache(st, nil)
	engine := alerts.NewEngine(st, nil)
	runner := checks.NewRunner(st, checks.Options{Dial: tn.Dial})
	reg := agents.NewRegistry(st, nil)
	fleet := docker.NewFleet(nil, nil)
	t.Cleanup(fleet.Close)
	feed := api.NewFeed(api.FeedOptions{
		Tailnet: snapshot, Agents: reg, Checks: runner, Alerts: engine, Store: st, Settings: prefs,
	})
	opts := api.Options{
		Version:  "v0.0.0-test",
		WhoIs:    tn.WhoIs,
		Snapshot: snapshot,
		Feed:     feed,
		Store:    st,
		Agents:   reg,
		Usage:    usage.NewRecorder(st, nil),
		Checks:   runner,
		Alerts:   engine,
		SSH:      ssh.NewManager(ssh.Options{KeyDir: t.TempDir(), Store: st, Tailnet: snapshot, Dial: tn.Dial}),
		Docker:   fleet,
		Settings: prefs,
		Notify:   notify.NewDispatcher(notify.Options{Store: st, Open: engine.Open, Settings: prefs}),
	}
	for _, c := range change {
		c(&opts)
	}

	go feed.Run(ctx)
	srv := &http.Server{Handler: api.New(opts), ReadHeaderTimeout: 10 * time.Second}
	ln, _, err := tn.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return &Server{Tailnet: tn, Store: st, Alerts: engine, Feed: feed, Checks: runner, Options: opts}
}

// Client returns an HTTP client whose requests reach the API as if the device
// at from sent them, so Bield sees whoever owns it.
func (s *Server) Client(from netip.Addr) *http.Client { return s.Tailnet.Client(from) }
