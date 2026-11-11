package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/api/apitest"
	"github.com/manasseh-fikadu/bield/internal/cli"
)

// makeToken makes an API token as alice and returns it.
func makeToken(t *testing.T, s *apitest.Server, args ...string) string {
	t.Helper()
	r := run(t, s, apitest.LaptopIP, append([]string{"tokens", "create"}, args...)...)
	r.want(t, 0, "bield_", "This is the only time Bield shows it")
	return strings.TrimSpace(r.stdout)
}

// runWithToken runs bield with args from the tagged server, with BIELD_TOKEN.
func runWithToken(t *testing.T, s *apitest.Server, token string, args ...string) result {
	t.Helper()
	e, stdout, stderr := env(t, s, apitest.ServerIP)
	e.Getenv = func(k string) string {
		return map[string]string{"BIELD_URL": apitest.URL, "BIELD_TOKEN": token}[k]
	}
	code := cli.Run(t.Context(), e, args)
	return result{stdout.String(), stderr.String(), code}
}

func TestRun_tokens(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)

	run(t, s, apitest.LaptopIP, "tokens").want(t, 0, "No API tokens", "")
	run(t, s, apitest.LaptopIP, "tokens", "create", "ci", "--role", "owner").want(t, cli.ExitUsage, "", "--role is admin or viewer")
	run(t, s, apitest.LaptopIP, "tokens", "create", "ci", "--days", "400").want(t, cli.ExitUsage, "", "--days is 1 to 365")
	run(t, s, apitest.PhoneIP, "tokens", "create", "ci").want(t, cli.ExitDenied, "", "a viewer")
	viewer := makeToken(t, s, "ci", "--days", "7")
	if strings.Count(viewer, "\n") != 0 || !strings.HasPrefix(viewer, "bield_") {
		t.Errorf("tokens create printed %q, want the token alone", viewer)
	}
	run(t, s, apitest.LaptopIP, "tokens", "create", "CI").want(t, cli.ExitFailed, "", "another API token is called CI")

	// The tagged server is someone now, with the token's role.
	runWithToken(t, s, viewer, "whoami").want(t, 0,
		"You are using the API token ci from web-1.example.ts.net, made by alice@example.com, with the role viewer", "")
	runWithToken(t, s, viewer, "devices", "--json").want(t, 0, `"name"`, "")
	id := openAlert(t, s, "disk", "Disk is full")
	runWithToken(t, s, viewer, "alerts", "ack", jsonNum(id)).want(t, cli.ExitDenied, "", "the API token ci is a viewer token; this needs an admin token")
	// Tokens are for people to manage.
	r := runWithToken(t, s, viewer, "tokens")
	r.want(t, cli.ExitDenied, "", "only a person can do this")
	if strings.Contains(r.stderr, "viewer token") {
		t.Errorf("tokens with a token = %q, want no hint about its role", r.stderr)
	}

	admin := makeToken(t, s, "deploy", "--role", "admin")
	runWithToken(t, s, admin, "alerts", "ack", jsonNum(id)).want(t, 0, "", "")
	var events api.EventList
	r = run(t, s, apitest.LaptopIP, "events", "--json")
	if err := json.Unmarshal([]byte(r.stdout), &events); err != nil {
		t.Fatalf("events --json = %q: %v", r.stdout, err)
	}
	if e := events.Events[0]; e.Actor != apitest.Alice || e.Token != "deploy" || e.Via != api.ViaCLI {
		t.Errorf("the ack's activity line = %+v, want alice's, with the token deploy, via the CLI", e)
	}

	r = run(t, s, apitest.LaptopIP, "tokens")
	r.want(t, 0, "deploy", "")
	for _, want := range []string{"ci", "viewer", "admin", "alice@example.com", "from web-1"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("tokens = %q, want it to have %q", r.stdout, want)
		}
	}
	if strings.Contains(r.stdout, viewer) || strings.Contains(r.stdout, admin) {
		t.Errorf("tokens = %q, which has a token in it", r.stdout)
	}

	run(t, s, apitest.LaptopIP, "tokens", "revoke", "ci").want(t, cli.ExitUsage, "", "--yes")
	run(t, s, apitest.LaptopIP, "tokens", "revoke", "nope", "--yes").want(t, cli.ExitNotFound, "", "no API token is called nope")
	run(t, s, apitest.LaptopIP, "tokens", "revoke", "CI", "--yes").want(t, 0, "Revoked the API token ci", "")
	runWithToken(t, s, viewer, "whoami").want(t, cli.ExitDenied, "", "Bield doesn't know this API token")

	runWithToken(t, s, "not-a-token", "whoami").want(t, cli.ExitFailed, "", "that isn't a Bield API token")
}

