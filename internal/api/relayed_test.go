package api_test

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// A server that was direct and then answers through DERP for five pings in a
// row raises an alert. It stays open while the server is offline, or when
// the count starts again, as it does after Bield restarts, and resolves a
// minute after the server is direct again. A server that has never been
// direct, and a laptop, which is off by default, don't alert.
func TestFeed_relayedAlert(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		// The NAS and the laptop were direct an hour ago. The other server
		// never was.
		err := st.AddPings(t.Context(), []store.Ping{
			{DeviceID: "nas", At: time.Now().Add(-time.Hour), Replied: true, Direct: true},
			{DeviceID: "laptop", At: time.Now().Add(-time.Hour), Replied: true, Direct: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		status := func(nasOnline bool) *tailnet.Status {
			return &tailnet.Status{
				State: tailnet.StateRunning,
				Self:  tailnet.Device{ID: "bield", HostName: "bield", OS: "linux", Online: true},
				Peers: []tailnet.Device{
					{ID: "nas", HostName: "nas", OS: "linux", Online: nasOnline},
					{ID: "strict", HostName: "strict", OS: "linux", Online: true},
					{ID: "laptop", HostName: "macbook", OS: "macOS", Online: true},
				},
			}
		}
		derp := func(relayed int) tailnet.PingRecord {
			return tailnet.PingRecord{
				At: time.Now(), Result: &tailnet.PingResult{Latency: 40 * time.Millisecond, DERPRegion: "fra"}, Relayed: relayed,
			}
		}
		src := &fakeSources{status: status(true), statusAt: time.Now(), changed: make(chan struct{}, 1)}
		setPings := func(nas tailnet.PingRecord, online bool) {
			src.set(func(f *fakeSources) {
				f.status, f.statusAt = status(online), time.Now()
				f.pings = map[string]tailnet.PingRecord{"nas": nas, "strict": derp(9), "laptop": derp(9)}
			})
		}
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: src.tailnet, DockerChanged: src.changed, Alerts: alerts.NewEngine(st, nil), Store: st,
		})
		go feed.Run(t.Context())
		synctest.Wait()
		want := func(when string, alerts ...string) {
			t.Helper()
			if got := alertSummary(feed.Latest().Alerts); !slices.Equal(got, alerts) {
				t.Errorf("%s, alerts = %q, want %q", when, got, alerts)
			}
		}
		const open = "warning: nas is relayed through DERP (fra)"

		setPings(derp(api.RelayedAfter-1), true)
		time.Sleep(2 * time.Second)
		want("after four replies through DERP")

		setPings(derp(api.RelayedAfter), true)
		time.Sleep(2 * time.Second)
		want("after five", open)
		if a := feed.Latest().Alerts; len(a) == 1 && !strings.Contains(a[0].Detail, "last reached it directly 1h ago") {
			t.Errorf("the alert's detail = %q, want when the NAS was last direct", a[0].Detail)
		}

		setPings(derp(1), true)
		time.Sleep(2 * time.Minute)
		want("when the count starts again", open)

		setPings(derp(1), false)
		time.Sleep(6 * time.Minute)
		want("while the NAS is offline", open)

		direct := tailnet.PingRecord{At: time.Now(), Result: &tailnet.PingResult{Latency: 9 * time.Millisecond, Endpoint: "203.0.113.4:41641"}}
		setPings(direct, true)
		time.Sleep(30 * time.Second)
		want("30 seconds after it's direct again", open)
		setPings(direct, true)
		time.Sleep(time.Minute)
		want("a minute after")
		if d := findDevice(t, feed.Latest(), "nas"); d.Path.DirectAt == nil || !d.Path.DirectAt.Equal(direct.At) {
			t.Errorf("the NAS's DirectAt = %v, want the direct reply's time, %v", d.Path.DirectAt, direct.At)
		}
	})
}

func TestRelayedCondition(t *testing.T) {
	t.Parallel()
	now := time.Now()
	hourAgo, weekAgo := now.Add(-time.Hour), now.Add(-8*24*time.Hour)
	rtt := 40.0
	device := func(kind api.PathKind, directAt *time.Time) api.Device {
		return api.Device{
			ID: "nas", Name: "NAS", Online: true,
			Path: api.Path{Kind: kind, DERP: "fra", RTTMs: &rtt, DirectAt: directAt},
		}
	}
	for _, tt := range []struct {
		name    string
		d       api.Device
		relayed int
		open    bool
		want    string // the title, or "" for no alert
	}{
		{"five replies through DERP", device(api.PathKindDERP, &hourAgo), 5, false, "NAS is relayed through DERP (fra)"},
		{"through a peer relay", device(api.PathKindPeerRelay, &hourAgo), 5, false, "NAS is relayed through a peer relay"},
		{"four replies", device(api.PathKindDERP, &hourAgo), 4, false, ""},
		{"never direct", device(api.PathKindDERP, nil), 50, false, ""},
		{"direct over a week ago", device(api.PathKindDERP, &weekAgo), 50, false, ""},
		{"direct now", device(api.PathKindDirect, &hourAgo), 50, false, ""},
		{"open, after one reply", device(api.PathKindDERP, &hourAgo), 1, true, "NAS is relayed through DERP (fra)"},
		{"open, direct over a week ago", device(api.PathKindDERP, &weekAgo), 1, true, "NAS is relayed through DERP (fra)"},
		{"open, direct now", device(api.PathKindDirect, &hourAgo), 0, true, ""},
		{"no reply to the latest ping", func() api.Device {
			d := device(api.PathKindDERP, &hourAgo)
			d.Path.RTTMs = nil
			return d
		}(), 5, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, ok := api.RelayedCondition(tt.d, tt.relayed, tt.open, now)
			if got := map[bool]string{true: c.Title}[ok]; got != tt.want {
				t.Errorf("RelayedCondition() = %q, %v; want %q", c.Title, ok, tt.want)
			}
			if ok && (c.Severity != alerts.Warning || c.DeviceID != "nas") {
				t.Errorf("RelayedCondition() = %+v, want a warning for the NAS", c)
			}
		})
	}
}
