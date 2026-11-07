package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

func dockerCommands() []*Command {
	const optional = "[device]"
	const what = "Without a device, it lists the Docker of every device that has any."
	return []*Command{
		{
			Name: "docker ps", Args: optional, Max: 1, Run: runDockerPs, What: what,
			Summary: "list containers",
			Flags:   []Flag{{Name: "all", Short: 'a', Kind: Bool, Usage: "include containers that aren't running"}},
		},
		{Name: "docker images", Args: optional, Max: 1, Run: runDockerImages, What: what, Summary: "list images"},
		{Name: "docker volumes", Args: optional, Max: 1, Run: runDockerVolumes, What: what, Summary: "list volumes"},
		{Name: "docker networks", Args: optional, Max: 1, Run: runDockerNetworks, What: what, Summary: "list networks"},
		{Name: "docker start", Args: "<device> <container>", Min: 2, Max: 2, Admin: true, Run: dockerAction("start"), Summary: "start a container"},
		{Name: "docker stop", Args: "<device> <container>", Min: 2, Max: 2, Admin: true, Destructive: true, Run: dockerAction("stop"), Summary: "stop a container"},
		{Name: "docker restart", Args: "<device> <container>", Min: 2, Max: 2, Admin: true, Destructive: true, Run: dockerAction("restart"), Summary: "restart a container"},
		{
			Name: "docker logs", Args: "<device> <container>", Min: 2, Max: 2, Admin: true, Stream: true, Run: runDockerLogs,
			Summary: "print a container's logs, or follow them",
			What:    "Logs often hold secrets, so only admins can read them. A container's error output goes to stderr.",
			Flags: []Flag{
				{Name: "tail", Short: 'n', Arg: "n", Kind: Int, Default: "100", Usage: "how many of the last lines to start with, or 0 for all"},
				{Name: "since", Arg: "time", Usage: "only lines since a time ago, such as 10m, or an RFC 3339 time"},
				{Name: "follow", Short: 'f', Kind: Bool, Usage: "keep running, and print new lines"},
				{Name: "timestamps", Short: 't', Kind: Bool, Usage: "put the time Docker stamped on each line before it"},
			},
		},
	}
}

// dockerDevices returns the devices a docker command is about: the one that
// is named, or else every one that has Docker.
func dockerDevices(c *Call) ([]api.Device, error) {
	if len(c.Args) > 0 {
		d, err := c.device(c.Args[0])
		if err != nil {
			return nil, err
		}
		return []api.Device{d}, nil
	}
	snap, err := c.Snapshot()
	if err != nil {
		return nil, err
	}
	var out []api.Device
	for _, d := range snap.Devices {
		if d.Docker {
			out = append(out, d)
		}
	}
	return out, nil
}

// onDockerDevices runs fn for each device a docker command is about. When it
// is every device, one that fails is reported and skipped, unless all do.
func onDockerDevices(c *Call, fn func(d api.Device) error) error {
	devices, err := dockerDevices(c)
	if err != nil {
		return err
	}
	var firstErr error
	failed := 0
	for _, d := range devices {
		if err := fn(d); err != nil {
			if len(c.Args) > 0 {
				return err
			}
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", d.Name, err)
			}
			c.Notef("bield: %s: %v", d.Name, err)
		}
	}
	if failed > 0 && failed == len(devices) {
		return firstErr
	}
	return nil
}

func containerPaint(v *View, s api.ContainerState, health string) string {
	text := string(s)
	if health != "" && s == api.ContainerStateRunning {
		text += " (" + health + ")"
	}
	switch {
	case health == "unhealthy", s == api.ContainerStateRestarting, s == api.ContainerStateDead:
		return v.Paint(bad, text)
	case s == api.ContainerStateRunning:
		return v.Paint(good, text)
	case s == api.ContainerStateExited, s == api.ContainerStateCreated:
		return v.Paint(dim, text)
	}
	return text
}

func portsLabel(ports []api.ContainerPort) string {
	var out []string
	for _, p := range ports {
		switch {
		case p.Public != 0:
			out = append(out, fmt.Sprintf("%d->%d", p.Public, p.Private))
		default:
			out = append(out, strconv.Itoa(p.Private))
		}
	}
	return strings.Join(out, ",")
}

// dockerRow is a container with the device it is on.
type dockerRow struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	api.DockerContainerRow
}

