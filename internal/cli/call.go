package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

// A Call is one run of a command: its arguments, its flags and a way to ask
// Bield.
type Call struct {
	Ctx context.Context //nolint:containedctx // A Call is one run of a command, and this is its context.
	Env Env
	// Args are the arguments after the command's name and flags.
	Args []string

	cmd      *Command
	global   globals
	flags    map[string]string
	client   *client.Client
	snapshot *api.Snapshot
}

// Str returns the value of a flag, or its default.
func (c *Call) Str(name string) string {
	if v, ok := c.flags[name]; ok {
		return v
	}
	for _, f := range c.cmd.Flags {
		if f.Name == name {
			return f.Default
		}
	}
	return ""
}

// Given reports whether a flag was given.
func (c *Call) Given(name string) bool {
	_, ok := c.flags[name]
	return ok
}

// Bool returns the value of a boolean flag.
func (c *Call) Bool(name string) bool { return c.Str(name) == "true" }

// Int returns the value of a number flag.
func (c *Call) Int(name string) int {
	n, _ := strconv.Atoi(c.Str(name))
	return n
}

// Duration returns the value of a flag that takes a time, such as 4h or 2d.
func (c *Call) Duration(name string) time.Duration {
	d, _ := parseDuration(c.Str(name))
	return d
}

// JSON reports whether the caller asked for JSON.
func (c *Call) JSON() bool { return c.global.json }

// Now returns the time to work out ages from.
func (c *Call) Now() time.Time {
	if c.Env.Now != nil {
		return c.Env.Now()
	}
	return time.Now()
}

// checkArgs checks how many arguments there are.
func (c *Call) checkArgs() error {
	switch n := len(c.Args); {
	case n < c.cmd.Min:
		return usagef(c.cmd, "%s needs %s", c.cmd.Name, argCount(c.cmd.Min))
	case c.cmd.Max >= 0 && n > c.cmd.Max:
		return usagef(c.cmd, "%s takes at most %s, not %d", c.cmd.Name, argCount(c.cmd.Max), n)
	}
	return nil
}

func argCount(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

// Client returns the client for the Bield that the command talks to, which is
// at --url, or else BIELD_URL, or else the address bield connect saved.
func (c *Call) Client() (*client.Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	cl, err := client.New(c.clientOptions())
	if err != nil {
		return nil, err
	}
	c.client = cl
	return cl, nil
}

// clientOptions returns the options of the client for the Bield that the
// command talks to: its address, and the API token to send it, if any.
func (c *Call) clientOptions() client.Options {
	addr, _ := c.address()
	return client.Options{URL: addr, Version: c.Env.Version, HTTP: c.Env.HTTP, Token: c.token(addr)}
}

// token returns the API token to send to the Bield at addr: BIELD_TOKEN, or
// else the one bield connect saved with addr. A saved token goes to no other
// address, so --url can't send it somewhere else.
func (c *Call) token(addr string) string {
	if t := c.Env.getenv("BIELD_TOKEN"); t != "" {
		return t
	}
	cfg, err := c.Env.loadConfig()
	if err != nil || cfg.Token == "" || !sameURL(cfg.URL, addr) {
		return ""
	}
	return cfg.Token
}

// sameURL reports whether a and b are the same Bield address.
func sameURL(a, b string) bool {
	ua, errA := client.ParseURL(a)
	ub, errB := client.ParseURL(b)
	return errA == nil && errB == nil && strings.EqualFold(ua.String(), ub.String())
}

// address returns where Bield is and how the command line knows: "--url",
// "BIELD_URL" or "bield connect". It's "" if it doesn't.
func (c *Call) address() (addr, source string) {
	switch {
	case c.global.url != "":
		return c.global.url, "--url"
	case c.Env.getenv("BIELD_URL") != "":
		return c.Env.getenv("BIELD_URL"), "BIELD_URL"
	}
	if cfg, err := c.Env.loadConfig(); err == nil && cfg.URL != "" {
		return cfg.URL, "bield connect"
	}
	return "", ""
}

// Snapshot returns what Bield knows now. It asks once per run.
func (c *Call) Snapshot() (api.Snapshot, error) {
	if c.snapshot != nil {
		return *c.snapshot, nil
	}
	cl, err := c.Client()
	if err != nil {
		return api.Snapshot{}, err
	}
	s, err := cl.Snapshot(c.Ctx)
	if isNotFound(err) {
		// The route is there in every Bield that has this command line.
		v := "an older version"
		if h, herr := cl.Health(c.Ctx); herr == nil {
			v = h.Version
		}
		return api.Snapshot{}, fmt.Errorf("this Bield (%s) is older than this bield command and doesn't have GET /api/snapshot; upgrade Bield, or use a bield command of the same version", v)
	}
	if err != nil {
		return api.Snapshot{}, err
	}
	c.snapshot = &s
	return s, nil
}

// Print shows a result: as JSON, with the API's field names, if the caller
// asked for it, and otherwise with render.
func (c *Call) Print(data any, render func(v *View)) error {
	if c.global.json {
		return writeJSON(c.Env.Stdout, data)
	}
	v := c.view()
	render(v)
	return v.err
}

// view returns a View of stdout.
func (c *Call) view() *View {
	return &View{w: c.Env.Stdout, color: c.Env.Terminal, now: c.Now()}
}

// Notef writes a line for the person running the command, to stderr so that
// it stays out of what a script reads. JSON callers get nothing.
func (c *Call) Notef(format string, args ...any) {
	if !c.global.json {
		fmt.Fprintf(c.Env.Stderr, format+"\n", args...)
	}
}

// writeJSON writes v as indented JSON and a newline.
func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// writeJSONLine writes v as one line of JSON, for output that goes on.
func writeJSONLine(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// config is what bield connect saves.
type config struct {
	URL string `json:"url"`
	// Token is an API token for the Bield at URL, if bield connect was given
	// one with --with-token.
	Token string `json:"token,omitempty"`
}

func (e Env) configPath() string {
	if e.ConfigDir == "" {
		return ""
	}
	return filepath.Join(e.ConfigDir, "cli.json")
}

func (e Env) loadConfig() (config, error) {
	var cfg config
	path := e.configPath()
	if path == "" {
		return cfg, errors.New("no config directory")
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the config directory.
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(b, &cfg)
}

func (e Env) saveConfig(cfg config) error {
	path := e.configPath()
	if path == "" {
		return errors.New("this machine has no config directory to save it in; set BIELD_URL instead")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	// It may hold a token, so it's only ever the owner's: a new file, made
	// 0600, takes the old one's place, whatever the old one's mode was.
	f, err := os.CreateTemp(filepath.Dir(path), ".cli-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
