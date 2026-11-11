package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/api/apitest"
	"github.com/manasseh-fikadu/bield/internal/cli"
)

// syncBuffer is a bytes.Buffer that one goroutine can write while another reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// result is what a run of a command printed and exited with.
type result struct {
	stdout, stderr string
	code           int
}

// env returns an Env for a run from the device at from on the test tailnet.
func env(t *testing.T, s *apitest.Server, from netip.Addr) (cli.Env, *syncBuffer, *syncBuffer) {
	t.Helper()
	var stdout, stderr syncBuffer
	return cli.Env{
		Stdin:     strings.NewReader(""),
		Stdout:    &stdout,
		Stderr:    &stderr,
		Getenv:    func(k string) string { return map[string]string{"BIELD_URL": apitest.URL}[k] },
		ConfigDir: t.TempDir(),
		Version:   "9.9.9",
		HTTP:      s.Client(from),
	}, &stdout, &stderr
}

// run runs bield with args as the device at from.
func run(t *testing.T, s *apitest.Server, from netip.Addr, args ...string) result {
	t.Helper()
	e, stdout, stderr := env(t, s, from)
	code := cli.Run(t.Context(), e, args)
	return result{stdout.String(), stderr.String(), code}
}

func (r result) want(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	if r.code != code {
		t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
	for _, want := range []struct{ name, got, sub string }{{"stdout", r.stdout, stdout}, {"stderr", r.stderr, stderr}} {
		if !strings.Contains(want.got, want.sub) {
			t.Errorf("%s = %q, want it to have %q", want.name, want.got, want.sub)
		}
	}
}

func openAlert(t *testing.T, s *apitest.Server, key, title string) int64 {
	t.Helper()
	s.Alerts.Apply(t.Context(), []alerts.Condition{{
		Key: key, Source: "metrics", DeviceID: "bield", Severity: alerts.Warning, Title: title,
	}}, func(string, string, string) bool { return true }, time.Now())
	s.Feed.Poke()
	for _, a := range s.Alerts.Open() {
		if a.Title == title {
			return a.ID
		}
	}
	t.Fatalf("alert %q didn't open", title)
	return 0
}

func TestRun_whoami(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	for _, tt := range []struct {
		name         string
		from         netip.Addr
		code         int
		stdout, errs string
	}{
		{"an admin", apitest.LaptopIP, 0, "You are alice@example.com on alice-laptop.example.ts.net, with the role admin", ""},
		{"a viewer", apitest.PhoneIP, 0, "with the role viewer", ""},
		{"someone with no role", apitest.TabletIP, 4, "and have no role", "ask one of its admins to make you a viewer"},
		{"a tagged server", apitest.ServerIP, 4, "Bield doesn't know who you are", "This machine is tagged in Tailscale"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run(t, s, tt.from, "whoami").want(t, tt.code, tt.stdout, tt.errs)
		})
	}
}

