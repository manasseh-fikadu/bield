package api

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"
	"time"
)

const (
	// relayCandidates is how many devices a Relay suggests at most.
	relayCandidates = 3
	// steadyUptime is how long a candidate must have been up, if Bield knows,
	// to count as steady rather than just restarted.
	steadyUptime = 24 * time.Hour
)

// cgnat is the range Tailscale gives its nodes their IPv4 addresses from,
// which isn't reachable from the internet.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// newRelay suggests a peer relay for the devices Bield reaches through DERP,
// from the snapshot's devices and the control server the node uses, "" for
// Tailscale's.
func newRelay(devices []Device, controlURL string) Relay {
	out := Relay{DERP: []string{}, Candidates: []RelayCandidate{}}
	if controlURL != "" {
		return out
	}
	out.Supported = true
	type candidate struct {
		RelayCandidate
		server, restarted bool
		rtt               float64
	}
	var cands []candidate
	for _, d := range devices {
		if d.Self || !replied(d) {
			continue
		}
		switch {
		case d.Path.Kind == PathKindDERP:
			out.DERP = append(out.DERP, d.ID)
		case d.Path.Kind == PathKindDirect && d.CanRelay():
			c := candidate{
				DeviceID: d.ID, PublicAddress: publicEndpoint(d.Path.Endpoint),
				server: d.Kind == DeviceKindServer,
				rtt:    *d.Path.RTTMs,
			}
			if m := d.Metrics; m != nil && m.Uptime > 0 {
				c.restarted = time.Duration(m.Uptime)*time.Second < steadyUptime
			}
			cands = append(cands, c)
		default:
		}
	}
	slices.SortStableFunc(cands, func(a, b candidate) int {
		return cmp.Or(
			compareTrueFirst(a.PublicAddress, b.PublicAddress),
			compareTrueFirst(a.server, b.server),
			compareTrueFirst(!a.restarted, !b.restarted),
			cmp.Compare(a.rtt, b.rtt),
		)
	})
	for _, c := range cands[:min(len(cands), relayCandidates)] {
		out.Candidates = append(out.Candidates, c.RelayCandidate)
	}
	return out
}

// CanRelay reports whether a device can be a peer relay: a computer, since
// Tailscale's apps for phones, tablets and TVs can use a relay but can't be
// one.
func (d Device) CanRelay() bool {
	switch strings.ToLower(d.OS) {
	case "ios", "ipados", "tvos", "android":
		return false
	}
	return d.Kind != DeviceKindPhone && d.Kind != DeviceKindTablet
}

// publicEndpoint reports whether an ip:port endpoint is at a public address.
func publicEndpoint(endpoint string) bool {
	ap, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return false
	}
	ip := ap.Addr().Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}

// compareTrueFirst orders true before false.
func compareTrueFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}
