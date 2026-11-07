package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

func basicCommands() []*Command {
	return []*Command{
		{
			Name: "connect", Args: "[url]", Max: 1, Run: runConnect,
			Summary: "save where Bield is, and check that it answers",
			What: "Bield's address is the MagicDNS name of its node, such as https://bield.example.ts.net. " +
				"This machine has to be on the same tailnet. Without an address, it shows the one in use.",
		},
		{Name: "whoami", Summary: "show who Bield thinks you are, and your role", Run: runWhoami},
		{Name: "status", Summary: "sum up the tailnet: health, devices and open alerts", Run: runStatus},
	}
}

func runConnect(c *Call) error {
	if len(c.Args) == 0 {
		addr, source := c.address()
		if addr == "" {
			return usagef(c.cmd, "no Bield address yet")
		}
		return c.Print(map[string]string{"url": addr, "source": source}, func(v *View) {
			v.Printf("%s (from %s)\n", addr, source)
		})
	}
	u, err := client.ParseURL(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := client.New(client.Options{URL: u.String(), Version: c.Env.Version, HTTP: c.Env.HTTP})
	if err != nil {
		return err
	}
	c.client = cl
	h, err := cl.Health(c.Ctx)
	if err != nil {
		return err
	}
	me, err := cl.Me(c.Ctx)
	if err != nil {
		return err
	}
	saved := true
	if err := c.Env.saveConfig(config{URL: u.String()}); err != nil {
		saved = false
		c.Notef("bield: couldn't save the address: %v", err)
	}
	return c.Print(struct {
		URL     string `json:"url"`
		Version string `json:"version"`
		Saved   bool   `json:"saved"`
		api.Me
	}{u.String(), h.Version, saved, me}, func(v *View) {
		v.Printf("Connected to Bield %s at %s\n", h.Version, u)
		v.Printf("%s\n", describeMe(me))
		if hint := client.Hint(me); hint != "" {
			v.Printf("%s\n", capitalize(hint))
		}
	})
}

// describeMe says who Bield thinks a caller is.
func describeMe(me api.Me) string {
	switch {
	case !me.SignedIn:
		return "Bield doesn't know who you are"
	case me.Role == "":
		return fmt.Sprintf("You are %s on %s, and have no role", me.Login, me.Device)
	}
	return fmt.Sprintf("You are %s on %s, with the role %s", me.Login, me.Device, me.Role)
}

func runWhoami(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	me, err := cl.Me(c.Ctx)
	if err != nil {
		return err
	}
	if err := c.Print(me, func(v *View) { v.Line(describeMe(me)) }); err != nil {
		return err
	}
	if me.Role == "" {
		// Nothing the caller can do works, and a script should be able to tell.
		hint := client.Hint(me)
		if hint == "" {
			hint = "Bield doesn't let you view it"
		}
		return &client.Error{Status: http.StatusForbidden, Message: "you have no role in Bield", Hint: hint}
	}
	return nil
}

// Status is the output of bield status.
type Status struct {
	Hub     api.Hub         `json:"hub"`
	Me      api.Me          `json:"me"`
	Health  api.HealthScore `json:"health"`
	Devices StatusDevices   `json:"devices"`
	Alerts  StatusAlerts    `json:"alerts"`
}

// StatusDevices counts the devices by status.
type StatusDevices struct {
	Total     int `json:"total"`
	Online    int `json:"online"`
	Attention int `json:"attention"`
	Offline   int `json:"offline"`
	Unknown   int `json:"unknown"`
}

// StatusAlerts counts the alerts that aren't resolved.
type StatusAlerts struct {
	Open         int `json:"open"`
	Critical     int `json:"critical"`
	Warning      int `json:"warning"`
	Acknowledged int `json:"acknowledged"`
	Snoozed      int `json:"snoozed"`
}

func runStatus(c *Call) error {
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	me, err := cl.Me(c.Ctx)
	if err != nil {
		return err
	}
	s := Status{Hub: snap.Hub, Me: me, Health: snap.Health}
	s.Devices.Total = len(snap.Devices)
	for _, d := range snap.Devices {
		switch d.Status {
		case api.StatusOnline:
			s.Devices.Online++
		case api.StatusAttention:
			s.Devices.Attention++
		case api.StatusOffline:
			s.Devices.Offline++
		default:
			s.Devices.Unknown++
		}
	}
	for _, a := range snap.Alerts {
		switch a.State {
		case api.AlertStateAcknowledged:
			s.Alerts.Acknowledged++
		case api.AlertStateSnoozed:
			s.Alerts.Snoozed++
		default:
			s.Alerts.Open++
		}
		switch a.Severity {
		case api.AlertSeverityCritical:
			s.Alerts.Critical++
		case api.AlertSeverityWarning:
			s.Alerts.Warning++
		default:
		}
	}
	return c.Print(s, func(v *View) {
		v.Printf("%s %s at %s\n", v.Paint(bold, snap.Hub.Name), snap.Hub.Version, snap.Hub.DNSName)
		v.Printf("%s\n\n", describeMe(me))
		h := snap.Health
		score := "no live checks"
		if h.Score != nil {
			score = fmt.Sprintf("%d%% %s (%d passing, %d failing)", *h.Score, h.Label, h.Passing, h.Failing)
		}
		v.Field("Health", score)
		v.Field("Devices", fmt.Sprintf("%d: %d online, %d need attention, %d offline",
			s.Devices.Total, s.Devices.Online, s.Devices.Attention, s.Devices.Offline))
		alerts := "none open"
		if n := s.Alerts.Open + s.Alerts.Acknowledged + s.Alerts.Snoozed; n > 0 {
			alerts = fmt.Sprintf("%d: %d critical, %d warning (%d acknowledged, %d snoozed)",
				n, s.Alerts.Critical, s.Alerts.Warning, s.Alerts.Acknowledged, s.Alerts.Snoozed)
		}
		v.Field("Alerts", alerts)
	})
}

// isNotFound reports whether err says there's no such thing.
func isNotFound(err error) bool {
	var ce *client.Error
	return errors.As(err, &ce) && ce.NotFound()
}
