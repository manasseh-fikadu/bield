package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func deviceCommands() []*Command {
	rangeFlag := Flag{Name: "range", Arg: "range", Default: "24h", Usage: "how far back: 1h, 6h, 24h or 7d"}
	return []*Command{
		{
			Name: "devices", Max: 0, Run: runDevices,
			Summary: "list the devices on the tailnet and how they are doing",
			Flags: []Flag{
				{Name: "status", Arg: "status", Usage: "only devices that are online, attention, offline or unknown"},
				{Name: "kind", Arg: "kind", Usage: "only servers, laptops, desktops, tablets, phones or others"},
				{Name: "tag", Arg: "tag", Usage: "only devices with this Tailscale tag, such as server or tag:server"},
				{Name: "path", Arg: "direct|derp|peer-relay", Usage: "only devices Bield reaches this way: directly, through a DERP relay or through a peer relay"},
			},
		},
		{Name: "devices show", Args: "<device>", Min: 1, Max: 1, Run: runDeviceShow, Summary: "show everything Bield knows about a device"},
		{
			Name: "devices history", Args: "<device>", Min: 1, Max: 1, Run: runDeviceHistory, Flags: []Flag{rangeFlag},
			Summary: "show a device's CPU, memory, network and latency over time",
		},
		{
			Name: "devices usage", Args: "<device>", Min: 1, Max: 1, Run: runDeviceUsage,
			Summary: "show what uses a device's CPU and memory now",
			What:    "Needs the device's agent. Viewers see no command lines.",
		},
		{
			Name: "devices set", Args: "<device>", Min: 1, Max: 1, Admin: true, Run: runDeviceSet,
			Summary: "change a device's name, kind, notes, offline alert or thresholds",
			What: "Only the flags you give change. A threshold takes a number in percent (minutes for offline-minutes), " +
				"or \"default\" to go back to the setting for every device.",
			Flags: []Flag{
				{Name: "name", Arg: "name", Usage: "the name Bield shows"},
				{Name: "kind", Arg: "kind", Usage: "server, laptop, desktop, tablet, phone or other"},
				{Name: "notes", Arg: "text", Usage: "notes about the device"},
				{Name: "alert-offline", Arg: "true|false", Usage: "raise an alert when it goes offline"},
				{Name: "disk-warn", Arg: "percent", Usage: "disk warning threshold"},
				{Name: "disk-critical", Arg: "percent", Usage: "disk critical threshold"},
				{Name: "memory-warn", Arg: "percent", Usage: "memory warning threshold"},
				{Name: "cpu-warn", Arg: "percent", Usage: "CPU warning threshold"},
				{Name: "offline-minutes", Arg: "minutes", Usage: "how long it can be offline before an alert"},
			},
		},
	}
}

func statusPaint(v *View, s api.Status) string {
	switch s {
	case api.StatusOnline:
		return v.Paint(good, string(s))
	case api.StatusAttention:
		return v.Paint(warn, string(s))
	case api.StatusOffline:
		return v.Paint(bad, string(s))
	default:
		return v.Paint(dim, string(s))
	}
}

// tagLabel lists a device's Tailscale tags, all of them, or "-" for none.
func tagLabel(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ",")
}

// hasTag reports whether tags has want, which is a tag with or without its
// "tag:" start, in any case.
func hasTag(tags []string, want string) bool {
	want = strings.TrimPrefix(strings.ToLower(want), "tag:")
	return slices.ContainsFunc(tags, func(t string) bool {
		return strings.TrimPrefix(strings.ToLower(t), "tag:") == want
	})
}

// pathLabel says how traffic reaches a device: "direct 12ms", "relay fra".
func pathLabel(p api.Path) string {
	rtt := ""
	if p.RTTMs != nil {
		rtt = fmt.Sprintf(" %.0fms", *p.RTTMs)
	}
	switch p.Kind {
	case api.PathKindDirect:
		return "direct" + rtt
	case api.PathKindDERP:
		return "relay " + p.DERP + rtt
	case api.PathKindPeerRelay:
		return "peer relay" + rtt
	case api.PathKindSelf:
		return "this hub"
	default:
		return "-"
	}
}

// pathKinds are the paths that bield devices --path picks between.
var pathKinds = []api.PathKind{api.PathKindDirect, api.PathKindDERP, api.PathKindPeerRelay}