// A caller who is refused hears who they are and what to do, not a bare 403,
// with the exit code for it.
func TestRun_refusals(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	ack := []string{"alerts", "ack", itoa(id)}

	run(t, s, apitest.PhoneIP, ack...).want(t, cli.ExitDenied, "", "You are signed in as bob@example.com, a viewer; this needs an admin")
	run(t, s, apitest.ServerIP, "devices").want(t, cli.ExitDenied, "", "tagged in Tailscale")
	run(t, s, apitest.TabletIP, "devices").want(t, cli.ExitDenied, "", "have no role in Bield")
	run(t, s, apitest.StrangerIP, "devices").want(t, cli.ExitDenied, "", "Tailscale doesn't know the machine")
	run(t, s, apitest.LaptopIP, ack...).want(t, 0, "Acknowledged alert #"+itoa(id), "")

	// With --json the error is JSON too.
	r := run(t, s, apitest.PhoneIP, append(ack, "--json")...)
	var e struct {
		Error  string `json:"error"`
		Hint   string `json:"hint"`
		Status int    `json:"status"`
		Code   int    `json:"code"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil {
		t.Fatalf("stderr = %q, want JSON: %v", r.stderr, err)
	}
	if e.Status != 403 || e.Code != cli.ExitDenied || !strings.Contains(e.Hint, "a viewer") || r.stdout != "" {
		t.Errorf("error = %+v with stdout %q, want a 403 with exit code 4, a hint, and nothing on stdout", e, r.stdout)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestRun_devices(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	var all []api.Device
	r := run(t, s, apitest.PhoneIP, "devices", "--json")
	r.want(t, 0, "", "")
	if err := json.Unmarshal([]byte(r.stdout), &all); err != nil || len(all) != 5 {
		t.Fatalf("devices --json = %q, %v, want 5 devices", r.stdout, err)
	}
	for _, d := range all {
		if d.SSH != nil {
			t.Errorf("a viewer got the SSH details of %s", d.Name)
		}
	}
	// Names work as well as IDs, in any case, and a prefix that is one device's.
	for _, q := range []string{"alice-laptop", "ALICE-LAPTOP", "laptop", "alice-laptop.example.ts.net"} {
		r := run(t, s, apitest.PhoneIP, "devices", "show", q, "--json")
		var d api.Device
		if err := json.Unmarshal([]byte(r.stdout), &d); err != nil || d.ID != "laptop" {
			t.Errorf("devices show %q = %q, %v, want the laptop", q, r.stdout, err)
		}
	}
	run(t, s, apitest.PhoneIP, "devices", "show", "nope").want(t, cli.ExitNotFound, "", `no device is called "nope"`)
	// "b" starts "bield" and "bob-phone".
	run(t, s, apitest.PhoneIP, "devices", "show", "b").want(t, cli.ExitUsage, "", "matches several devices")
	run(t, s, apitest.PhoneIP, "devices", "--status", "offline").want(t, 0, "No devices match.", "")
	r = run(t, s, apitest.PhoneIP, "devices")
	r.want(t, 0, "NAME", "")
	for _, name := range []string{"alice-laptop", "bob-phone", "web-1", "carol-tablet"} {
		if !strings.Contains(r.stdout, name) {
			t.Errorf("devices = %q, want it to list %s", r.stdout, name)
		}
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Errorf("devices printed color codes though stdout isn't a terminal: %q", r.stdout)
	}
	run(t, s, apitest.PhoneIP, "status").want(t, 0, "5: 5 online, 0 need attention, 0 offline", "")
}

func TestRun_alertsAndEvents(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	n := itoa(id)

	run(t, s, apitest.LaptopIP, "alerts", "note", n, "Looking", "at", "it").want(t, 0, "Wrote a note on alert #"+n, "")
	run(t, s, apitest.LaptopIP, "alerts", "snooze", n, "--for", "2h").want(t, 0, "Snoozed (2h) alert #"+n, "")
	run(t, s, apitest.LaptopIP, "alerts", "snooze", n, "--for", "31d").want(t, cli.ExitUsage, "", "--for is from 1 minute to 30 days")
	run(t, s, apitest.LaptopIP, "alerts", "wake", n).want(t, 0, "Woke alert", "")
	run(t, s, apitest.LaptopIP, "alerts", "ack", n).want(t, 0, "Acknowledged", "")
	run(t, s, apitest.LaptopIP, "alerts", "ack", "x").want(t, cli.ExitUsage, "", `"x" isn't an alert ID`)
	run(t, s, apitest.LaptopIP, "alerts", "ack", "999").want(t, cli.ExitNotFound, "", "")
	run(t, s, apitest.PhoneIP, "alerts", "show", n).want(t, 0, "Looking at it", "")

	// The activity log says who did it, and that it was the CLI. A viewer sees
	// the line but not the login.
	r := run(t, s, apitest.LaptopIP, "events", "--kind", "alert", "--limit", "10")
	r.want(t, 0, "by alice@example.com with the CLI", "")
	r = run(t, s, apitest.PhoneIP, "events", "--kind", "alert", "--limit", "10")
	if r.code != 0 || strings.Contains(r.stdout, "alice@example.com") || !strings.Contains(r.stdout, "Acknowledged") {
		t.Errorf("a viewer's events = %q (exit %d), want the lines without logins", r.stdout, r.code)
	}
}

// A command that stops or deletes does nothing without --yes, and never asks.
func TestRun_destructive(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	run(t, s, apitest.LaptopIP, "checks", "add", "--name", "Blog", "--kind", "tcp", "--target", "blog.example.com:443").
		want(t, 0, "Added the check Blog", "")
	r := run(t, s, apitest.LaptopIP, "checks", "delete", "Blog")
	r.want(t, cli.ExitUsage, "", "add --yes if you mean it")
	run(t, s, apitest.LaptopIP, "checks", "show", "Blog").want(t, 0, "Blog", "")
	run(t, s, apitest.LaptopIP, "checks", "pause", "Blog").want(t, 0, "Paused the check Blog", "")
	run(t, s, apitest.LaptopIP, "checks", "resume", "blog").want(t, 0, "Resumed the check Blog", "")
	run(t, s, apitest.LaptopIP, "checks", "delete", "Blog", "--yes").want(t, 0, "Deleted the check Blog", "")
	run(t, s, apitest.LaptopIP, "checks", "show", "Blog").want(t, cli.ExitNotFound, "", `no check is called "Blog"`)
	run(t, s, apitest.LaptopIP, "checks", "add", "--name", "x").want(t, cli.ExitUsage, "", "--kind is required")
	run(t, s, apitest.LaptopIP, "checks", "add", "--name", "x", "--kind", "ftp", "--target", "y").want(t, cli.ExitUsage, "", "--kind is http, tcp or dns")
}

func TestRun_settingsAndAccess(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	run(t, s, apitest.LaptopIP, "settings", "set", "thresholds.diskWarn=85", "rules.cpu=false", "general.name=Home lab").
		want(t, 0, "Changed 3 settings", "")
	r := run(t, s, apitest.LaptopIP, "settings", "--json")
	var view api.SettingsView
	if err := json.Unmarshal([]byte(r.stdout), &view); err != nil {
		t.Fatal(err)
	}
	if got := view.Settings; got.Thresholds.DiskWarn != 85 || got.Rules.CPU || got.General.Name != "Home lab" {
		t.Errorf("settings = %+v, want disk 85, CPU rule off and the name Home lab", got)
	}
	// A name that is all digits is still text.
	run(t, s, apitest.LaptopIP, "settings", "set", "general.name=1234").want(t, 0, "Changed 1 setting", "")
	run(t, s, apitest.LaptopIP, "settings", "set", "thresholds.diskWrn=85").want(t, cli.ExitUsage, "", "there is no setting called that: diskWrn")
	run(t, s, apitest.LaptopIP, "settings", "set", "thresholds.diskWarn=lots").want(t, cli.ExitUsage, "", "can't be")
	run(t, s, apitest.LaptopIP, "settings", "set", "nonsense").want(t, cli.ExitUsage, "", "isn't name=value")
	run(t, s, apitest.PhoneIP, "settings").want(t, cli.ExitDenied, "", "a viewer")
	run(t, s, apitest.LaptopIP, "settings").want(t, 0, "general.name", "")

	run(t, s, apitest.LaptopIP, "access", "set", apitest.Carol, "viewer").want(t, cli.ExitUsage, "", "add --yes")
	run(t, s, apitest.LaptopIP, "access", "set", apitest.Carol, "viewer", "--yes").want(t, 0, "carol@example.com is now viewer", "")
	run(t, s, apitest.TabletIP, "whoami").want(t, 0, "with the role viewer", "")
	run(t, s, apitest.LaptopIP, "access", "set", apitest.Carol, "owner", "--yes").want(t, cli.ExitUsage, "", "a role is admin or viewer")
	run(t, s, apitest.LaptopIP, "access", "remove", apitest.Carol, "--yes").want(t, 0, "no longer has a role", "")
	run(t, s, apitest.LaptopIP, "access", "remove", apitest.Alice, "--yes").want(t, cli.ExitFailed, "", "")
	run(t, s, apitest.LaptopIP, "access", "who-can-view", "tailnet").want(t, cli.ExitUsage, "", "needs --yes")
	run(t, s, apitest.LaptopIP, "access", "who-can-view", "tailnet", "--yes").want(t, 0, "tailnet", "")
	run(t, s, apitest.LaptopIP, "access", "who-can-view").want(t, 0, "tailnet", "")
	run(t, s, apitest.TabletIP, "whoami").want(t, 0, "with the role viewer", "")

	r = run(t, s, apitest.LaptopIP, "enroll")
	r.want(t, 0, "/api/agent/install/", "works on one machine")
	if strings.Contains(r.stdout, "works on one machine") {
		t.Errorf("enroll printed its note on stdout: %q", r.stdout)
	}
}

func TestRun_connect(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	e, stdout, stderr := env(t, s, apitest.LaptopIP)
	e.Getenv = func(string) string { return "" }

	if code := cli.Run(t.Context(), e, []string{"devices"}); code != cli.ExitFailed || !strings.Contains(stderr.String(), "no Bield address") {
		t.Errorf("devices with no address = %d %q, want it to ask for one", code, stderr)
	}
	stderr.b.Reset()
	if code := cli.Run(t.Context(), e, []string{"connect", "http://bield.example.ts.net"}); code != 0 {
		t.Fatalf("connect = %d %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Connected to Bield v0.0.0-test at http://bield.example.ts.net") {
		t.Errorf("connect printed %q", stdout)
	}
	saved, err := os.ReadFile(filepath.Join(e.ConfigDir, "cli.json"))
	if err != nil || !strings.Contains(string(saved), "http://bield.example.ts.net") {
		t.Errorf("saved config = %q, %v", saved, err)
	}
	if fi, err := os.Stat(filepath.Join(e.ConfigDir, "cli.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config file mode = %v, %v, want 0600", fi, err)
	}
	// The saved address is used from now on, and --url beats it.
	stdout.b.Reset()
	e.HTTP = s.Client(apitest.LaptopIP)
	if code := cli.Run(t.Context(), e, []string{"whoami"}); code != 0 {
		t.Fatalf("whoami after connect = %d %s", code, stderr)
	}
	stdout.b.Reset()
	if code := cli.Run(t.Context(), e, []string{"connect"}); code != 0 || !strings.Contains(stdout.String(), "http://bield.example.ts.net (from bield connect)") {
		t.Errorf("connect with no address = %d %q", code, stdout)
	}
	// BIELD_URL beats the saved address, and --url beats both.
	e.Getenv = func(k string) string { return map[string]string{"BIELD_URL": "http://127.0.0.1:1"}[k] }
	e.HTTP = nil
	stderr.b.Reset()
	if code := cli.Run(t.Context(), e, []string{"whoami"}); code != cli.ExitFailed || !strings.Contains(stderr.String(), "can't reach Bield at http://127.0.0.1:1") {
		t.Errorf("whoami with BIELD_URL nowhere = %d %q, want a message that Bield can't be reached", code, stderr)
	}
	e.HTTP = s.Client(apitest.LaptopIP)
	stdout.b.Reset()
	if code := cli.Run(t.Context(), e, []string{"whoami", "--url", apitest.URL}); code != 0 || !strings.Contains(stdout.String(), "alice@example.com") {
		t.Errorf("whoami --url = %d %q %q, want alice", code, stdout, stderr)
	}
}

func TestRun_usage(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	run(t, s, apitest.LaptopIP, "frobnicate").want(t, cli.ExitUsage, "", `unknown command "frobnicate"`)
	run(t, s, apitest.LaptopIP, "alerts", "ack").want(t, cli.ExitUsage, "", "alerts ack needs 1 argument")
	run(t, s, apitest.LaptopIP, "alerts", "show", "1", "2").want(t, cli.ExitUsage, "", "takes at most 1 argument")
	run(t, s, apitest.LaptopIP, "devices", "--bogus").want(t, cli.ExitUsage, "", "unknown flag --bogus")
	run(t, s, apitest.LaptopIP, "events", "--limit", "many").want(t, cli.ExitUsage, "", "--limit takes a number")
	run(t, s, apitest.LaptopIP, "devices", "--timeout", "soon").want(t, cli.ExitUsage, "", "--timeout takes a time")
	run(t, s, apitest.LaptopIP, "alerts", "ack", "-h").want(t, 0, "Usage: bield alerts ack <id>", "")
	run(t, s, apitest.LaptopIP, "docker", "stop", "-h").want(t, 0, "Needs an admin, and --yes.", "")
	r := run(t, s, apitest.LaptopIP, "devices", "history", "laptop", "--range", "2d")
	r.want(t, cli.ExitUsage, "", "--range is 1h, 6h, 24h or 7d")
	if !strings.Contains(r.stderr, "Usage: bield devices history <device> [--range <range>]") {
		t.Errorf("usage error = %q, want the usage line", r.stderr)
	}
	// Flags may stand before, among or after the arguments, and "--" ends them.
	run(t, s, apitest.LaptopIP, "--json", "devices", "show", "laptop").want(t, 0, `"id": "laptop"`, "")
	run(t, s, apitest.LaptopIP, "devices", "show", "--json", "laptop").want(t, 0, `"id": "laptop"`, "")
	run(t, s, apitest.LaptopIP, "devices", "show", "laptop", "--json").want(t, 0, `"id": "laptop"`, "")
	run(t, s, apitest.LaptopIP, "devices", "show", "--", "-laptop").want(t, cli.ExitNotFound, "", `no device is called "-laptop"`)
}

// Nothing waits for input: a command that needs a thing it doesn't have says so.
func TestRun_neverAsks(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	e, _, stderr := env(t, s, apitest.LaptopIP)
	e.Stdin = blockingReader{}
	done := make(chan int, 1)
	go func() { done <- cli.Run(t.Context(), e, []string{"files", "put", "laptop", "-"}) }()
	select {
	case code := <-done:
		if code != cli.ExitUsage || !strings.Contains(stderr.String(), "needs --name") {
			t.Errorf("files put - = %d %q, want it to ask for --name without reading stdin", code, stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a command waited for input")
	}
}

type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }

func TestRun_followAlerts(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	first := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	e, stdout, stderr := env(t, s, apitest.PhoneIP)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- cli.Run(ctx, e, []string{"alerts", "--follow", "--json"}) }()

	waitFor(t, stdout, `"event":"open"`)
	second := openAlert(t, s, "mem:bield", "Memory 95% used on bield")
	waitFor(t, stdout, `"event":"opened"`)
	// An alert resolves once its condition has been gone for a while.
	fresh := func(string, string, string) bool { return true }
	s.Alerts.Apply(t.Context(), nil, fresh, time.Now())
	s.Alerts.Apply(t.Context(), nil, fresh, time.Now().Add(alerts.ResolveAfter+time.Second))
	s.Feed.Poke()
	waitFor(t, stdout, `"event":"resolved"`)
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("alerts --follow = %d %q after Ctrl-C, want 0", code, stderr)
	}
	var events []struct {
		Event string    `json:"event"`
		Alert api.Alert `json:"alert"`
	}
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		var ev struct {
			Event string    `json:"event"`
			Alert api.Alert `json:"alert"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q isn't JSON: %v", line, err)
		}
		events = append(events, ev)
	}
	if len(events) < 3 || events[0].Alert.ID != first || events[1].Event != "opened" || events[1].Alert.ID != second {
		t.Errorf("events = %+v, want the first alert open, then the second opened, then resolved", events)
	}
}

func TestRun_followEvents(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	e, stdout, _ := env(t, s, apitest.LaptopIP)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- cli.Run(ctx, e, []string{"events", "--follow", "--limit", "1"}) }()
	time.Sleep(300 * time.Millisecond)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	if r := run(t, s, apitest.LaptopIP, "alerts", "ack", itoa(id)); r.code != 0 {
		t.Fatal(r.stderr)
	}
	waitFor(t, stdout, "Acknowledged: Disk 90% full on bield")
	cancel()
	<-done
}

// waitFor waits until the output has text in it.
func waitFor(t *testing.T, out *syncBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(out.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("output = %q, want it to get %q", out, text)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCompletion(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		r := run(t, s, apitest.LaptopIP, "completion", shell)
		r.want(t, 0, "bield", "")
		flags := []string{"--follow", "--json"}
		if shell == "fish" {
			flags = []string{"-l follow", "-l json"}
		}
		for _, want := range append([]string{"alerts", "docker"}, flags...) {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%s completion lacks %q", shell, want)
			}
		}
		if shell == "bash" || shell == "zsh" {
			sh, err := exec.LookPath(shell)
			if err != nil {
				continue
			}
			cmd := exec.CommandContext(t.Context(), sh, "-n") //nolint:gosec // G204: sh is bash or zsh, found on the PATH.
			cmd.Stdin = strings.NewReader(r.stdout)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s -n on the %s completion: %v\n%s", shell, shell, err, out)
			}
		}
	}
	run(t, s, apitest.LaptopIP, "completion", "tcsh").want(t, cli.ExitUsage, "", "the shells are bash, zsh and fish")
}

// TestCompletion_inShells completes words in the shells that are installed,
// as a person pressing Tab would get them. zsh uses the bash script through
// bashcompinit, and needs a terminal to press Tab in, so it's checked by hand
// (docs/cli.md says how it was).
func TestCompletion_inShells(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	cases := []struct {
		line string // what's typed, up to the cursor
		want []string
	}{
		{"bield tok", []string{"tokens"}},
		{"bield tokens ", []string{"create", "revoke"}},
		{"bield tokens create --", []string{"--role", "--days", "--json"}},
		{"bield connect --w", []string{"--with-token"}},
		{"bield docker logs --f", []string{"--follow"}},
	}
	complete := map[string]func(script, line string) string{
		// bash: call the function as complete -F would, with the words so far.
		"bash": func(script, line string) string {
			words := strings.Fields(line)
			if strings.HasSuffix(line, " ") {
				words = append(words, "")
			}
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = "'" + w + "'"
			}
			return script + "\nCOMP_WORDS=(" + strings.Join(quoted, " ") + "); COMP_CWORD=" + strconv.Itoa(len(words)-1) +
				"; _bield; printf '%s\\n' \"${COMPREPLY[@]}\"\n"
		},
		// fish: complete -C is what fish runs on Tab.
		"fish": func(script, line string) string {
			return script + "\ncomplete -C '" + line + "'\n"
		},
	}
	for shell, input := range complete {
		sh, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("no %s here, so its completion isn't run", shell)
			continue
		}
		script := run(t, s, apitest.LaptopIP, "completion", shell).stdout
		for _, tc := range cases {
			cmd := exec.CommandContext(t.Context(), sh) //nolint:gosec // G204: sh is bash or fish, found on the PATH.
			cmd.Stdin = strings.NewReader(input(script, tc.line))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("%s completing %q: %v\n%s", shell, tc.line, err, out)
				continue
			}
			got := map[string]bool{}
			for l := range strings.Lines(string(out)) {
				word, _, _ := strings.Cut(strings.TrimSpace(l), "\t")
				got[word] = true
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Errorf("%s completing %q = %q, want %q among them", shell, tc.line, out, w)
				}
			}
		}
	}
}

func TestHas(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{"alerts": true, "docker": true, "completion": true, "serve": false, "admin": false, "version": false, "": false} {
		if got := cli.Has(name); got != want {
			t.Errorf("Has(%q) = %v, want %v", name, got, want)
		}
	}
}
