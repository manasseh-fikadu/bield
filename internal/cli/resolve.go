package cli

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

func notFound(format string, args ...any) error {
	return &client.Error{Status: http.StatusNotFound, Message: fmt.Sprintf(format, args...)}
}

// shortName returns the first label of a MagicDNS name.
func shortName(dns string) string {
	s, _, _ := strings.Cut(strings.TrimSuffix(dns, "."), ".")
	return s
}

// device finds the device a person means by its ID or by a name: the name
// Bield shows, the name the machine reports, or its MagicDNS name, in any
// case. A name that starts several devices' names is not enough.
func (c *Call) device(query string) (api.Device, error) {
	snap, err := c.Snapshot()
	if err != nil {
		return api.Device{}, err
	}
	return findDevice(snap.Devices, query)
}

func findDevice(devices []api.Device, query string) (api.Device, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	names := func(d api.Device) []string {
		return []string{strings.ToLower(d.Name), strings.ToLower(d.HostName), strings.ToLower(shortName(d.DNSName)), strings.ToLower(strings.TrimSuffix(d.DNSName, "."))}
	}
	var exact, prefix []api.Device
	for _, d := range devices {
		if d.ID == query {
			return d, nil
		}
		if slices.Contains(names(d), q) {
			exact = append(exact, d)
		}
		if q != "" && slices.ContainsFunc(names(d), func(n string) bool { return strings.HasPrefix(n, q) }) {
			prefix = append(prefix, d)
		}
	}
	for _, found := range [][]api.Device{exact, prefix} {
		switch len(found) {
		case 0:
			continue
		case 1:
			return found[0], nil
		}
		var list []string
		for _, d := range found {
			list = append(list, fmt.Sprintf("%s (%s)", d.Name, d.ID))
		}
		return api.Device{}, usagef(nil, "%q matches several devices: %s; use the ID", query, strings.Join(list, ", "))
	}
	return api.Device{}, notFound("no device is called %q; bield devices lists them", query)
}

// check finds a check by its ID or its name.
func (c *Call) check(query string) (api.Check, error) {
	snap, err := c.Snapshot()
	if err != nil {
		return api.Check{}, err
	}
	var byName []api.Check
	for _, ch := range snap.Checks {
		if ch.ID == query {
			return ch, nil
		}
		if strings.EqualFold(ch.Name, query) {
			byName = append(byName, ch)
		}
	}
	switch len(byName) {
	case 0:
		return api.Check{}, notFound("no check is called %q; bield checks lists them", query)
	case 1:
		return byName[0], nil
	}
	var ids []string
	for _, ch := range byName {
		ids = append(ids, fmt.Sprintf("%s (%s)", ch.Name, ch.ID))
	}
	return api.Check{}, usagef(nil, "%q matches several checks: %s; use the ID", query, strings.Join(ids, ", "))
}

// container finds a container on a device by its name or the start of its ID.
func (c *Call) container(deviceID, query string) (api.DockerContainerRow, error) {
	cl, err := c.Client()
	if err != nil {
		return api.DockerContainerRow{}, err
	}
	list, err := cl.DockerContainers(c.Ctx, deviceID)
	if err != nil {
		return api.DockerContainerRow{}, err
	}
	var prefix []api.DockerContainerRow
	for _, ct := range list.Containers {
		if ct.Name == query || ct.ID == query {
			return ct, nil
		}
		if len(query) >= 3 && strings.HasPrefix(ct.ID, query) {
			prefix = append(prefix, ct)
		}
	}
	switch len(prefix) {
	case 0:
		return api.DockerContainerRow{}, notFound("no container is called %q on this device; bield docker ps lists them", query)
	case 1:
		return prefix[0], nil
	}
	return api.DockerContainerRow{}, usagef(nil, "%q starts several containers' IDs; give more of it", query)
}