func runDockerPs(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	rows := []dockerRow{}
	err = onDockerDevices(c, func(d api.Device) error {
		list, err := cl.DockerContainers(c.Ctx, d.ID)
		if err != nil {
			return err
		}
		for _, ct := range list.Containers {
			if c.Bool("all") || ct.State == api.ContainerStateRunning || ct.State == api.ContainerStateRestarting {
				rows = append(rows, dockerRow{d.ID, d.Name, ct})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.Print(rows, func(v *View) {
		if len(rows) == 0 {
			v.Line("No containers.")
			return
		}
		t := v.Table("DEVICE", "NAME", "IMAGE", "STATE", "STATUS", "CPU", "MEM", "PORTS")
		for _, r := range rows {
			cpu, mem := "-", "-"
			if r.Stats != nil {
				cpu = percent(r.Stats.CPU)
				if r.Stats.MemUsed != nil {
					mem = bytesLabel(uint64(max(*r.Stats.MemUsed, 0)))
				}
			}
			t.Row(r.DeviceName, r.Name, oneLine(r.Image, 40), containerPaint(v, r.State, r.Health), oneLine(r.Status, 30), cpu, mem, oneLine(portsLabel(r.Ports), 30))
		}
		t.Write()
	})
}

func runDockerImages(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	type row struct {
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
		api.DockerImage
	}
	rows := []row{}
	err = onDockerDevices(c, func(d api.Device) error {
		list, err := cl.DockerImages(c.Ctx, d.ID)
		for _, im := range list {
			rows = append(rows, row{d.ID, d.Name, im})
		}
		return err
	})
	if err != nil {
		return err
	}
	return c.Print(rows, func(v *View) {
		if len(rows) == 0 {
			v.Line("No images.")
			return
		}
		t := v.Table("DEVICE", "IMAGE", "ID", "SIZE", "AGE", "USED BY")
		for _, r := range rows {
			name := "<none>"
			if len(r.Tags) > 0 {
				name = strings.Join(r.Tags, ", ")
			}
			var users []string
			for _, u := range r.UsedBy {
				users = append(users, u.Name)
			}
			t.Row(r.DeviceName, oneLine(name, 50), shortID(r.ID), bytesLabel(uint64(max(r.Size, 0))), v.ago(r.Created), oneLine(strings.Join(users, ", "), 40))
		}
		t.Write()
	})
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	return id[:min(len(id), 12)]
}

func runDockerVolumes(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	type row struct {
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
		api.DockerVolume
	}
	rows := []row{}
	err = onDockerDevices(c, func(d api.Device) error {
		list, err := cl.DockerVolumes(c.Ctx, d.ID)
		for _, vol := range list {
			rows = append(rows, row{d.ID, d.Name, vol})
		}
		return err
	})
	if err != nil {
		return err
	}
	return c.Print(rows, func(v *View) {
		if len(rows) == 0 {
			v.Line("No volumes.")
			return
		}
		t := v.Table("DEVICE", "VOLUME", "DRIVER", "SIZE", "USED BY")
		for _, r := range rows {
			size := "-"
			if r.Size != nil {
				size = bytesLabel(uint64(max(*r.Size, 0)))
			}
			var users []string
			for _, u := range r.UsedBy {
				users = append(users, u.Name)
			}
			t.Row(r.DeviceName, oneLine(r.Name, 50), r.Driver, size, oneLine(strings.Join(users, ", "), 40))
		}
		t.Write()
	})
}

func runDockerNetworks(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	type row struct {
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
		api.DockerNetwork
	}
	rows := []row{}
	err = onDockerDevices(c, func(d api.Device) error {
		list, err := cl.DockerNetworks(c.Ctx, d.ID)
		for _, n := range list {
			rows = append(rows, row{d.ID, d.Name, n})
		}
		return err
	})
	if err != nil {
		return err
	}
	return c.Print(rows, func(v *View) {
		if len(rows) == 0 {
			v.Line("No networks.")
			return
		}
		t := v.Table("DEVICE", "NETWORK", "DRIVER", "SCOPE", "SUBNETS", "CONTAINERS")
		for _, r := range rows {
			var subnets []string
			for _, s := range r.Subnets {
				subnets = append(subnets, s.Subnet)
			}
			t.Row(r.DeviceName, r.Name, r.Driver, r.Scope, strings.Join(subnets, ","), strconv.Itoa(len(r.Containers)))
		}
		t.Write()
	})
}

func dockerAction(action string) func(*Call) error {
	return func(c *Call) error {
		d, err := c.device(c.Args[0])
		if err != nil {
			return err
		}
		ct, err := c.container(d.ID, c.Args[1])
		if err != nil {
			return err
		}
		cl, err := c.Client()
		if err != nil {
			return err
		}
		res, err := cl.DockerAction(c.Ctx, d.ID, ct.ID, action)
		if err != nil {
			return err
		}
		past := map[string]string{"start": "Started", "stop": "Stopped", "restart": "Restarted"}[action]
		out := struct {
			Device    string `json:"device"`
			Container string `json:"container"`
			Action    string `json:"action"`
			Changed   bool   `json:"changed"`
		}{d.ID, ct.Name, action, res.Changed}
		return c.Print(out, func(v *View) {
			if !res.Changed {
				v.Printf("%s on %s was already %s\n", ct.Name, d.Name, map[string]string{"start": "running", "stop": "stopped", "restart": "restarted"}[action])
				return
			}
			v.Printf("%s %s on %s\n", past, ct.Name, d.Name)
		})
	}
}

func runDockerLogs(c *Call) error {
	d, err := c.device(c.Args[0])
	if err != nil {
		return err
	}
	ct, err := c.container(d.ID, c.Args[1])
	if err != nil {
		return err
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	tail := c.Int("tail")
	if tail == 0 {
		tail = -1
	}
	opts := client.LogOptions{Follow: c.Bool("follow"), Tail: tail, Since: c.Str("since"), Stdout: true, Stderr: true}
	_, err = cl.DockerLogs(c.Ctx, d.ID, ct.ID, opts, func(lines []api.DockerLogLine) error {
		for _, l := range lines {
			if c.JSON() {
				if err := writeJSONLine(c.Env.Stdout, l); err != nil {
					return err
				}
				continue
			}
			w := c.Env.Stdout
			if l.S == 2 {
				w = c.Env.Stderr
			}
			text := l.M
			if c.Bool("timestamps") && l.T != "" {
				text = l.T + " " + text
			}
			if _, err := fmt.Fprintln(w, text); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
