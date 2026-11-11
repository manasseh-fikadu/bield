package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

func eventCommands() []*Command {
	return []*Command{{
		Name: "events", Run: runEvents, Stream: true,
		Summary: "show the activity log: what happened and who did it",
		What:    "The oldest comes first and the newest last, as in a log. Viewers don't see events about access, SSH, terminals or files.",
		Flags: []Flag{
			{Name: "limit", Short: 'n', Arg: "n", Kind: Int, Default: "30", Usage: "how many events to show"},
			{Name: "kind", Arg: "kind", Usage: "only one kind: device, agent, alert, check, container, access, settings, ssh, terminal, files"},
			{Name: "device", Arg: "device", Usage: "only events about one device"},
			{Name: "before", Arg: "id", Kind: Int, Usage: "show events from before this ID, for an earlier page"},
			{Name: "follow", Short: 'f', Kind: Bool, CLIOnly: true, Usage: "keep running, and print each new event"},
		},
	}}
}

func eventLine(v *View, e api.Event) {
	level := e.Level
	text := e.Message
	switch level {
	case api.EventLevelError:
		text = v.Paint(bad, text)
	case api.EventLevelWarning:
		text = v.Paint(warn, text)
	default:
	}
	by := ""
	if e.Actor != "" {
		by = v.Paint(dim, "  by "+e.Actor)
		if e.Token != "" {
			by += v.Paint(dim, " with the token "+e.Token)
		}
		switch e.Via {
		case api.ViaCLI:
			by += v.Paint(dim, comma(e.Token)+" with the CLI")
		case api.ViaMCP:
			by += v.Paint(dim, comma(e.Token)+" with MCP")
		}
	}
	v.Printf("%s  %-9s %s%s\n", v.Paint(dim, clock(e.At)), e.Kind, text, by)
}

func runEvents(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	q := client.EventsQuery{Kind: c.Str("kind"), Limit: min(max(c.Int("limit"), 1), 200), Before: int64(c.Int("before"))}
	if d := c.Str("device"); d != "" {
		dev, err := c.device(d)
		if err != nil {
			return err
		}
		q.Device = dev.ID
	}
	page, err := cl.Events(c.Ctx, q)
	if err != nil {
		return err
	}
	// The API lists the newest first. A log reads the other way.
	events := slices.Clone(page.Events)
	slices.Reverse(events)
	if c.Bool("follow") {
		return followEvents(c, q, events)
	}
	return c.Print(page, func(v *View) {
		if len(events) == 0 {
			v.Line("No events.")
			return
		}
		if page.More {
			v.Printf("%s\n", v.Paint(dim, "Earlier: bield events --before "+formatInt(events[0].ID)))
		}
		for _, e := range events {
			eventLine(v, e)
		}
	})
}

func followEvents(c *Call, q client.EventsQuery, first []api.Event) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	emit := func(e api.Event) error {
		if c.JSON() {
			return writeJSONLine(c.Env.Stdout, e)
		}
		v := c.view()
		eventLine(v, e)
		return v.err
	}
	var lastSeen int64
	for _, e := range first {
		if err := emit(e); err != nil {
			return err
		}
		lastSeen = max(lastSeen, e.ID)
	}
	err = cl.Watch(c.Ctx, func(s api.Snapshot) error {
		if s.LastEventID <= lastSeen {
			return nil
		}
		var fresh []api.Event
		fq := q
		fq.After, fq.Before = lastSeen, 0
		fq.Limit = 100
		for {
			page, err := cl.Events(c.Ctx, fq)
			if err != nil {
				return err
			}
			fresh = append(fresh, page.Events...)
			if !page.More || len(page.Events) == 0 {
				break
			}
			fq.Before = page.Events[len(page.Events)-1].ID
		}
		slices.Reverse(fresh)
		for _, e := range fresh {
			if err := emit(e); err != nil {
				return err
			}
			lastSeen = max(lastSeen, e.ID)
		}
		// Events of other kinds still move the last ID.
		lastSeen = max(lastSeen, s.LastEventID)
		return nil
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// comma returns "," if token isn't empty, so "with the token deploy, with the
// CLI" reads as two parts.
func comma(token string) string {
	if token == "" {
		return ""
	}
	return ","
}
