package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func adminCommands() []*Command {
	return []*Command{
		{
			Name: "enroll", Admin: true, Run: runEnroll,
			Summary: "make an install command that adds a machine's agent",
			What: "Prints a command to run on the machine. It works on one machine, for an hour. The agent reports the machine's " +
				"CPU, memory, disk and Docker to Bield.",
		},
		{
			Name: "settings", Admin: true, ReadOnly: true, Run: runSettings,
			Summary: "show Bield's settings and what they are by default",
			What:    "Names are the API's, such as thresholds.diskWarn or rules.cpu. --json has the full structure.",
		},
		{
			Name: "settings set", Args: "<name=value>...", Min: 1, Max: -1, Admin: true, Run: runSettingsSet,
			Summary: "change settings; only the ones you name change",
			What: "A value is a number, true or false, text, or for watchedUnits a comma-separated list. " +
				"Example: bield settings set thresholds.diskWarn=85 rules.cpu=false general.name=\"Home lab\"",
		},
		{Name: "access", Admin: true, ReadOnly: true, Run: runAccess, Summary: "show who has a role in Bield, and who else can view it"},
		{
			Name: "access set", Args: "<login> <admin|viewer>", Min: 2, Max: 2, Admin: true, Destructive: true, Run: runAccessSet,
			Summary: "give a Tailscale login a role",
			What:    "Admins can open shells and change files on every device. So this needs --yes.",
		},
		{
			Name: "access remove", Args: "<login>", Min: 1, Max: 1, Admin: true, Destructive: true, Run: runAccessRemove,
			Summary: "take a login's role away",
		},
		{
			Name: "access who-can-view", Args: "[tailnet|listed]", Max: 1, Admin: true, Run: runWhoCanView,
			Summary: "show or set who can view Bield besides the users with a role",
			What:    "tailnet lets anyone who signs in from the tailnet view it; listed lets only the users with a role. Choosing tailnet needs --yes.",
		},
	}
}

// Yes reports whether the caller passed --yes.
func (c *Call) Yes() bool { return c.global.yes }

func runEnroll(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	code, err := cl.NewEnrollmentCode(c.Ctx)
	if err != nil {
		return err
	}
	return c.Print(code, func(v *View) {
		// The command alone on stdout, so a script can run it or pass it on.
		v.Line(code.InstallCommand)
		c.Notef("Run this on the machine to add. It works on one machine, until %s.", clock(code.ExpiresAt))
	})
}

func runSettings(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	s, err := cl.Settings(c.Ctx)
	if err != nil {
		return err
	}
	return c.Print(s, func(v *View) {
		cur, def := flatten(s.Settings), flatten(s.Defaults)
		keys := make([]string, 0, len(cur))
		for k := range cur {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		t := v.Table("NAME", "VALUE", "DEFAULT")
		for _, k := range keys {
			val := cur[k]
			if val != def[k] {
				val = v.Paint(warn, val)
			}
			t.Row(k, val, v.Paint(dim, def[k]))
		}
		t.Write()
		v.Printf("\nTailscale: %s. Names in color differ from the default.\n", s.Tailscale)
	})
}

// flatten writes a value as JSON and lists its leaves under dotted names.
func flatten(v any) map[string]string {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	out := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, e)
			}
		case []any:
			var parts []string
			for _, e := range x {
				parts = append(parts, fmt.Sprint(e))
			}
			out[prefix] = strings.Join(parts, ",")
		case nil:
			out[prefix] = ""
		default:
			out[prefix] = fmt.Sprint(x)
		}
	}
	walk("", m)
	return out
}

func runSettingsSet(c *Call) error {
	tree := map[string]any{}
	for _, arg := range c.Args {
		key, val, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return usagef(c.cmd, "%q isn't name=value", arg)
		}
		parsed, err := settingValue(key, val)
		if err != nil {
			return usagef(c.cmd, "%v", err)
		}
		node := tree
		parts := strings.Split(key, ".")
		for _, p := range parts[:len(parts)-1] {
			next, ok := node[p].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[p] = next
			}
			node = next
		}
		node[parts[len(parts)-1]] = parsed
	}
	in, err := decodeChange(tree)
	if err != nil {
		return usagef(c.cmd, "%v", err)
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	s, err := cl.ChangeSettings(c.Ctx, in)
	if err != nil {
		return err
	}
	return c.Print(s, func(v *View) { v.Printf("Changed %s\n", plural(len(c.Args), "setting", "settings")) })
}

