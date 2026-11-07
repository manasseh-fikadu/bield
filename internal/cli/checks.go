package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func checkCommands() []*Command {
	rangeFlag := Flag{Name: "range", Arg: "range", Default: "24h", Usage: "how far back: 1h, 6h, 24h or 7d"}
	return []*Command{
		{
			Name: "checks", Run: runChecks,
			Summary: "list the checks and whether they pass",
			What:    "Bield also pings every device on the tailnet, which are listed with --all. Viewers see a check's address without the parts that may hold a secret.",
			Flags: []Flag{
				{Name: "failing", Kind: Bool, Usage: "only checks that are failing"},
				{Name: "group", Arg: "group", Usage: "only checks in one group"},
				{Name: "all", Kind: Bool, Usage: "include the ping checks of the devices"},
			},
		},
		{Name: "checks show", Args: "<check>", Min: 1, Max: 1, Run: runCheckShow, Summary: "show a check and its latest result"},
		{
			Name: "checks history", Args: "<check>", Min: 1, Max: 1, Run: runCheckHistory, Flags: []Flag{rangeFlag},
			Summary: "show a check's uptime and latency over time",
		},
		{Name: "checks run", Args: "<check>", Min: 1, Max: 1, Admin: true, Run: runCheckRun, Summary: "run a check now"},
		{
			Name: "checks add", Max: 0, Admin: true, Run: runCheckAdd,
			Summary: "add a check on a URL, a TCP port or a host name",
			What:    "For an http check the target is a full URL, for tcp it is host:port, and for dns it is a host name.",
			Flags: []Flag{
				{Name: "name", Arg: "name", Usage: "what to call it (required)"},
				{Name: "kind", Arg: "kind", Usage: "http, tcp or dns (required)"},
				{Name: "target", Arg: "target", Usage: "what to probe (required)"},
				{Name: "group", Arg: "group", Usage: "the group to list it in (default Custom)"},
				{Name: "interval", Arg: "time", Kind: Duration, Usage: "how often it runs, from 10s to 1d (default 1m)"},
				{Name: "probe-timeout", Arg: "time", Kind: Duration, Usage: "how long the target has to answer (default 5s)"},
				{Name: "expect-status", Arg: "code", Kind: Int, Usage: "the HTTP status that passes (default: any below 400)"},
				{Name: "insecure", Kind: Bool, Usage: "don't check the server's TLS certificate"},
			},
		},
		{Name: "checks pause", Args: "<check>", Min: 1, Max: 1, Admin: true, Run: checkPause(true), Summary: "stop a check from running until you resume it"},
		{Name: "checks resume", Args: "<check>", Min: 1, Max: 1, Admin: true, Run: checkPause(false), Summary: "run a paused check again"},
		{Name: "checks delete", Args: "<check>", Min: 1, Max: 1, Admin: true, Destructive: true, Run: runCheckDelete, Summary: "delete a check and its history"},
	}
}

func statePaint(v *View, s api.CheckState) string {
	switch s {
	case api.CheckStatePassing:
		return v.Paint(good, string(s))
	case api.CheckStateFailing:
		return v.Paint(bad, string(s))
	default:
		return v.Paint(dim, string(s))
	}
}

func uptimeLabel(u *float64) string {
	if u == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *u)
}

func runChecks(c *Call) error {
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	list := []api.Check{}
	for _, ch := range snap.Checks {
		switch {
		case ch.Kind == api.CheckKindPing && !c.Bool("all"):
			continue
		case c.Bool("failing") && ch.State != api.CheckStateFailing:
			continue
		case c.Str("group") != "" && !strings.EqualFold(ch.Group, c.Str("group")):
			continue
		}
		list = append(list, ch)
	}
	return c.Print(list, func(v *View) {
		if len(list) == 0 {
			v.Line("No checks match.")
			return
		}
		t := v.Table("NAME", "GROUP", "KIND", "STATE", "UPTIME", "LAST", "ENDPOINT")
		for _, ch := range list {
			last := "-"
			if ch.Last != nil {
				last = v.since(ch.Last.At)
			}
			t.Row(ch.Name, ch.Group, string(ch.Kind), statePaint(v, ch.State), uptimeLabel(ch.Uptime), last, oneLine(ch.Endpoint, 50))
		}
		t.Write()
	})
}