func jsonNum(n int64) string { return strconv.FormatInt(n, 10) }

// bield connect --with-token saves the token with the address, and sends it
// to no other.
func TestRun_connectWithToken(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	token := makeToken(t, s, "server")

	e, stdout, stderr := env(t, s, apitest.ServerIP)
	e.Getenv = func(string) string { return "" }
	connect := func(stdin string, args ...string) int {
		t.Helper()
		stdout.b.Reset()
		stderr.b.Reset()
		e.Stdin = strings.NewReader(stdin)
		return cli.Run(t.Context(), e, append([]string{"connect"}, args...))
	}
	path := filepath.Join(e.ConfigDir, "cli.json")

	if code := connect("", apitest.URL, "--with-token"); code != cli.ExitUsage || !strings.Contains(stderr.String(), "it was empty") {
		t.Errorf("connect --with-token with nothing on stdin = %d %q", code, stderr)
	}
	if code := connect("bield_WRONGWRONGWRONGWRONGWRONGW\n", apitest.URL, "--with-token"); code != cli.ExitDenied ||
		!strings.Contains(stderr.String(), "Bield doesn't know this API token") {
		t.Errorf("connect --with-token with a token Bield doesn't know = %d %q", code, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a token Bield refused was saved: %v", err)
	}

	// A config that was readable by others becomes the owner's alone.
	if err := os.WriteFile(path, []byte(`{"url": "http://elsewhere"}`), 0o644); err != nil { //nolint:gosec // G306: on purpose.
		t.Fatal(err)
	}
	if code := connect(token+"\n", apitest.URL, "--with-token"); code != 0 {
		t.Fatalf("connect --with-token = %d %s", code, stderr)
	}
	if !strings.Contains(stdout.String(), "You are using the API token server") {
		t.Errorf("connect --with-token printed %q", stdout)
	}
	saved, err := os.ReadFile(path) //nolint:gosec // G304: the test's own config.
	if err != nil || !strings.Contains(string(saved), token) {
		t.Errorf("saved config = %q, %v; want the token in it", saved, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config file mode = %v, %v, want 0600", fi, err)
	}

	run := func(args ...string) int {
		t.Helper()
		stdout.b.Reset()
		stderr.b.Reset()
		return cli.Run(t.Context(), e, args)
	}
	if code := run("whoami"); code != 0 || !strings.Contains(stdout.String(), "API token server") {
		t.Errorf("whoami after connect --with-token = %d %q %q", code, stdout, stderr)
	}
	// Connecting again to the same Bield keeps the token.
	if code := connect("", apitest.URL); code != 0 || !strings.Contains(stdout.String(), "API token server") {
		t.Errorf("connect again = %d %q %q, want the token kept", code, stdout, stderr)
	}
	// Another address doesn't get it.
	var sent []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"signedIn": false}`))
	}))
	t.Cleanup(elsewhere.Close)
	e.HTTP = nil
	run("whoami", "--url", elsewhere.URL)
	if len(sent) != 1 || sent[0] != "" {
		t.Errorf("whoami --url elsewhere sent Authorization %q, want none", sent)
	}
	// BIELD_TOKEN goes wherever the command does, since whoever set it chose.
	e.Getenv = func(k string) string { return map[string]string{"BIELD_TOKEN": token}[k] }
	run("whoami", "--url", elsewhere.URL)
	if len(sent) != 2 || sent[1] != "Bearer "+token {
		t.Errorf("whoami --url elsewhere with BIELD_TOKEN sent Authorization %q, want the token", sent)
	}
}
