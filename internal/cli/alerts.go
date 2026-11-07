package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func alertCommands() []*Command {
	return []*Command{
		{
			Name: "alerts", Run: runAlerts, Stream: true,
			Summary: "list the alerts that are open, or follow them as they change",
			What:    "Open alerts include the ones acknowledged or snoozed. --resolved lists the ones that cleared in the last 30 days.",
			Flags: []Flag{
				{Name: "follow", Short: 'f', Kind: Bool, Usage: "keep running, and print each alert as it opens, changes or resolves"},
				{Name: "resolved", Kind: Bool, Usage: "list resolved alerts instead, the latest first"},
				{Name: "limit", Arg: "n", Kind: Int, Default: "30", Usage: "how many resolved alerts to list"},
				{Name: "before", Arg: "id", Kind: Int, Usage: "list resolved alerts from before this ID, for the next page"},
			},
		},
		{Name: "alerts show", Args: "<id>", Min: 1, Max: 1, Run: runAlertShow, Summary: "show an alert, with the notes on it"},
		{Name: "alerts ack", Args: "<id>", Min: 1, Max: 1, Admin: true, Run: alertAction("ack"), Summary: "acknowledge an open alert: you are on it"},
		{Name: "alerts unack", Args: "<id>", Min: 1, Max: 1, Admin: true, Run: alertAction("unack"), Summary: "take back an acknowledgement"},
		{
			Name: "alerts snooze", Args: "<id>", Min: 1, Max: 1, Admin: true, Run: alertAction("snooze"),
			Summary: "hide an open alert for a while",
			Flags:   []Flag{{Name: "for", Arg: "time", Kind: Duration, Default: "1h", Usage: "how long, such as 30m, 4h or 2d (up to 30 days)"}},
		},
		{Name: "alerts wake", Args: "<id>", Min: 1, Max: 1, Admin: true, Run: alertAction("wake"), Summary: "end an alert's snooze"},
		{
			Name: "alerts note", Args: "<id> <text...>", Min: 2, Max: -1, Admin: true, Run: alertAction("note"),
			Summary: "write a note on an alert, open or resolved",
		},
	}
}

func severityPaint(v *View, s api.AlertSeverity) string {
	switch s {
	case api.AlertSeverityCritical:
		return v.Paint(bad, string(s))
	case api.AlertSeverityWarning:
		return v.Paint(warn, string(s))
	default:
		return v.Paint(dim, string(s))
	}
}

// deviceNames returns the names of the devices by ID.
func deviceNames(devices []api.Device) map[string]string {
	m := make(map[string]string, len(devices))
	for _, d := range devices {
		m[d.ID] = d.Name
	}
	return m
}

func alertID(c *Call, s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil || id < 1 {
		return 0, usagef(c.cmd, "%q isn't an alert ID; bield alerts lists them", s)
	}
	return id, nil
}

func alertRows(v *View, list []api.Alert, names map[string]string) *Table {
	t := v.Table("ID", "SEVERITY", "STATE", "AGE", "DEVICE", "TITLE")
	for _, a := range list {
		state := string(a.State)
		switch {
		case a.State == api.AlertStateSnoozed && a.SnoozedUntil != nil:
			state += " " + v.until(*a.SnoozedUntil)
		case a.State == api.AlertStateResolved && a.ResolvedAt != nil:
			state += " " + v.since(*a.ResolvedAt)
		}
		t.Row(strconv.FormatInt(a.ID, 10), severityPaint(v, a.Severity), state, v.ago(a.OpenedAt), names[a.DeviceID], oneLine(a.Title, 70))
	}
	return t
}

func runAlerts(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if c.Bool("follow") && !c.Bool("resolved") {
		return followAlerts(c)
	}
	if c.Bool("resolved") {
		l, err := cl.Alerts(c.Ctx, int64(c.Int("before")), c.Int("limit"))
		if err != nil {
			return err
		}
		snap, err := c.Snapshot()
		if err != nil {
			return err
		}
		return c.Print(l, func(v *View) {
			t := alertRows(v, l.Alerts, deviceNames(snap.Devices))
			t.Write()
			switch {
			case t.Len() == 0:
				v.Line("No resolved alerts.")
			case l.More:
				v.Printf("\nMore: bield alerts --resolved --before %d\n", l.Alerts[len(l.Alerts)-1].ID)
			}
		})
	}
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	list := snap.Alerts
	if list == nil {
		list = []api.Alert{}
	}
	return c.Print(list, func(v *View) {
		if len(list) == 0 {
			v.Line("No open alerts.")
			return
		}
		alertRows(v, list, deviceNames(snap.Devices)).Write()
	})
}