func runCheckShow(c *Call) error {
	ch, err := c.check(c.Args[0])
	if err != nil {
		return err
	}
	return c.Print(ch, func(v *View) {
		v.Printf("%s\n", v.Paint(bold, ch.Name))
		v.Field("ID", ch.ID)
		v.Field("State", statePaint(v, ch.State)+reasonSuffix(ch.StateReason))
		v.Field("Group", ch.Group)
		v.Field("Kind", string(ch.Kind))
		target := ch.Target
		if target == "" {
			target = ch.Endpoint
		}
		v.Field("Target", target)
		v.Field("Every", (time.Duration(ch.IntervalSec) * time.Second).String())
		v.Field("Timeout", (time.Duration(ch.TimeoutMs) * time.Millisecond).String())
		if ch.ExpectStatus != 0 {
			v.Field("Expects", fmt.Sprintf("HTTP %d", ch.ExpectStatus))
		}
		v.Field("Uptime (24h)", uptimeLabel(ch.Uptime))
		if ch.Failures > 0 {
			v.Field("Failed in a row", strconv.Itoa(ch.Failures))
		}
		if ch.Last != nil {
			v.Field("Last run", fmt.Sprintf("%s: %s", v.since(ch.Last.At), ch.Last.Detail))
		}
		if ch.Paused {
			v.Field("Paused", "yes")
		}
	})
}

func runCheckHistory(c *Call) error {
	rng := c.Str("range")
	if err := rangeOK(c, rng); err != nil {
		return err
	}
	ch, err := c.check(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	h, err := cl.CheckHistory(c.Ctx, ch.ID, rng)
	if err != nil {
		return err
	}
	return c.Print(h, func(v *View) {
		v.Printf("%s, %s to %s\n\n", v.Paint(bold, ch.Name), clock(h.From), clock(h.To))
		t := v.Table("METRIC", "LATEST", "MIN", "AVG", "MAX")
		for _, s := range []seriesSummary{summarize("Uptime", "%", h.Uptime), summarize("Latency", "ms", h.Latency)} {
			if s.Points == 0 {
				t.Row(s.Name, "-", "-", "-", "-")
				continue
			}
			t.Row(s.Name, num(s.Latest, s.Unit), num(s.Min, s.Unit), num(s.Avg, s.Unit), num(s.Max, s.Unit))
		}
		t.Write()
	})
}

func runCheckRun(c *Call) error {
	ch, err := c.check(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	res, err := cl.RunCheck(c.Ctx, ch.ID)
	if err != nil {
		return err
	}
	return c.Print(res, func(v *View) {
		word := v.Paint(good, "passed")
		if !res.OK {
			word = v.Paint(bad, "failed")
		}
		latency := ""
		if res.LatencyMs != nil {
			latency = fmt.Sprintf(" in %.0f ms", *res.LatencyMs)
		}
		v.Printf("%s %s%s: %s\n", ch.Name, word, latency, res.Detail)
	})
}

func runCheckAdd(c *Call) error {
	for _, name := range []string{"name", "kind", "target"} {
		if c.Str(name) == "" {
			return usagef(c.cmd, "--%s is required", name)
		}
	}
	kind := api.CheckKind(c.Str("kind"))
	if !slices.Contains([]api.CheckKind{api.CheckKindHTTP, api.CheckKindTCP, api.CheckKindDNS}, kind) {
		return usagef(c.cmd, "--kind is http, tcp or dns, not %q", kind)
	}
	in := api.CheckInput{
		Name: c.Str("name"), Kind: kind, Target: c.Str("target"), Group: c.Str("group"),
		ExpectStatus: c.Int("expect-status"), Insecure: c.Bool("insecure"),
	}
	if c.Given("interval") {
		in.IntervalSec = int(c.Duration("interval") / time.Second)
	}
	if c.Given("probe-timeout") {
		in.TimeoutMs = int(c.Duration("probe-timeout") / time.Millisecond)
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	ch, err := cl.AddCheck(c.Ctx, in)
	if err != nil {
		return err
	}
	return c.Print(ch, func(v *View) { v.Printf("Added the check %s (%s)\n", ch.Name, ch.ID) })
}

func checkPause(pause bool) func(*Call) error {
	return func(c *Call) error {
		ch, err := c.check(c.Args[0])
		if err != nil {
			return err
		}
		cl, err := c.Client()
		if err != nil {
			return err
		}
		in := api.CheckInput{
			Name: ch.Name, Kind: ch.Kind, Target: ch.Target, Group: ch.Group, IntervalSec: ch.IntervalSec,
			TimeoutMs: ch.TimeoutMs, ExpectStatus: ch.ExpectStatus, Insecure: ch.Insecure, Paused: pause,
		}
		got, err := cl.ChangeCheck(c.Ctx, ch.ID, in)
		if err != nil {
			return err
		}
		verb := "Resumed"
		if pause {
			verb = "Paused"
		}
		return c.Print(got, func(v *View) { v.Printf("%s the check %s\n", verb, got.Name) })
	}
}

func runCheckDelete(c *Call) error {
	ch, err := c.check(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if err := cl.DeleteCheck(c.Ctx, ch.ID); err != nil {
		return err
	}
	return c.Print(map[string]any{"deleted": ch.ID, "name": ch.Name}, func(v *View) { v.Printf("Deleted the check %s\n", ch.Name) })
}
