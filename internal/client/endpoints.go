package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func devicePath(device, rest string) string {
	return "/api/devices/" + url.PathEscape(device) + rest
}

func containerPath(device, cid, rest string) string {
	return devicePath(device, "/docker/containers/"+url.PathEscape(cid)+rest)
}

// Alerts returns the alerts that resolved, the latest opened first, a page at
// a time. before is the ID to list from, or 0, and limit the page size, or 0
// for the default. The ones that are open are in the Snapshot.
func (c *Client) Alerts(ctx context.Context, before int64, limit int) (l api.AlertList, err error) {
	q := url.Values{}
	if before > 0 {
		q.Set("before", strconv.FormatInt(before, 10))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return l, c.do(ctx, http.MethodGet, "/api/alerts", q, nil, &l)
}

// Alert returns one alert, open or resolved.
func (c *Client) Alert(ctx context.Context, id int64) (a api.Alert, err error) {
	return a, c.do(ctx, http.MethodGet, "/api/alerts/"+strconv.FormatInt(id, 10), nil, nil, &a)
}

func alertPath(id int64, rest string) string {
	return "/api/alerts/" + strconv.FormatInt(id, 10) + rest
}

// AckAlert acknowledges an open alert.
func (c *Client) AckAlert(ctx context.Context, id int64) (a api.Alert, err error) {
	return a, c.do(ctx, http.MethodPut, alertPath(id, "/ack"), nil, nil, &a)
}

// UnackAlert takes back the acknowledgement of an alert.
func (c *Client) UnackAlert(ctx context.Context, id int64) (a api.Alert, err error) {
	return a, c.do(ctx, http.MethodDelete, alertPath(id, "/ack"), nil, nil, &a)
}

// SnoozeAlert hides an open alert for some minutes.
func (c *Client) SnoozeAlert(ctx context.Context, id int64, minutes int) (a api.Alert, err error) {
	return a, c.do(ctx, http.MethodPut, alertPath(id, "/snooze"), nil, api.SnoozeAlert{Minutes: minutes}, &a)
}

// WakeAlert ends an alert's snooze.
func (c *Client) WakeAlert(ctx context.Context, id int64) (a api.Alert, err error) {
	return a, c.do(ctx, http.MethodDelete, alertPath(id, "/snooze"), nil, nil, &a)
}

// NoteAlert writes a note on an alert.
func (c *Client) NoteAlert(ctx context.Context, id int64, text string) (n api.AlertNote, err error) {
	return n, c.do(ctx, http.MethodPost, alertPath(id, "/notes"), nil, api.AddNote{Text: text}, &n)
}

// EventsQuery picks events from the activity log. Its zero value picks the
// latest page.
type EventsQuery struct {
	// Before and After, if they're above 0, pick the events with an ID below
	// or above them.
	Before, After int64
	// Kind and Device pick the events of a kind, or about a device.
	Kind, Device string
	// Limit is the page size, or 0 for the default.
	Limit int
}

// Events returns a page of the activity log, the newest first.
func (c *Client) Events(ctx context.Context, e EventsQuery) (l api.EventList, err error) {
	q := url.Values{}
	if e.Before > 0 {
		q.Set("before", strconv.FormatInt(e.Before, 10))
	}
	if e.After > 0 {
		q.Set("after", strconv.FormatInt(e.After, 10))
	}
	if e.Kind != "" {
		q.Set("kind", e.Kind)
	}
	if e.Device != "" {
		q.Set("device", e.Device)
	}
	if e.Limit > 0 {
		q.Set("limit", strconv.Itoa(e.Limit))
	}
	return l, c.do(ctx, http.MethodGet, "/api/events", q, nil, &l)
}

// RecentChecks returns the latest results of every check, by check ID.
func (c *Client) RecentChecks(ctx context.Context) (r api.RecentResults, err error) {
	return r, c.do(ctx, http.MethodGet, "/api/checks/recent", nil, nil, &r)
}

// CheckHistory returns what a check measured over a range: 1h, 6h, 24h or 7d.
func (c *Client) CheckHistory(ctx context.Context, id, rng string) (h api.CheckHistory, err error) {
	return h, c.do(ctx, http.MethodGet, "/api/checks/"+url.PathEscape(id)+"/history", rangeQuery(rng), nil, &h)
}

// AddCheck adds a check.
func (c *Client) AddCheck(ctx context.Context, in api.CheckInput) (ch api.Check, err error) {
	return ch, c.do(ctx, http.MethodPost, "/api/checks", nil, in, &ch)
}

// ChangeCheck replaces a check.
func (c *Client) ChangeCheck(ctx context.Context, id string, in api.CheckInput) (ch api.Check, err error) {
	return ch, c.do(ctx, http.MethodPut, "/api/checks/"+url.PathEscape(id), nil, in, &ch)
}

// DeleteCheck deletes a check.
func (c *Client) DeleteCheck(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/checks/"+url.PathEscape(id), nil, nil, nil)
}

// RunCheck runs a check now, and returns what happened.
func (c *Client) RunCheck(ctx context.Context, id string) (r api.CheckResult, err error) {
	return r, c.do(ctx, http.MethodPost, "/api/checks/"+url.PathEscape(id)+"/run", nil, nil, &r)
}

func rangeQuery(rng string) url.Values {
	if rng == "" {
		return nil
	}
	return url.Values{"range": {rng}}
}

// DeviceHistory returns a device's metrics over a range: 1h, 6h, 24h or 7d.
func (c *Client) DeviceHistory(ctx context.Context, device, rng string) (h api.DeviceHistory, err error) {
	return h, c.do(ctx, http.MethodGet, devicePath(device, "/history"), rangeQuery(rng), nil, &h)
}

// DeviceUsage returns what uses a device's CPU and memory now.
func (c *Client) DeviceUsage(ctx context.Context, device string) (u api.DeviceUsage, err error) {
	return u, c.do(ctx, http.MethodGet, devicePath(device, "/usage"), nil, nil, &u)
}

// SetDeviceSettings replaces the settings of a device.
func (c *Client) SetDeviceSettings(ctx context.Context, device string, in api.SetDeviceSettings) (s api.DeviceSettings, err error) {
	return s, c.do(ctx, http.MethodPut, devicePath(device, "/settings"), nil, in, &s)
}

// DockerOverview sums up the Docker on a device.
func (c *Client) DockerOverview(ctx context.Context, device string) (o api.DockerOverview, err error) {
	return o, c.do(ctx, http.MethodGet, devicePath(device, "/docker/overview"), nil, nil, &o)
}

// DockerContainers lists the containers on a device.
func (c *Client) DockerContainers(ctx context.Context, device string) (l api.DockerContainerList, err error) {
	return l, c.do(ctx, http.MethodGet, devicePath(device, "/docker/containers"), nil, nil, &l)
}

// DockerImages lists the images on a device.
func (c *Client) DockerImages(ctx context.Context, device string) (l []api.DockerImage, err error) {
	return l, c.do(ctx, http.MethodGet, devicePath(device, "/docker/images"), nil, nil, &l)
}

// DockerVolumes lists the volumes on a device.
func (c *Client) DockerVolumes(ctx context.Context, device string) (l []api.DockerVolume, err error) {
	return l, c.do(ctx, http.MethodGet, devicePath(device, "/docker/volumes"), nil, nil, &l)
}

// DockerNetworks lists the networks on a device.
func (c *Client) DockerNetworks(ctx context.Context, device string) (l []api.DockerNetwork, err error) {
	return l, c.do(ctx, http.MethodGet, devicePath(device, "/docker/networks"), nil, nil, &l)
}

// DockerAction starts, stops or restarts a container.
func (c *Client) DockerAction(ctx context.Context, device, cid, action string) (r api.DockerActionResult, err error) {
	switch action {
	case "start", "stop", "restart":
	default:
		return r, errors.New("a container can be started, stopped or restarted")
	}
	return r, c.do(ctx, http.MethodPost, containerPath(device, cid, "/"+action), nil, nil, &r)
}

// LogOptions say which of a container's logs to read.
type LogOptions struct {
	// Follow keeps the stream open for new lines.
	Follow bool
	// Tail is how many of the last lines to start with. 0 is Bield's default,
	// which is all of them unless Follow is set, and -1 is all of them.
	Tail int
	// Since is how far back to read, such as "10m" or an RFC 3339 time.
	Since string
	// Stdout and Stderr pick the streams. Neither set means both.
	Stdout, Stderr bool
}

// DockerLogs calls fn with each batch of a container's log lines, until the
// log ends, fn returns an error or ctx is done. It returns Docker's reason
// for ending, such as "End of log", or an error if the stream broke.
func (c *Client) DockerLogs(ctx context.Context, device, cid string, o LogOptions, fn func([]api.DockerLogLine) error) (string, error) {
	q := url.Values{}
	if !o.Follow {
		q.Set("follow", "0")
	}
	switch {
	case o.Tail > 0:
		q.Set("tail", strconv.Itoa(o.Tail))
	case o.Tail < 0:
		q.Set("tail", "all")
	}
	if o.Since != "" {
		q.Set("since", o.Since)
	}
	if o.Stdout != o.Stderr {
		if !o.Stdout {
			q.Set("stdout", "0")
		} else {
			q.Set("stderr", "0")
		}
	}
	var reason string
	err := c.stream(ctx, containerPath(device, cid, "/logs"), q, func(ev Event) error {
		switch ev.Name {
		case "lines":
			var lines []api.DockerLogLine
			if err := json.Unmarshal(ev.Data, &lines); err != nil {
				return err
			}
			return fn(lines)
		case "end":
			var end api.DockerStreamEnd
			if err := json.Unmarshal(ev.Data, &end); err != nil {
				return err
			}
			reason = end.Reason
		case "failure":
			var f api.DockerStreamFailure
			if err := json.Unmarshal(ev.Data, &f); err != nil {
				return err
			}
			return errors.New(f.Message)
		}
		return nil
	})
	return reason, err
}

// NewEnrollmentCode makes a code that enrolls one machine's agent, and the
// command that installs the agent with it.
func (c *Client) NewEnrollmentCode(ctx context.Context) (code api.AgentCode, err error) {
	return code, c.do(ctx, http.MethodPost, "/api/agent/codes", nil, nil, &code)
}

// Settings returns Bield's settings.
func (c *Client) Settings(ctx context.Context) (s api.SettingsView, err error) {
	return s, c.do(ctx, http.MethodGet, "/api/settings", nil, nil, &s)
}

// ChangeSettings changes the settings it names, and returns all of them.
func (c *Client) ChangeSettings(ctx context.Context, in api.ChangeSettings) (s api.SettingsView, err error) {
	return s, c.do(ctx, http.MethodPatch, "/api/settings", nil, in, &s)
}

// Access returns who has a role in Bield, and who else can view it.
func (c *Client) Access(ctx context.Context) (a api.Access, err error) {
	return a, c.do(ctx, http.MethodGet, "/api/access", nil, nil, &a)
}

// SetRole gives a Tailscale login a role.
func (c *Client) SetRole(ctx context.Context, login string, role api.Role) (a api.Access, err error) {
	return a, c.do(ctx, http.MethodPut, "/api/access/users/"+url.PathEscape(login), nil, api.SetRole{Role: role}, &a)
}

// RemoveRole takes a login's role away.
func (c *Client) RemoveRole(ctx context.Context, login string) (a api.Access, err error) {
	return a, c.do(ctx, http.MethodDelete, "/api/access/users/"+url.PathEscape(login), nil, nil, &a)
}

// SetWhoCanView changes who can view Bield besides the users with a role.
func (c *Client) SetWhoCanView(ctx context.Context, who api.WhoCanView) (a api.Access, err error) {
	return a, c.do(ctx, http.MethodPatch, "/api/access", nil, api.ChangeAccess{WhoCanView: who}, &a)
}

// Tokens returns the API tokens. Only a person can ask, not a token.
func (c *Client) Tokens(ctx context.Context) (ts []api.APIToken, err error) {
	return ts, c.do(ctx, http.MethodGet, "/api/tokens", nil, nil, &ts)
}

// NewToken makes an API token, and returns it with its secret, which Bield
// shows only this once.
func (c *Client) NewToken(ctx context.Context, t api.NewAPIToken) (r api.NewAPITokenResult, err error) {
	return r, c.do(ctx, http.MethodPost, "/api/tokens", nil, t, &r)
}

// RevokeToken revokes the API token with id at once.
func (c *Client) RevokeToken(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, "/api/tokens/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// ListFiles lists a folder on a device. Without a path, it's the home folder
// of the user Bield logs in as.
func (c *Client) ListFiles(ctx context.Context, device, path string) (l api.FileListing, err error) {
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	return l, c.do(ctx, http.MethodGet, devicePath(device, "/files/list"), q, nil, &l)
}

// StatFile returns what a path on a device is.
func (c *Client) StatFile(ctx context.Context, device, path string) (s api.FileStat, err error) {
	return s, c.do(ctx, http.MethodGet, devicePath(device, "/files/stat"), url.Values{"path": {path}}, nil, &s)
}

// ReadFile opens a file on a device. The caller closes it.
func (c *Client) ReadFile(ctx context.Context, device, path string) (io.ReadCloser, error) {
	req, err := c.request(ctx, http.MethodGet, devicePath(device, "/files/raw"), url.Values{"path": {path}}, nil, "")
	if err != nil {
		return nil, err
	}
	req.Header.Del("Accept")
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// UploadFile writes r to a new file called name in the folder dir on a
// device. A conflict is "fail", "overwrite" or "rename", and "" is fail.
func (c *Client) UploadFile(ctx context.Context, device, dir, name, conflict string, r io.Reader) (u api.FileUpload, err error) {
	q := url.Values{"dir": {dir}, "name": {name}}
	if conflict != "" {
		q.Set("conflict", conflict)
	}
	req, err := c.request(ctx, http.MethodPut, devicePath(device, "/files/upload"), q, r, "application/octet-stream")
	if err != nil {
		return u, err
	}
	return u, c.finish(req, &u)
}

// WriteFile saves r as the text of a file on a device, which Bield limits to
// a size an editor can open. With create, a file that isn't there is made.
func (c *Client) WriteFile(ctx context.Context, device, path string, create bool, r io.Reader) (e api.FileEntry, err error) {
	q := url.Values{"path": {path}}
	if create {
		q.Set("create", "1")
	}
	req, err := c.request(ctx, http.MethodPut, devicePath(device, "/files/write"), q, r, "application/octet-stream")
	if err != nil {
		return e, err
	}
	return e, c.finish(req, &e)
}

// finish sends req and decodes the JSON answer into out.
func (c *Client) finish(req *http.Request, out any) error {
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return json.NewDecoder(resp.Body).Decode(out)
}