// alertChange is a line of bield alerts --follow.
type alertChange struct {
	Event string    `json:"event"`
	Alert api.Alert `json:"alert"`
}

func followAlerts(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var (
		seen  map[int64]api.Alert
		names map[string]string
	)
	emit := func(event string, a api.Alert) error {
		if c.JSON() {
			return writeJSONLine(c.Env.Stdout, alertChange{event, a})
		}
		v := c.view()
		v.Printf("%s  %-8s  %s  %s  %s\n", time.Now().Format("15:04:05"), strings.ToUpper(event), severityPaint(v, a.Severity),
			v.Paint(dim, fmt.Sprintf("#%d", a.ID)), oneLine(a.Title+deviceSuffix(names, a), 100))
		return v.err
	}
	err = cl.Watch(c.Ctx, func(s api.Snapshot) error {
		names = deviceNames(s.Devices)
		cur := make(map[int64]api.Alert, len(s.Alerts))
		for _, a := range s.Alerts {
			cur[a.ID] = a
			old, known := seen[a.ID]
			switch {
			case seen == nil:
				if err := emit("open", a); err != nil {
					return err
				}
			case !known:
				if err := emit("opened", a); err != nil {
					return err
				}
			case old.State != a.State:
				if err := emit(string(a.State), a); err != nil {
					return err
				}
			}
		}
		for id, old := range seen {
			if _, ok := cur[id]; !ok {
				now := time.Now()
				old.State, old.ResolvedAt = api.AlertStateResolved, &now
				if err := emit("resolved", old); err != nil {
					return err
				}
			}
		}
		seen = cur
		return nil
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func deviceSuffix(names map[string]string, a api.Alert) string {
	if n := names[a.DeviceID]; n != "" && !strings.Contains(a.Title, n) {
		return " (" + n + ")"
	}
	return ""
}

func runAlertShow(c *Call) error {
	id, err := alertID(c, c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	a, err := cl.Alert(c.Ctx, id)
	if err != nil {
		return err
	}
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	return c.Print(a, func(v *View) {
		v.Printf("%s\n", v.Paint(bold, a.Title))
		v.Field("ID", strconv.FormatInt(a.ID, 10))
		v.Field("Severity", severityPaint(v, a.Severity))
		v.Field("State", string(a.State))
		v.Field("Source", string(a.Source))
		v.Field("Device", deviceNames(snap.Devices)[a.DeviceID])
		v.Field("Detail", a.Detail)
		v.Field("Opened", clock(a.OpenedAt)+" ("+v.since(a.OpenedAt)+")")
		if a.ResolvedAt != nil {
			v.Field("Resolved", clock(*a.ResolvedAt))
		}
		if a.AckedAt != nil {
			v.Field("Acknowledged", clock(*a.AckedAt)+byWho(a.AckedBy))
		}
		if a.SnoozedUntil != nil {
			v.Field("Snoozed until", clock(*a.SnoozedUntil)+byWho(a.SnoozedBy))
		}
		for _, n := range a.Notes {
			v.Printf("\n%s%s\n  %s\n", v.Paint(dim, clock(n.At)), byWho(n.By), n.Text)
		}
	})
}

func byWho(login string) string {
	if login == "" {
		return ""
	}
	return " by " + login
}

// alertAction returns the command that changes an alert.
func alertAction(action string) func(*Call) error {
	return func(c *Call) error {
		id, err := alertID(c, c.Args[0])
		if err != nil {
			return err
		}
		cl, err := c.Client()
		if err != nil {
			return err
		}
		var (
			a    api.Alert
			done string
		)
		switch action {
		case "ack":
			a, err = cl.AckAlert(c.Ctx, id)
			done = "Acknowledged"
		case "unack":
			a, err = cl.UnackAlert(c.Ctx, id)
			done = "Took back the acknowledgement of"
		case "snooze":
			d := c.Duration("for")
			minutes := int((d + time.Minute - 1) / time.Minute)
			if minutes < 1 || minutes > 30*24*60 {
				return usagef(c.cmd, "--for is from 1 minute to 30 days")
			}
			a, err = cl.SnoozeAlert(c.Ctx, id, minutes)
			done = "Snoozed (" + c.Str("for") + ")"
		case "wake":
			a, err = cl.WakeAlert(c.Ctx, id)
			done = "Woke"
		case "note":
			var n api.AlertNote
			n, err = cl.NoteAlert(c.Ctx, id, strings.Join(c.Args[1:], " "))
			if err != nil {
				return err
			}
			return c.Print(n, func(v *View) { v.Printf("Wrote a note on alert #%d\n", id) })
		}
		if err != nil {
			return err
		}
		return c.Print(a, func(v *View) { v.Printf("%s alert #%d: %s\n", done, a.ID, a.Title) })
	}
}
