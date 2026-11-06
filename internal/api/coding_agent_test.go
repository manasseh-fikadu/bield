package api_test

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/coding"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
)

// TestCoding_realAgent runs the agent's own script on the made-up sessions in
// agent/testdata, against a hub that serves on loopback and takes 127.0.0.1 for
// alice's laptop, and checks that what the hub counts is what the agent's tests
// say the files hold. It's the one place where the agent and the hub meet over
// HTTP, with curl and the headers and the chunks of a real request.
func TestCoding_realAgent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the agent's tests run on Linux")
	}
	for _, tool := range []string{"bash", "curl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s to run the agent with", tool)
		}
	}
	// The sessions are from November 2026, and Bield keeps three years at most.
	if time.Now().After(time.Date(2029, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Skip("the made-up sessions are older than Bield keeps coding history")
	}
	t.Parallel()

	tn := tailnettest.New(tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "bield", DNSName: "localhost", IPs: []netip.Addr{bieldIP}, Owner: alice, Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, Owner: alice, Online: true},
		},
	})
	st := testStore(t)
	keep := settings.Defaults()
	keep.Retention.CodingDays = settings.MaxCodingDays
	if err := settings.Save(t.Context(), st, keep); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	opts := withStore(t, tn, st, api.Options{AppURL: "http://localhost:" + port})
	srv.Config.Handler = api.New(opts)
	srv.Start()
	t.Cleanup(srv.Close)

	secret := enrolled(t, st, "laptop")
	if err := st.SetCoding(t.Context(), "laptop", true); err != nil {
		t.Fatal(err)
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "agent", "agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	fixtures := filepath.Join("..", "..", "agent", "testdata", "sessions")
	runAgent := func(tool string) {
		t.Helper()
		home, err := filepath.Abs(filepath.Join(fixtures, tool, "home"))
		if err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // G204: the agent's own script, in this repository.
		cmd := exec.CommandContext(t.Context(), "bash", script, "--sessions-once")
		cmd.Env = append(os.Environ(),
			"HOME="+home, "SERVER_URL=http://localhost:"+port, "SECRET="+secret,
			"BIELD_AGENT_STATE="+filepath.Join(t.TempDir(), "state"), "BIELD_AGENT_CONF=/nonexistent",
			"SESSIONS_DAYS=36500",
		)
		cmd.WaitDelay = 20 * time.Second
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the agent on the %s sessions failed: %v\n%s", tool, err, out)
		}
	}

	var wantTokens, wantRows int64
	for _, tool := range []string{"claude", "codex", "pi"} {
		raw, err := os.ReadFile(filepath.Join(fixtures, tool+".golden.json")) //nolint:gosec // G304: a file in testdata.
		if err != nil {
			t.Fatal(err)
		}
		var want coding.Report
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		for _, u := range want.Usage {
			wantTokens += u.In + u.CR + u.CW + u.Out
			wantRows++
		}
		runAgent(tool)
	}

	from, to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	read := func() (tokens, rows int64) {
		t.Helper()
		got, err := st.CodingUsageBetween(t.Context(), "laptop", from, to)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range got {
			tokens += u.Input + u.CacheRead + u.CacheWrite + u.Output
		}
		return tokens, int64(len(got))
	}
	tokens, rows := read()
	if tokens != wantTokens || rows != wantRows || rows == 0 {
		t.Errorf("the hub counted %d tokens in %d rows, want %d in %d", tokens, rows, wantTokens, wantRows)
	}

	// Reading the same files again, with the state the agent kept, changes
	// nothing, and neither does starting over without it.
	runAgent("claude")
	if t2, r2 := read(); t2 != tokens || r2 != rows {
		t.Errorf("after reading the claude sessions again: %d tokens in %d rows, want %d in %d", t2, r2, tokens, rows)
	}

	tools, _ := st.CodingTools(t.Context(), "laptop")
	seen := map[string]string{}
	for _, tl := range tools {
		seen[tl.Tool] = tl.Status
	}
	for _, tool := range []string{"claude", "codex", "pi"} {
		if seen[tool] != "ok" {
			t.Errorf("tool %s status = %q, want ok (all: %v)", tool, seen[tool], seen)
		}
	}
	if limits, _ := st.CodingLimits(t.Context(), "laptop"); len(limits) == 0 {
		t.Error("the hub has none of the limits the agent sent")
	}
	if sessions, _ := st.CodingSessionsSince(t.Context(), "laptop", from, 100); len(sessions) == 0 {
		t.Error("the hub has none of the sessions the agent sent")
	}
}