func runDevices(c *Call) error {
	path := api.PathKind(c.Str("path"))
	if path != "" && !slices.Contains(pathKinds, path) {
		return usagef(c.cmd, "--path is direct, derp or peer-relay")
	}
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	var list []api.Device
	for _, d := range snap.Devices {
		if s := c.Str("status"); s != "" && string(d.Status) != s {
			continue
		}
		if k := c.Str("kind"); k != "" && string(d.Kind) != k {
			continue
		}
		if t := c.Str("tag"); t != "" && !hasTag(d.Tags, t) {
			continue
		}
		if path != "" && d.Path.Kind != path {
			continue
		}
		list = append(list, d)
	}
	if list == nil {
		list = []api.Device{}
	}
	return c.Print(list, func(v *View) {
		t := v.Table("NAME", "STATUS", "KIND", "OS", "CPU", "MEM", "DISK", "PATH", "TAGS", "NOTE")
		for _, d := range list {
			cpu, mem, disk := "-", "-", "-"
			if m := d.Metrics; m != nil {
				cpu = percent(m.CPU)
				if m.Memory != nil {
					mem = share(m.Memory.Used, m.Memory.Total)
				}
				if m.Disk != nil {
					disk = share(m.Disk.Used, m.Disk.Total)
				}
			}
			name := d.Name
			if d.Self {
				name += " (hub)"
			}
			note := d.StatusReason
			if d.Status == api.StatusOffline && d.LastSeen != nil {
				note = "seen " + v.since(*d.LastSeen)
			}
			t.Row(name, statusPaint(v, d.Status), string(d.Kind), d.OS, cpu, mem, disk, pathLabel(d.Path), tagLabel(d.Tags), oneLine(note, 50))
		}
		t.Write()
		if t.Len() == 0 {
			v.Line("No devices match.")
		}
	})
}

