package cli

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// defaultRelayPort is the UDP port bield relay suggests, as Tailscale's
// documentation does.
const defaultRelayPort = 40000

func relayCommands() []*Command {
	return []*Command{{
		Name: "relay", Run: runRelay,
		Summary: "suggest a peer relay for the devices Bield reaches through DERP, and how to set it up",
		Flags: []Flag{
			{Name: "port", Kind: Int, Arg: "port", Default: strconv.Itoa(defaultRelayPort), Usage: "the UDP port the relay listens on"},
			{Name: "via", Arg: "device", Usage: "make this device the relay instead of the one Bield suggests"},
		},
	}}
}

// RelayAdvice is what bield relay says, and prints with --json.
type RelayAdvice struct {
	// Supported is whether the tailnet can have peer relays: not on
	// Headscale.
	Supported bool `json:"supported"`
	// DERP are the devices Bield reaches through DERP now.
	DERP []RelayDevice `json:"derp"`
	// Candidates are the devices that could be the relay, best first.
	Candidates []RelayDevice `json:"candidates"`
	// Relay is the device to make the relay: --via's, or the best candidate.
	Relay *RelayDevice `json:"relay,omitempty"`
	Port  int          `json:"port"`
	// Command is what to run on the relay, and Grant what to add to the
	// tailnet policy file's grants, while devices are on DERP.
	Command string `json:"command,omitempty"`
	Grant   string `json:"grant,omitempty"`
}

// RelayDevice is a device in a RelayAdvice.
type RelayDevice struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind api.DeviceKind `json:"kind"`
	OS   string         `json:"os"`
	// IP is its tailnet IPv4 address, or its first address if it has none.
	IP   string   `json:"ip"`
	Path api.Path `json:"path"`
	// PublicAddress is whether Bield reaches it at a public IP address.
	PublicAddress bool `json:"publicAddress,omitempty"`
	// Uptime is how long it has been up, in seconds, if its agent says.
	Uptime int64 `json:"uptime,omitempty"`
}

func runRelay(c *Call) error {
	port := c.Int("port")
	if port < 1 || port > 65535 {
		return usagef(c.cmd, "--port is a UDP port from 1 to 65535")
	}
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	byID := map[string]api.Device{}
	for _, d := range snap.Devices {
		byID[d.ID] = d
	}
	r := snap.Relay
	advice := RelayAdvice{Supported: r.Supported, DERP: []RelayDevice{}, Candidates: []RelayDevice{}, Port: port}
	for _, id := range r.DERP {
		if d, ok := byID[id]; ok {
			advice.DERP = append(advice.DERP, relayDevice(d, false))
		}
	}
	for _, rc := range r.Candidates {
		if d, ok := byID[rc.DeviceID]; ok {
			advice.Candidates = append(advice.Candidates, relayDevice(d, rc.PublicAddress))
		}
	}
	if via := c.Str("via"); via != "" {
		d, err := findDevice(snap.Devices, via)
		if err != nil {
			return err
		}
		if d.Self {
			return usagef(c.cmd, "%s is Bield's own node, which can't relay its own connections", d.Name)
		}
		if !d.CanRelay() {
			return usagef(c.cmd, "%s can't be a peer relay: phones, tablets and TVs can use one but can't be one", d.Name)
		}
		rd := relayDevice(d, false)
		for _, cand := range advice.Candidates {
			if cand.ID == d.ID {
				rd = cand
			}
		}
		advice.Relay = &rd
	} else if len(advice.Candidates) > 0 {
		advice.Relay = &advice.Candidates[0]
	}
	var src []string
	for _, d := range advice.DERP {
		if advice.Relay == nil || d.ID != advice.Relay.ID {
			src = append(src, d.IP)
		}
	}
	if advice.Supported && advice.Relay != nil && len(src) > 0 {
		advice.Command = relayCommand(advice.Relay.OS, port)
		advice.Grant = relayGrant(src, advice.Relay.IP)
	}
	return c.Print(advice, func(v *View) { printRelay(v, advice) })
}