var errUnknownSetting = errors.New("there is no setting called that")

// decodeChange decodes the settings to change, and refuses a name it doesn't know.
func decodeChange(tree map[string]any) (api.ChangeSettings, error) {
	var in api.ChangeSettings
	b, err := json.Marshal(tree)
	if err != nil {
		return in, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		if _, name, ok := strings.Cut(err.Error(), "unknown field "); ok {
			return in, fmt.Errorf("%w: %s; bield settings lists them", errUnknownSetting, strings.Trim(name, `"`))
		}
		return in, fmt.Errorf("a value has the wrong form: %w", err)
	}
	return in, nil
}

// settingValue reads a value: JSON if it is (a number, true, false), a list
// for watchedUnits, and else text. It tries the way that fits the setting.
func settingValue(key, val string) (any, error) {
	if key == "watchedUnits" {
		if val == "" {
			return []string{}, nil
		}
		return strings.Split(val, ","), nil
	}
	var parsed any
	if json.Unmarshal([]byte(val), &parsed) == nil {
		switch parsed.(type) {
		case float64, bool:
			// Text that looks like a number, such as a name, is tried as text
			// below if the setting turns out to be text.
			err := probe(key, parsed)
			if err == nil {
				return parsed, nil
			}
			if errors.Is(err, errUnknownSetting) {
				return nil, err
			}
		}
	}
	if err := probe(key, val); err != nil {
		if errors.Is(err, errUnknownSetting) {
			return nil, err
		}
		return nil, fmt.Errorf("%s can't be %q", key, val)
	}
	return val, nil
}

// probe checks that a setting takes a value, by decoding it alone.
func probe(key string, val any) error {
	tree := map[string]any{}
	node := tree
	parts := strings.Split(key, ".")
	for _, p := range parts[:len(parts)-1] {
		next := map[string]any{}
		node[p] = next
		node = next
	}
	node[parts[len(parts)-1]] = val
	_, err := decodeChange(tree)
	return err
}

func runAccess(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	a, err := cl.Access(c.Ctx)
	if err != nil {
		return err
	}
	return c.Print(a, func(v *View) {
		t := v.Table("LOGIN", "ROLE", "SINCE")
		for _, u := range a.Users {
			t.Row(u.Login, string(u.Role), clock(u.Since))
		}
		t.Write()
		who := "only the users above"
		if a.WhoCanView == api.WhoCanViewTailnet {
			who = "anyone who signs in from the tailnet, as a viewer"
		}
		v.Printf("\nWho can view Bield: %s\n", who)
	})
}

func runAccessSet(c *Call) error {
	role := api.Role(c.Args[1])
	if role != api.RoleAdmin && role != api.RoleViewer {
		return usagef(c.cmd, "a role is admin or viewer, not %q", c.Args[1])
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	a, err := cl.SetRole(c.Ctx, c.Args[0], role)
	if err != nil {
		return err
	}
	return c.Print(a, func(v *View) { v.Printf("%s is now %s\n", c.Args[0], role) })
}

func runAccessRemove(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	a, err := cl.RemoveRole(c.Ctx, c.Args[0])
	if err != nil {
		return err
	}
	return c.Print(a, func(v *View) { v.Printf("%s no longer has a role\n", c.Args[0]) })
}

func runWhoCanView(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	if len(c.Args) == 0 {
		a, err := cl.Access(c.Ctx)
		if err != nil {
			return err
		}
		return c.Print(map[string]api.WhoCanView{"whoCanView": a.WhoCanView}, func(v *View) { v.Line(string(a.WhoCanView)) })
	}
	who := api.WhoCanView(c.Args[0])
	switch who {
	case api.WhoCanViewListed:
	case api.WhoCanViewTailnet:
		if !c.Yes() {
			return usagef(c.cmd, "letting anyone on the tailnet view Bield needs --yes")
		}
	default:
		return usagef(c.cmd, "who can view is tailnet or listed, not %q", c.Args[0])
	}
	a, err := cl.SetWhoCanView(c.Ctx, who)
	if err != nil {
		return err
	}
	return c.Print(a, func(v *View) { v.Printf("Who can view Bield: %s\n", a.WhoCanView) })
}