func runDeviceShow(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	return c.Print(d, func(v *View) {
		v.Printf("%s\n", v.Paint(bold, d.Name))
		v.Field("ID", d.ID)
		v.Field("Status", statusPaint(v, d.Status)+reasonSuffix(d.StatusReason))
		v.Field("Kind", string(d.Kind))
		v.Field("DNS name", d.DNSName)
		v.Field("Addresses", strings.Join(d.IPs, ", "))
		v.Field("Tags", strings.Join(d.Tags, ", "))
		if d.Info != nil {
			os := d.Info.OSVersion
			if os == "" {
				os = d.OS
			}
			v.Field("System", strings.TrimSpace(os+" "+d.Info.Arch))
			v.Field("Model", d.Info.Model)
			if d.Info.Cores > 0 {
				v.Field("Cores", strconv.Itoa(d.Info.Cores))
			}
		} else {
			v.Field("System", d.OS)
		}
		if !d.Online && d.LastSeen != nil {
			v.Field("Last seen", clock(*d.LastSeen)+" ("+v.since(*d.LastSeen)+")")
		}
		v.Field("Path", pathLabel(d.Path))
		if d.KeyExpiry != nil {
			v.Field("Key expires", clock(*d.KeyExpiry)+" (in "+v.until(*d.KeyExpiry)+")")
		}
		if m := d.Metrics; m != nil {
			v.Field("CPU", percent(m.CPU))
			if len(m.Load) == 3 {
				v.Field("Load", fmt.Sprintf("%.2f %.2f %.2f", m.Load[0], m.Load[1], m.Load[2]))
			}
			if m.Memory != nil {
				v.Field("Memory", fmt.Sprintf("%s of %s (%s)", bytesLabel(m.Memory.Used), bytesLabel(m.Memory.Total), share(m.Memory.Used, m.Memory.Total)))
			}
			if m.Disk != nil {
				v.Field("Disk", fmt.Sprintf("%s of %s (%s)", bytesLabel(m.Disk.Used), bytesLabel(m.Disk.Total), share(m.Disk.Used, m.Disk.Total)))
			}
			if m.Uptime > 0 {
				v.Field("Uptime", age(time.Duration(m.Uptime)*time.Second))
			}
			if m.Battery != nil {
				v.Field("Battery", fmt.Sprintf("%.0f%%", m.Battery.Percent))
			}
		}
		switch d.MeasuredBy {
		case api.MeasuredByHub:
			v.Field("Measured by", "Bield itself")
		case api.MeasuredByAgent:
			agent := "the agent"
			if d.Agent != nil {
				agent += " " + d.Agent.Version
				if d.Agent.Stale {
					agent += " (stopped reporting)"
				} else if d.Agent.Outdated {
					agent += " (outdated)"
				}
			}
			v.Field("Measured by", agent)
		}
		if d.Docker {
			v.Field("Docker", "yes")
		}
		v.Field("Notes", d.Settings.Notes)
	})
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

// rangeOK checks a range the history routes take.
func rangeOK(c *Call, rng string) error {
	if !slices.Contains([]string{"1h", "6h", "24h", "7d"}, rng) {
		return usagef(c.cmd, "--range is 1h, 6h, 24h or 7d, not %q", rng)
	}
	return nil
}

// seriesSummary sums up the points of a series.
type seriesSummary struct {
	Name   string   `json:"name"`
	Unit   string   `json:"unit"`
	Points int      `json:"points"`
	Latest *float64 `json:"latest"`
	Min    *float64 `json:"min"`
	Avg    *float64 `json:"avg"`
	Max    *float64 `json:"max"`
}

func summarize(name, unit string, points []api.Point) seriesSummary {
	s := seriesSummary{Name: name, Unit: unit}
	var sum float64
	for _, p := range points {
		if p.V == nil {
			continue
		}
		s.Points++
		v := *p.V
		sum += v
		s.Latest = &v
		if s.Min == nil || v < *s.Min {
			s.Min = &v
		}
		if s.Max == nil || v > *s.Max {
			s.Max = &v
		}
	}
	if s.Points > 0 {
		avg := sum / float64(s.Points)
		s.Avg = &avg
	}
	return s
}

func num(f *float64, unit string) string {
	if f == nil {
		return "-"
	}
	switch unit {
	case "B/s":
		return bytesLabel(uint64(max(*f, 0))) + "/s"
	case "%":
		return fmt.Sprintf("%.0f%%", *f)
	}
	return fmt.Sprintf("%.1f %s", *f, unit)
}

func runDeviceHistory(c *Call) error {
	rng := c.Str("range")
	if err := rangeOK(c, rng); err != nil {
		return err
	}
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	h, err := cl.DeviceHistory(c.Ctx, d.ID, rng)
	if err != nil {
		return err
	}
	return c.Print(h, func(v *View) {
		v.Printf("%s, %s to %s\n\n", v.Paint(bold, d.Name), clock(h.From), clock(h.To))
		t := v.Table("METRIC", "LATEST", "MIN", "AVG", "MAX")
		for _, s := range []seriesSummary{
			summarize("CPU", "%", h.CPU), summarize("Memory", "%", h.Memory),
			summarize("Received", "B/s", h.NetRx), summarize("Sent", "B/s", h.NetTx), summarize("Latency", "ms", h.RTT),
		} {
			if s.Points == 0 {
				t.Row(s.Name, "-", "-", "-", "-")
				continue
			}
			t.Row(s.Name, num(s.Latest, s.Unit), num(s.Min, s.Unit), num(s.Avg, s.Unit), num(s.Max, s.Unit))
		}
		t.Write()
	})
}

func runDeviceUsage(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	u, err := cl.DeviceUsage(c.Ctx, d.ID)
	if err != nil {
		return err
	}
	return c.Print(u, func(v *View) {
		if u.Sample == nil {
			v.Printf("Bield has no sample of what uses %s; it needs the device's agent.\n", d.Name)
			return
		}
		v.Printf("%s, sampled %s\n\n", v.Paint(bold, d.Name), v.since(u.Sample.At))
		t := v.Table("GROUP", "KIND", "CPU", "MEM", "PROCS", "BUSIEST")
		for _, g := range u.Sample.Groups {
			t.Row(g.Name, string(g.Kind), fmt.Sprintf("%.1f%%", g.CPU), bytesLabel(g.Mem), strconv.Itoa(g.Procs), oneLine(strings.Join(g.Top, ", "), 40))
		}
		t.Write()
	})
}

func runDeviceSet(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	in := api.SetDeviceSettings{
		DisplayName: d.Settings.DisplayName, Kind: d.Settings.Kind, Notes: d.Settings.Notes,
		AlertOffline: &d.Settings.AlertOffline, Thresholds: d.Settings.Thresholds,
	}
	changed := 0
	if c.Given("name") {
		in.DisplayName, changed = c.Str("name"), changed+1
	}
	if c.Given("kind") {
		in.Kind, changed = api.DeviceKind(c.Str("kind")), changed+1
	}
	if c.Given("notes") {
		in.Notes, changed = c.Str("notes"), changed+1
	}
	if c.Given("alert-offline") {
		on := c.Str("alert-offline") == "true"
		if s := c.Str("alert-offline"); s != "true" && s != "false" {
			return usagef(c.cmd, "--alert-offline is true or false, not %q", s)
		}
		in.AlertOffline, changed = &on, changed+1
	}
	for name, dst := range map[string]**float64{
		"disk-warn": &in.Thresholds.DiskWarn, "disk-critical": &in.Thresholds.DiskCritical,
		"memory-warn": &in.Thresholds.MemoryWarn, "cpu-warn": &in.Thresholds.CPUWarn,
	} {
		if !c.Given(name) {
			continue
		}
		f, err := thresholdValue(c, name)
		if err != nil {
			return err
		}
		*dst, changed = f, changed+1
	}
	if c.Given("offline-minutes") {
		f, err := thresholdValue(c, "offline-minutes")
		if err != nil {
			return err
		}
		in.Thresholds.OfflineMinutes = nil
		if f != nil {
			n := int(*f)
			in.Thresholds.OfflineMinutes = &n
		}
		changed++
	}
	if changed == 0 {
		return usagef(c.cmd, "nothing to change; give at least one flag")
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	got, err := cl.SetDeviceSettings(c.Ctx, d.ID, in)
	if err != nil {
		return err
	}
	return c.Print(got, func(v *View) { v.Printf("Changed the settings of %s\n", d.Name) })
}

// thresholdValue reads a threshold flag: a number, or "default" for none.
func thresholdValue(c *Call, name string) (*float64, error) {
	s := c.Str(name)
	if s == "default" {
		return nil, nil
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil || f < 0 {
		return nil, usagef(c.cmd, "--%s is a number or \"default\", not %q", name, s)
	}
	return &f, nil
}