func printRelay(v *View, a RelayAdvice) {
	if !a.Supported {
		v.Line("Bield doesn't suggest a peer relay on this tailnet: peer relays need Tailscale's")
		v.Line("coordination server, and this one uses another, such as Headscale.")
		return
	}
	if len(a.DERP) == 0 {
		v.Line("Bield reaches every online device directly or through a peer relay, so none needs one now.")
		if a.Relay != nil {
			v.Printf("If one falls back to DERP, the best machine to relay for it is %s.\n", a.Relay.Name)
		}
		return
	}
	v.Printf("Bield reaches %s through DERP, one of Tailscale's shared relays:\n\n", plural(len(a.DERP), "device", "devices"))
	t := v.Table("NAME", "PATH")
	for _, d := range a.DERP {
		t.Row(d.Name, pathLabel(d.Path))
	}
	t.Write()
	v.Line("")
	v.Line("A peer relay is a machine of yours that relays for them instead, which is often faster.")
	if a.Relay == nil {
		v.Line("No device fits: it takes an online computer that Bield reaches directly, best a server")
		v.Line("at a public address. Pick one with --via.")
		return
	}
	v.Printf("%s %s (%s)\n", v.Paint(bold, "Suggested:"), a.Relay.Name, relayReasons(*a.Relay))
	var others []string
	for _, cand := range a.Candidates {
		if cand.ID != a.Relay.ID {
			others = append(others, cand.Name)
		}
	}
	if len(others) > 0 {
		v.Printf("Also possible: %s\n", strings.Join(others, ", "))
	}
	v.Line("If it's an app's built-in node, such as a Bield's, it has no tailscale command: pick another")
	v.Line("with --via.")
	if a.Command == "" {
		v.Line("")
		v.Printf("%s is the only device on DERP, so it has no one to relay for.\n", a.Relay.Name)
		return
	}
	v.Line("")
	v.Printf("1. On %s, open UDP port %d to the internet in its firewall and in any cloud\n", a.Relay.Name, a.Port)
	v.Line("   firewall in front of it, then run:")
	v.Printf("     %s\n", a.Command)
	v.Line("2. In the Tailscale admin console, open Access controls and add this to \"grants\":")
	for line := range strings.SplitSeq(a.Grant, "\n") {
		v.Printf("     %s\n", line)
	}
	v.Line("3. Within a minute or two, bield devices --path peer-relay should list them.")
	v.Line("")
	v.Line("Bield only sees its own connections. It knows how it reaches each device, not how they")
	v.Line("reach each other, so it can't tell whether the relay will help them. Both ends need")
	v.Line("Tailscale 1.86 or newer.")
}

// relayDevice describes d for a RelayAdvice.
func relayDevice(d api.Device, public bool) RelayDevice {
	rd := RelayDevice{ID: d.ID, Name: d.Name, Kind: d.Kind, OS: d.OS, IP: tailnetIPv4(d.IPs), Path: d.Path, PublicAddress: public}
	if d.Metrics != nil {
		rd.Uptime = d.Metrics.Uptime
	}
	return rd
}

// relayReasons says why a device makes a good relay, such as "server,
// public address, direct 12ms, up 41d".
func relayReasons(d RelayDevice) string {
	var out []string
	if d.Kind != "" && d.Kind != api.DeviceKindOther {
		out = append(out, string(d.Kind))
	}
	if d.PublicAddress {
		out = append(out, "public address")
	}
	out = append(out, pathLabel(d.Path))
	if d.Uptime > 0 {
		out = append(out, "up "+age(time.Duration(d.Uptime)*time.Second))
	}
	return strings.Join(out, ", ")
}

// tailnetIPv4 picks a device's IPv4 tailnet address, or its first address.
func tailnetIPv4(ips []string) string {
	for _, s := range ips {
		if ip, err := netip.ParseAddr(s); err == nil && ip.Is4() {
			return s
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// relayCommand is what makes a machine running os a peer relay on port.
// Tailscale's command needs root on Linux and the BSDs.
func relayCommand(os string, port int) string {
	cmd := fmt.Sprintf("tailscale set --relay-server-port=%d", port)
	switch strings.ToLower(os) {
	case "macos", "windows":
		return cmd
	}
	return "sudo " + cmd
}

// relayGrant is the grant that lets the devices at src use the relay at dst.
func relayGrant(src []string, dst string) string {
	return fmt.Sprintf("{\n  \"src\": [%s],\n  \"dst\": [%q],\n  \"app\": { \"tailscale.com/cap/relay\": [] }\n}",
		quoteList(src), dst)
}

// quoteList quotes each string and joins them with commas.
func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
