package cli

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// editorPoll is how often editor install asks whether the install is over.
var editorPoll = 2 * time.Second

func editorCommands() []*Command {
	const how = "Bield runs code-server on the device, as the user it logs in as over SSH, " +
		"and serves each device's editor on a port of its own, for admins in a browser."
	return []*Command{
		{
			Name: "editor", Args: "<device>", Min: 1, Max: 1, Admin: true, ReadOnly: true, Run: runEditor,
			Summary: "show VS Code on a device",
			What:    how + " This says whether it's installed and running, and where it is.",
		},
		{
			Name: "editor install", Args: "<device>", Min: 1, Max: 1, Admin: true, Run: runEditorInstall, Wait: 20 * time.Minute,
			Summary: "install VS Code on a device, or update it",
			What: "It downloads code-server's build for the device from GitHub on the device, about 230 MB, " +
				"checks it against the SHA-256 Bield pins, and waits until it's installed. An editor that runs stops first.",
		},
		{
			Name: "editor open", Args: "<device> [folder]", Min: 1, Max: 2, Admin: true, Run: runEditorOpen,
			NoTool:  "an editor is for a person, in a browser, and its port lets no API token in",
			Summary: "start VS Code on a device, and print where to open it",
			What:    "Open the address in a browser on a device of the tailnet. Without a folder, it opens the one it had open last.",
		},
		{
			Name: "editor stop", Args: "<device>", Min: 1, Max: 1, Admin: true, Destructive: true, Run: runEditorStop,
			Summary: "stop VS Code on a device",
			What:    "It closes the editor for everyone who has it open. It stops by itself after 30 minutes with no one connected.",
		},
		{
			Name: "editor remove", Args: "<device>", Min: 1, Max: 1, Admin: true, Destructive: true, Run: runEditorRemove,
			Summary: "remove VS Code from a device",
			What:    "It deletes code-server, with its settings and extensions unless you say --keep-data.",
			Flags:   []Flag{{Name: "keep-data", Kind: Bool, Usage: "keep the settings and the extensions for the next install"}},
		},
	}
}

func runEditor(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	e, err := cl.Editor(c.Ctx, d.ID)
	if err != nil {
		return err
	}
	return c.Print(e, func(v *View) {
		v.Printf("%s\n", v.Paint(bold, "VS Code on "+d.Name))
		state := string(e.State)
		switch e.State {
		case api.EditorStateInstalled:
			state = v.Paint(good, "installed")
			if e.Running {
				state += ", running"
			} else {
				state += ", " + v.Paint(dim, "stopped")
			}
		case api.EditorStateNotInstalled:
			state = "not installed"
		case api.EditorStateMissing:
			state = v.Paint(bad, "its files are gone from the device; install it again")
		case api.EditorStateUnavailable:
			state = v.Paint(dim, "unavailable")
		}
		v.Field("State", state)
		if e.Reason != "" {
			v.Field("Why", e.Reason)
		}
		if e.Version != "" {
			version := "code-server " + e.Version
			if e.Latest != "" && e.Version != e.Latest {
				version += v.Paint(warn, " (Bield installs "+e.Latest+": bield editor install "+d.Name+")")
			}
			v.Field("Version", version)
		}
		if e.URL != "" {
			v.Field("Address", e.URL)
		}
		if e.ListenError != "" {
			v.Field("Port", v.Paint(bad, e.ListenError))
		}
		if len(e.OpenBy) > 0 {
			v.Field("Open by", strings.Join(e.OpenBy, ", "))
		}
		if e.Install != nil {
			v.Field("Install", installLine(v, e.Install))
		}
		if e.Error != "" {
			v.Field("Error", v.Paint(bad, e.Error))
		}
	})
}

func installLine(v *View, j *api.EditorInstall) string {
	switch {
	case j.Running:
		return "code-server " + j.Version + ", started " + v.ago(j.StartedAt) + " by " + j.By
	case j.OK:
		return v.Paint(good, "code-server "+j.Version+" installed") + " by " + j.By + ", " + v.ago(j.StartedAt)
	}
	return v.Paint(bad, "failed") + ", started " + v.ago(j.StartedAt) + " by " + j.By
}

// errInstallFailed is what editor install returns when the install didn't
// work, once it has printed what the device said.
var errInstallFailed = errors.New("the install failed")

func runEditorInstall(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	job, err := cl.InstallEditor(c.Ctx, d.ID)
	if err != nil {
		return err
	}
	c.Notef("Installing code-server %s on %s, which downloads about 230 MB…", job.Version, d.Name)
	t := time.NewTicker(editorPoll)
	defer t.Stop()
	for {
		select {
		case <-c.Ctx.Done():
			return c.Ctx.Err()
		case <-t.C:
		}
		e, err := cl.Editor(c.Ctx, d.ID)
		if err != nil {
			return err
		}
		if e.Install == nil || e.Install.Running {
			continue
		}
		done := *e.Install
		err = c.Print(done, func(v *View) {
			if done.Output != "" {
				v.Line(done.Output)
			}
			if done.OK {
				v.Printf("%s\n", v.Paint(good, "Installed VS Code (code-server "+done.Version+") on "+d.Name))
			}
		})
		if err == nil && !done.OK {
			err = errInstallFailed
		}
		return err
	}
}

func runEditorOpen(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	o, err := cl.StartEditor(c.Ctx, d.ID)
	if err != nil {
		return err
	}
	if len(c.Args) > 1 {
		u, err := url.Parse(o.URL)
		if err != nil {
			return err
		}
		u.RawQuery = url.Values{"folder": {c.Args[1]}}.Encode()
		o.URL = u.String()
	}
	return c.Print(o, func(v *View) { v.Line(o.URL) })
}

func runEditorStop(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if err := cl.StopEditor(c.Ctx, d.ID); err != nil {
		return err
	}
	out := struct {
		Device  string `json:"device"`
		Stopped bool   `json:"stopped"`
	}{d.ID, true}
	return c.Print(out, func(v *View) { v.Line("Stopped VS Code on " + d.Name) })
}

func runEditorRemove(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	r, err := cl.RemoveEditor(c.Ctx, d.ID, c.Bool("keep-data"))
	if err != nil {
		return err
	}
	err = c.Print(r, func(v *View) {
		if r.Output != "" {
			v.Line(r.Output)
		}
	})
	if err == nil && !r.OK {
		err = errors.New("the removal failed")
	}
	return err
}
