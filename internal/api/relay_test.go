package api_test

import (
	"reflect"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func TestNewRelay(t *testing.T) {
	t.Parallel()
	rtt := func(ms float64) *float64 { return &ms }
	// dev is an online device that answered a ping from endpoint, "" for a
	// path through a relay.
	dev := func(id string, kind api.DeviceKind, path api.PathKind, endpoint string, ms float64) api.Device {
		return api.Device{
			ID: id, OS: "linux", Kind: kind, Online: true,
			Path: api.Path{Kind: path, Endpoint: endpoint, RTTMs: rtt(ms)},
		}
	}
	upFor := func(d api.Device, seconds int64) api.Device {
		d.Metrics = &api.Metrics{Uptime: seconds}
		return d
	}
	self := api.Device{ID: "self", OS: "linux", Kind: api.DeviceKindServer, Online: true, Self: true, Path: api.Path{Kind: api.PathKindSelf}}
	stuck := dev("stuck", api.DeviceKindServer, api.PathKindDERP, "", 90)
	vps := dev("vps", api.DeviceKindServer, api.PathKindDirect, "203.0.113.40:41641", 30)
	nas := dev("nas", api.DeviceKindServer, api.PathKindDirect, "192.168.1.20:41641", 1)
	desktop := dev("desktop", api.DeviceKindDesktop, api.PathKindDirect, "198.51.100.9:41641", 20)
	fast := dev("fast", api.DeviceKindServer, api.PathKindDirect, "203.0.113.41:41641", 10)
	phone := dev("phone", api.DeviceKindPhone, api.PathKindDirect, "203.0.113.50:41641", 40)
	phone.OS = "android"
	ipad := dev("ipad", api.DeviceKindOther, api.PathKindDirect, "203.0.113.51:41641", 40)
	ipad.OS = "iOS"
	relayed := dev("relayed", api.DeviceKindServer, api.PathKindPeerRelay, "", 15)
	offline := dev("offline", api.DeviceKindServer, api.PathKindDirect, "203.0.113.60:41641", 5)
	offline.Online = false
	// The status says direct, but no ping came back to say so.
	unpinged := dev("unpinged", api.DeviceKindServer, api.PathKindDirect, "203.0.113.61:41641", 0)
	unpinged.Path.RTTMs = nil
	cgnat := dev("cgnat", api.DeviceKindServer, api.PathKindDirect, "100.100.1.2:41641", 5)
	derpOnly := dev("derp-phone", api.DeviceKindPhone, api.PathKindDERP, "", 300)
	derpOnly.OS = "android"

	cand := func(id string, public bool) api.RelayCandidate {
		return api.RelayCandidate{DeviceID: id, PublicAddress: public}
	}
	for _, tc := range []struct {
		name       string
		devices    []api.Device
		controlURL string
		want       api.Relay
	}{
		{
			name:    "public servers first, then by round trip",
			devices: []api.Device{self, stuck, nas, desktop, vps, fast},
			want: api.Relay{
				Supported: true, DERP: []string{"stuck"},
				Candidates: []api.RelayCandidate{cand("fast", true), cand("vps", true), cand("desktop", true)},
			},
		},
		{
			name:    "a server that just restarted comes after a steady one",
			devices: []api.Device{upFor(fast, 3600), upFor(vps, 40*86400)},
			want:    api.Relay{Supported: true, DERP: []string{}, Candidates: []api.RelayCandidate{cand("vps", true), cand("fast", true)}},
		},
		{
			name:    "a private address comes after a public one, whatever the round trip",
			devices: []api.Device{nas, cgnat, vps},
			want: api.Relay{
				Supported: true, DERP: []string{},
				Candidates: []api.RelayCandidate{cand("vps", true), cand("nas", false), cand("cgnat", false)},
			},
		},
		{
			name: "phones, tablets, Bield, relayed, offline and unpinged devices can't be one",
			devices: []api.Device{
				self, phone, ipad, relayed, offline, unpinged, derpOnly,
			},
			want: api.Relay{Supported: true, DERP: []string{"derp-phone"}, Candidates: []api.RelayCandidate{}},
		},
		{
			name:       "not on another control server",
			devices:    []api.Device{stuck, vps},
			controlURL: "https://headscale.example.com",
			want:       api.Relay{DERP: []string{}, Candidates: []api.RelayCandidate{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := api.NewRelay(tc.devices, tc.controlURL); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NewRelay = %+v, want %+v", got, tc.want)
			}
		})
	}
}
