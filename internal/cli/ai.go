package cli

func aiCommands() []*Command {
	return []*Command{
		{
			Name: "ai watch", Max: 0, Admin: true, Run: aiWatch(true),
			Summary: "turn on watching the AI tools on every machine that has an agent",
			What: "Each machine's agent then reads the session files of Claude Code, Codex, OpenCode and Pi once a minute, " +
				"and sends Bield counts of tokens. It needs agent 1.4.0 or newer. Machines that are watched already stay as they are.",
		},
		{
			Name: "ai unwatch", Max: 0, Admin: true, Destructive: true, Run: aiWatch(false),
			Summary: "turn off watching the AI tools on every machine",
			What: "Bield stops counting what they use, and keeps what it counted. " +
				"Which machines were watched is lost: ai watch turns it on for every machine that has an agent.",
		},
	}
}

func aiWatch(on bool) func(*Call) error {
	return func(c *Call) error {
		cl, err := c.Client()
		if err != nil {
			return err
		}
		w, err := cl.WatchAllCoding(c.Ctx, on)
		if err != nil {
			return err
		}
		return c.Print(w, func(v *View) {
			switch {
			case w.Changed == 0 && on:
				v.Line("Every machine with an agent was watched already")
			case w.Changed == 0:
				v.Line("No machine was watched")
			case on:
				v.Line("Turned on watching the AI tools on " + plural(w.Changed, "machine", "machines"))
			default:
				v.Line("Turned off watching the AI tools on " + plural(w.Changed, "machine", "machines"))
			}
		})
	}
}
