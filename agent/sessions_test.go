// SPDX-License-Identifier: MIT

package agent_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files of the session tests")

// The shapes of what the agent sends, as far as the tests look at them.
type (
	toolRow struct {
		Tool   string `json:"tool"`
		Status string `json:"status"`
	}
	sessionRow struct {
		Tool    string `json:"tool"`
		ID      string `json:"id"`
		Project string `json:"project"`
		First   string `json:"first"`
		Last    string `json:"last"`
	}
	usageRow struct {
		Tool    string  `json:"tool"`
		Session string  `json:"session"`
		Model   string  `json:"model"`
		Speed   string  `json:"speed"`
		Hour    string  `json:"hour"`
		In      int64   `json:"in"`
		Cr      int64   `json:"cr"`
		Cw      int64   `json:"cw"`
		Out     int64   `json:"out"`
		Reason  int64   `json:"reason"`
		N       int64   `json:"n"`
		Cost    float64 `json:"cost"`
	}
	limitRow struct {
		Tool   string   `json:"tool"`
		ID     string   `json:"id"`
		Window int      `json:"window"`
		Used   *float64 `json:"used"`
		Resets int64    `json:"resets"`
		Status string   `json:"status"`
		Plan   string   `json:"plan"`
		Seen   string   `json:"seen"`
	}
	payload struct {
		V        int          `json:"v"`
		Tools    []toolRow    `json:"tools"`
		Sessions []sessionRow `json:"sessions"`
		Usage    []usageRow   `json:"usage"`
		Limits   []limitRow   `json:"limits"`
	}
)

// key says which row of usage a usage row is a total for.
func (u usageRow) key() string {
	return strings.Join([]string{u.Tool, u.Session, u.Model, u.Speed, u.Hour}, "|")
}

func parsePayload(t *testing.T, raw []byte) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("not a valid report: %v\n%.300s", err, raw)
	}
	return p
}

func usageByKey(p payload) map[string]usageRow {
	m := make(map[string]usageRow, len(p.Usage))
	for _, u := range p.Usage {
		m[u.key()] = u
	}
	return m
}

// requireShell skips the test when the machine lacks what the agent needs for sessions.
func requireShell(t *testing.T) {
	t.Helper()
	for _, name := range []string{"bash", "awk", "grep", "find", "stat", "cksum", "od", "tail", "head", "sort", "curl"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("no %s on this machine", name)
		}
	}
}

// agentEnv is what agent.sh runs with: HOME is home, its state is in state, and sessions of any
// age count, since the fixtures date from 2026.
func agentEnv(home, state string, extra ...string) []string {
	return append([]string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "BIELD_AGENT_CONF=/nonexistent",
		"SERVER_URL=https://bield.example.ts.net", "SECRET=not-a-real-secret",
		"BIELD_AGENT_STATE=" + state, "SESSIONS_DAYS=36500",
	}, extra...)
}

// runAgent runs agent.sh with arguments and returns what it printed.
func runAgent(t *testing.T, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{"agent.sh"}, args...)...) //nolint:gosec // G204: bash is the one on PATH.
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	return out.String(), errb.String(), err
}

// printReport runs --sessions-print, which must work, and parses what it prints.
func printReport(t *testing.T, env []string) (payload, string) {
	t.Helper()
	out, stderr, err := runAgent(t, env, "--sessions-print")
	if err != nil {
		t.Fatalf("agent.sh --sessions-print: %v\n%s\n%s", err, out, stderr)
	}
	return parsePayload(t, []byte(out)), out
}

// fixture copies a tree of made-up sessions from testdata, so a test can change it.
func fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", "sessions", name, "home")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(path) //nolint:gosec // G304: a fixture in testdata.
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600) //nolint:gosec // G703: the target is in a temp dir.
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: a file the test made.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// claudeLine is a line of a Claude Code transcript with one response in it.
func claudeLine(id, model, ts string, in, cc, cr, out int) string {
	return fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"message":{"model":%q,"id":%q,"type":"message","role":"assistant",`+
		`"content":[{"type":"text","text":"PROMPT-LEAK"}],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,`+
		`"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"service_tier":"standard","iterations":[],"speed":"standard"}},`+
		`"requestId":"req_%s","type":"assistant","uuid":"u-%s","timestamp":%q,"cwd":"/home/demo/bield","sessionId":%q}`+"\n",
		model, id, in, cc, cr, out, id, id, ts, claudeSession)
}

const claudeSession = "6f1c0e2a-0000-4000-8000-00000000aaaa"

func claudeFile(home string) string {
	return filepath.Join(home, ".claude", "projects", "-home-demo-bield", claudeSession+".jsonl")
}

// checkGolden compares a report with testdata/sessions/<name>.golden.json. go test -update
// writes it again; look at what changed before committing.
func checkGolden(t *testing.T, name string, raw string) {
	t.Helper()
	var got any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	pretty, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "sessions", name+".golden.json")
	if *update {
		if err := os.WriteFile(path, append(pretty, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	wantRaw, err := os.ReadFile(path) //nolint:gosec // G304: a golden file in testdata.
	if err != nil {
		t.Fatalf("%v (go test -update makes it)", err)
	}
	var want any
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the report differs from %s:\ngot:\n%s\nwant:\n%s", path, pretty, wantRaw)
	}
}

// What each tool's files say, worked out by hand from the fixtures. The numbers are in the
// golden files; the comments below say why.
func TestSessions_golden(t *testing.T) {
	requireShell(t)
	// claude: a response written on two lines counts once; a user line that holds a usage object,
	// a <synthetic> response and a text that quotes a usage object count for nothing; the sub-agent's
	// file adds to the session of its parent; the limit is the one the synthetic line records.
	// codex: a repeated event, a zero request and an event without usage count for nothing; the total
	// dropping from 3000 to 500 is not subtracted; priority is fast; the forked rollout leaves out the
	// history it copied (the first 2 events, in a burst) and keeps the request after the gap.
	// pi: two requests in the same hour and model make one row, and a summary has its own model.
	for _, name := range []string{"claude", "codex", "pi"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			home := fixture(t, name)
			_, raw := printReport(t, agentEnv(home, t.TempDir()))
			checkGolden(t, name, raw)
			for _, leak := range []string{"PROMPT-LEAK", `"text"`, `"content"`, "777777", "999999"} {
				if strings.Contains(raw, leak) {
					t.Errorf("the report has %s in it: %s", leak, raw)
				}
			}
		})
	}
}

// A second run with what the first one left, with nothing changed, says the same.
// A line added to a file changes only the rows it belongs to, and is read once.
func TestSessions_incremental(t *testing.T) {
	requireShell(t)
	home := fixture(t, "claude")
	state := t.TempDir()
	env := agentEnv(home, state)
	first, raw1 := printReport(t, env)
	_, raw2 := printReport(t, env)
	if raw1 != raw2 {
		t.Errorf("the second run differs from the first:\n%s\n%s", raw1, raw2)
	}

	file := claudeFile(home)
	appendTo(t, file, claudeLine("msg_06", "claude-sonnet-5-5", "2026-11-06T18:15:00.000Z", 10, 20, 30, 40))
	second, _ := printReport(t, env)
	changed := map[string]bool{}
	was := usageByKey(first)
	for key, u := range usageByKey(second) {
		if old, ok := was[key]; !ok || old != u {
			changed[key] = true
		}
	}
	const sonnet18 = "claude|" + claudeSession + "|claude-sonnet-5-5|standard|2026-11-06T18"
	if len(changed) != 1 || !changed[sonnet18] {
		t.Errorf("changed rows = %v, want only %s", changed, sonnet18)
	}
	got := usageByKey(second)[sonnet18]
	if want := (usageRow{Tool: "claude", Session: claudeSession, Model: "claude-sonnet-5-5", Speed: "standard", Hour: "2026-11-06T18", In: 14, Cr: 46, Cw: 28, Out: 72, N: 2}); got != want {
		t.Errorf("row = %+v, want %+v (the 4/16/8/32 of before plus 10/30/20/40)", got, want)
	}
	if last := second.Sessions[0].Last; last != "2026-11-06T18:15:00Z" {
		t.Errorf("the session's last time = %s, want 2026-11-06T18:15:00Z", last)
	}

	// A line that isn't finished is read when it is.
	line := claudeLine("msg_07", "claude-opus-5-5", "2026-11-06T19:00:00.000Z", 1, 2, 3, 4)
	appendTo(t, file, line[:len(line)/2])
	partial, _ := printReport(t, env)
	if !reflect.DeepEqual(usageByKey(partial), usageByKey(second)) {
		t.Error("half a line was counted")
	}
	appendTo(t, file, line[len(line)/2:])
	done, _ := printReport(t, env)
	row := usageByKey(done)["claude|"+claudeSession+"|claude-opus-5-5|standard|2026-11-06T19"]
	if row.In != 1 || row.Cr != 3 || row.Cw != 2 || row.Out != 4 || row.N != 1 {
		t.Errorf("the finished line = %+v, want 1/3/2/4 once", row)
	}
	if len(done.Usage) != len(second.Usage)+1 {
		t.Errorf("rows = %d, want %d", len(done.Usage), len(second.Usage)+1)
	}
	// And a run that finds nothing new doesn't read it again: the offset is the end of the file.
	dats, _ := filepath.Glob(filepath.Join(state, "sessions", "f", "*.dat"))
	for _, dat := range dats {
		b, err := os.ReadFile(dat) //nolint:gosec // G304: the agent's state in a temp dir.
		if err != nil {
			t.Fatal(err)
		}
		head, _, _ := strings.Cut(string(b), "\n")
		f := strings.Split(head, "\t")
		if len(f) == 5 && f[1] == file {
			if st, err := os.Stat(file); err != nil || strconv.FormatInt(st.Size(), 10) != f[2] {
				t.Errorf("offset = %s, want the size of the file", f[2])
			}
		}
	}
}

// A file that shrinks or gets other contents is read again, and what it said before is gone.
func TestSessions_rewrittenFile(t *testing.T) {
	requireShell(t)
	home := fixture(t, "claude")
	env := agentEnv(home, t.TempDir())
	if p, _ := printReport(t, env); len(p.Usage) != 5 {
		t.Fatalf("rows = %d, want 5", len(p.Usage))
	}
	file := claudeFile(home)
	short := claudeLine("msg_90", "claude-opus-5-5", "2026-11-06T17:00:00.000Z", 1, 1, 1, 1)
	if err := os.WriteFile(file, []byte(short), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ := printReport(t, env)
	// What is left is the new file and the sub-agent's.
	want := map[string]int64{
		"claude|" + claudeSession + "|claude-opus-5-5|standard|2026-11-06T17":  1,
		"claude|" + claudeSession + "|claude-haiku-5-5|standard|2026-11-06T17": 10,
	}
	got := map[string]int64{}
	for _, u := range p.Usage {
		got[u.key()] = u.In
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows after shrinking = %v, want %v", got, want)
	}
	// Longer than before, and different from the first byte.
	long := strings.Repeat(claudeLine("msg_91", "claude-sonnet-5-5", "2026-11-06T20:00:00.000Z", 2, 0, 0, 2), 3)
	long += claudeLine("msg_92", "claude-sonnet-5-5", "2026-11-06T20:30:00.000Z", 3, 0, 0, 3)
	if err := os.WriteFile(file, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ = printReport(t, env)
	row := usageByKey(p)["claude|"+claudeSession+"|claude-sonnet-5-5|standard|2026-11-06T20"]
	if row.In != 5 || row.N != 2 {
		t.Errorf("row of a rewritten longer file = %+v, want in 5 and 2 responses (three lines of one count once)", row)
	}
	if _, ok := usageByKey(p)["claude|"+claudeSession+"|claude-opus-5-5|standard|2026-11-06T17"]; ok {
		t.Error("what the file said before it was rewritten is still there")
	}
}

// Home directories are only read when they are listed.
func TestSessions_homes(t *testing.T) {
	requireShell(t)
	claudeHome, codexHome, other := fixture(t, "claude"), fixture(t, "codex"), t.TempDir()
	p, _ := printReport(t, agentEnv(other, t.TempDir(), "BIELD_SESSION_HOMES="+claudeHome+" "+codexHome))
	var tools []string
	for _, tr := range p.Tools {
		tools = append(tools, tr.Tool)
	}
	if !reflect.DeepEqual(tools, []string{"claude", "codex"}) {
		t.Errorf("tools = %v, want claude and codex from the listed homes", tools)
	}
	p, _ = printReport(t, agentEnv(claudeHome, t.TempDir(), "BIELD_SESSION_HOMES="+other))
	if len(p.Tools) != 0 || len(p.Usage) != 0 {
		t.Errorf("a home that is not listed was read: %+v", p)
	}
	p, _ = printReport(t, agentEnv(claudeHome, t.TempDir()))
	if len(p.Tools) != 1 || p.Tools[0].Tool != "claude" {
		t.Errorf("without BIELD_SESSION_HOMES the home of the user is read, got %+v", p.Tools)
	}
}

// fakeHub answers the agent's requests, and keeps what it is sent.
type fakeHub struct {
	mu      sync.Mutex
	status  int
	hb      func(w http.ResponseWriter)
	reports []payload
	headers []http.Header
}

// answer sets how the hub answers a report, and heartbeat how it answers a heartbeat.
func (h *fakeHub) answer(status int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
}

func (h *fakeHub) heartbeat(f func(w http.ResponseWriter)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hb = f
}

func newHub(t *testing.T) (*fakeHub, *httptest.Server) {
	t.Helper()
	h := &fakeHub{status: http.StatusNoContent}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/agent/heartbeat":
			_, _ = io.Copy(io.Discard, r.Body)
			if h.hb != nil {
				h.hb(w)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/agent/sessions":
			b, _ := io.ReadAll(r.Body)
			var p payload
			if json.Unmarshal(b, &p) == nil {
				h.reports = append(h.reports, p)
			}
			h.headers = append(h.headers, r.Header.Clone())
			if h.status == http.StatusNotFound {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(h.status)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return h, srv
}

func (h *fakeHub) posts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.reports)
}

func (h *fakeHub) last() payload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reports[len(h.reports)-1]
}

// The agent sends what the hub lacks, and only that: the first time everything, then nothing, then
// what a new line changed. What the hub did not take is sent again.
func TestSessions_once(t *testing.T) {
	requireShell(t)
	hub, srv := newHub(t)
	home := fixture(t, "claude")
	env := agentEnv(home, t.TempDir(), "SERVER_URL="+srv.URL)
	once := func() error {
		_, _, err := runAgent(t, env, "--sessions-once")
		return err
	}

	hub.answer(http.StatusInternalServerError)
	if err := once(); err == nil {
		t.Error("a refused report is not a success")
	}
	hub.answer(http.StatusNoContent)
	if err := once(); err != nil {
		t.Fatal(err)
	}
	if hub.posts() != 2 {
		t.Fatalf("posts = %d, want the refused one and the one that was taken", hub.posts())
	}
	all := hub.last()
	if len(all.Usage) != 5 || len(all.Sessions) != 1 || len(all.Tools) != 1 || len(all.Limits) != 1 {
		t.Errorf("after a refusal the report is not complete: %+v", all)
	}
	hub.mu.Lock()
	h := hub.headers[1]
	hub.mu.Unlock()
	if h.Get("Authorization") != "Bearer not-a-real-secret" || h.Get("X-Bield") != "agent" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}

	if err := once(); err != nil {
		t.Fatal(err)
	}
	if hub.posts() != 2 {
		t.Errorf("posts = %d after a run with nothing new, want 2", hub.posts())
	}

	appendTo(t, claudeFile(home), claudeLine("msg_06", "claude-sonnet-5-5", "2026-11-06T18:15:00.000Z", 10, 20, 30, 40))
	if err := once(); err != nil {
		t.Fatal(err)
	}
	if hub.posts() != 3 {
		t.Fatalf("posts = %d, want 3", hub.posts())
	}
	delta := hub.last()
	if len(delta.Usage) != 1 || delta.Usage[0].In != 14 || len(delta.Sessions) != 1 || len(delta.Tools) != 0 || len(delta.Limits) != 0 {
		t.Errorf("what a new line sends = %+v, want one row with its new total, its session, and no tools or limits", delta)
	}

	// An older hub has no such route, so the agent waits instead of asking every minute.
	hub.answer(http.StatusNotFound)
	appendTo(t, claudeFile(home), claudeLine("msg_08", "claude-sonnet-5-5", "2026-11-06T18:16:00.000Z", 1, 1, 1, 1))
	if err := once(); err == nil {
		t.Error("a hub with no route is not a success")
	}
	before := hub.posts()
	hub.answer(http.StatusNoContent)
	if err := once(); err != nil || hub.posts() != before {
		t.Errorf("within 10 minutes of a 404: err = %v, posts = %d, want no request (was %d)", err, hub.posts(), before)
	}
}

// A report is cut into requests of at most 400 rows, with the tools in the first.
func TestSessions_chunks(t *testing.T) {
	requireShell(t)
	hub, srv := newHub(t)
	home := fixture(t, "claude")
	var b strings.Builder
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range 900 {
		ts := start.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000Z")
		b.WriteString(claudeLine(fmt.Sprintf("big_%03d", i), "claude-opus-5-5", ts, 1, 0, 0, 1))
	}
	if err := os.WriteFile(claudeFile(home), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	env := agentEnv(home, t.TempDir(), "SERVER_URL="+srv.URL)
	if _, stderr, err := runAgent(t, env, "--sessions-once"); err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var sizes []int
	total, tools := 0, 0
	for _, r := range hub.reports {
		sizes = append(sizes, len(r.Usage))
		total += len(r.Usage)
		tools += len(r.Tools)
	}
	if total != 901 { // 900 hours, and the sub-agent's row
		t.Errorf("rows sent = %d (%v), want 901", total, sizes)
	}
	for i, n := range sizes {
		if n > 400 {
			t.Errorf("request %d has %d rows, want at most 400", i, n)
		}
	}
	if len(hub.reports) != 3 || tools != 1 || len(hub.reports[0].Tools) != 1 {
		t.Errorf("requests = %d (tools in them: %d), want 3 with the tools in the first", len(hub.reports), tools)
	}
}

// What the heartbeat's answer says about coding sessions starts them, and only then.
func TestHeartbeat_codingFlag(t *testing.T) {
	requireShell(t)
	if runtime.GOOS != "linux" {
		t.Skip("the heartbeat reads the machine's metrics from /proc, so it runs on Linux only")
	}
	for _, tt := range []struct {
		name   string
		reply  func(w http.ResponseWriter)
		starts bool
	}{
		{"off is a 204", func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }, false},
		{"on is a 200 with the flag", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"coding":{"enabled":true}}`)
		}, true},
		{"a 200 that says off", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"coding":{"enabled":false}}`)
		}, false},
		{"the machine of the hub gets a 409 that says on", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"Bield measures this machine itself","coding":{"enabled":true}}`)
		}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hub, srv := newHub(t)
			hub.heartbeat(tt.reply)
			hub.answer(http.StatusNoContent)
			home, state := fixture(t, "claude"), t.TempDir()
			cmd := exec.CommandContext(t.Context(), "bash", "agent.sh")
			cmd.Env = agentEnv(home, state, "SERVER_URL="+srv.URL, "INTERVAL=1", "SESSIONS_INTERVAL=1")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			if !tt.starts {
				deadline = time.Now().Add(6 * time.Second)
			}
			for time.Now().Before(deadline) && hub.posts() == 0 {
				time.Sleep(100 * time.Millisecond)
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			// A round that is running when the agent stops finishes by itself.
			for range 100 {
				if _, err := os.Stat(filepath.Join(state, "sessions", "lock")); err != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if got := hub.posts() > 0; got != tt.starts {
				t.Errorf("sessions sent = %v, want %v", got, tt.starts)
			}
		})
	}
}

// A refusal is printed as the message alone, with or without the coding key after it.
func TestHeartbeat_errorMessage(t *testing.T) {
	requireShell(t)
	if runtime.GOOS != "linux" {
		t.Skip("the heartbeat reads the machine's metrics from /proc, so it runs on Linux only")
	}
	for _, tt := range []struct{ body, want string }{
		{`{"error":"Bield runs on this machine, so it doesn't need the agent","coding":{"enabled":true}}`, "HTTP 409: Bield runs on this machine, so it doesn't need the agent\n"},
		{`{"error":"nope"}`, "HTTP 409: nope\n"},
	} {
		hub, srv := newHub(t)
		hub.heartbeat(func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, tt.body)
		})
		_, stderr, err := runAgent(t, agentEnv(t.TempDir(), t.TempDir(), "SERVER_URL="+srv.URL), "--once")
		if err == nil || !strings.HasSuffix(stderr, tt.want) {
			t.Errorf("--once with %s: err = %v, stderr = %q, want it to end in %q", tt.body, err, stderr, tt.want)
		}
	}
}

// pythonSQLite makes the OpenCode databases of a made-up home: one in the format of the current
// version (session_message and session_v2), one in the format of older ones (message and session).
const pythonSQLite = `
import calendar, json, sqlite3, sys
def ms(h, m=0, s=0): return calendar.timegm((2026, 11, 6, h, m, s)) * 1000
def tokens(i, o, r, cr, cw): return {"input": i, "output": o, "reasoning": r, "cache": {"read": cr, "write": cw}}
root = sys.argv[1]
c = sqlite3.connect(root + "/opencode.db")
c.executescript("""CREATE TABLE session_v2 (id TEXT PRIMARY KEY, directory TEXT);
CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT);""")
c.execute("INSERT INTO session_v2 VALUES ('ses_1', '/home/demo/bield')")
rows = [
    ("m1", "ses_1", "assistant", ms(17, 0, 5), {"model": {"id": "gpt-5.5", "providerID": "openai"}, "cost": 0, "tokens": tokens(100, 40, 10, 300, 5)}),
    ("m2", "ses_1", "assistant", ms(17, 30), {"model": {"id": "gpt-5.5", "providerID": "openai"}, "cost": 0.01, "tokens": tokens(50, 20, 0, 100, 0)}),
    ("m3", "ses_1", "assistant", ms(18, 0, 9), {"model": {"id": "kimi-k2", "providerID": "moonshot"}, "cost": 0, "tokens": tokens(7, 3, 1, 0, 0)}),
    ("m4", "ses_1", "assistant", ms(18, 5), {"model": {"id": "kimi-k2", "providerID": "moonshot"}, "cost": 0, "tokens": tokens(0, 0, 0, 0, 0)}),
    ("m5", "ses_1", "user", ms(17, 0, 1), {"content": [{"type": "text", "text": "PROMPT-LEAK-opencode"}]}),
]
for id, s, t, tc, d in rows:
    c.execute("INSERT INTO session_message VALUES (?, ?, ?, 0, ?, ?, ?)", (id, s, t, tc, tc + 1, json.dumps(d)))
c.commit()
c.close()
c = sqlite3.connect(root + "/opencode-old.db")
c.executescript("""CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT);
CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);""")
c.execute("INSERT INTO session VALUES ('ses_old', '/home/demo/old')")
c.execute("INSERT INTO message VALUES ('o1', 'ses_old', ?, ?)", (ms(19, 0, 0), json.dumps({"role": "assistant", "modelID": "claude-sonnet-5-5", "providerID": "anthropic", "cost": 0.5, "tokens": tokens(11, 12, 0, 13, 14)})))
c.execute("INSERT INTO message VALUES ('o2', 'ses_old', ?, ?)", (ms(19, 1, 0), json.dumps({"role": "user", "text": "PROMPT-LEAK-opencode-old"})))
c.commit()
`

// fakeSQLite3 is a sqlite3 command made of python3, which takes the options the agent gives it, so
// that the way the agent calls sqlite3 is tried on machines that have none.
const fakeSQLite3 = `#!/bin/sh
exec python3 -I -c '
import sqlite3, sys
a = sys.argv[1:]; sep = "\t"; db = None; sql = None; i = 0
while i < len(a):
    if a[i] in ("-cmd", "-separator"):
        if a[i] == "-separator": sep = a[i + 1]
        i += 2
    elif a[i].startswith("-"): i += 1
    elif db is None: db = a[i]; i += 1
    else: sql = a[i]; i += 1
c = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
for q in sql.split(";"):
    if q.strip():
        for r in c.execute(q): print(sep.join("" if v is None else str(v) for v in r))
' "$@"
`

func TestSessions_opencode(t *testing.T) {
	requireShell(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 to make the databases with")
	}
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "python3", "-I", "-c", pythonSQLite, root).CombinedOutput(); err != nil { //nolint:gosec // G204: python3 is the one on PATH.
		t.Skipf("python3 can't make the databases: %v\n%s", err, out)
	}
	want := map[string]usageRow{
		"opencode|ses_1|gpt-5.5|standard|2026-11-06T17":             {In: 150, Cr: 400, Cw: 5, Out: 70, Reason: 10, N: 2, Cost: 0.01},
		"opencode|ses_1|kimi-k2|standard|2026-11-06T18":             {In: 7, Cr: 0, Cw: 0, Out: 4, Reason: 1, N: 1},
		"opencode|ses_old|claude-sonnet-5-5|standard|2026-11-06T19": {In: 11, Cr: 13, Cw: 14, Out: 12, N: 1, Cost: 0.5},
	}
	check := func(t *testing.T, env []string) {
		t.Helper()
		p, raw := printReport(t, env)
		got := map[string]usageRow{}
		for _, u := range p.Usage {
			k := u.key()
			u.Tool, u.Session, u.Model, u.Speed, u.Hour = "", "", "", "", ""
			got[k] = u
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %+v\nwant %+v", got, want)
		}
		if len(p.Tools) != 1 || p.Tools[0] != (toolRow{"opencode", "ok"}) {
			t.Errorf("tools = %+v, want opencode ok", p.Tools)
		}
		projects := map[string]string{}
		for _, s := range p.Sessions {
			projects[s.ID] = s.Project
		}
		if !reflect.DeepEqual(projects, map[string]string{"ses_1": "bield", "ses_old": "old"}) {
			t.Errorf("sessions = %+v", p.Sessions)
		}
		if strings.Contains(raw, "PROMPT-LEAK") {
			t.Errorf("the report has a prompt in it: %s", raw)
		}
	}
	t.Run("python3", func(t *testing.T) {
		// No sqlite3 on PATH, or the one of this machine: whichever the agent finds, python3 is the
		// fallback, and the result is the same.
		check(t, agentEnv(home, t.TempDir()))
	})
	t.Run("the sqlite3 command", func(t *testing.T) {
		bin := t.TempDir()
		if err := os.WriteFile(filepath.Join(bin, "sqlite3"), []byte(fakeSQLite3), 0o700); err != nil { //nolint:gosec // G306: it has to run.
			t.Fatal(err)
		}
		check(t, agentEnv(home, t.TempDir(), "PATH="+bin+":"+os.Getenv("PATH")))
	})
	t.Run("neither", func(t *testing.T) {
		// A PATH with what the agent needs, and no python3 or sqlite3.
		bin := t.TempDir()
		for _, name := range []string{"awk", "cat", "cksum", "curl", "cut", "date", "find", "grep", "head", "mkdir", "mv", "od", "rm", "sed", "sort", "stat", "tail", "tr", "uname", "wc", "chmod", "nice", "getconf", "ls", "kill"} {
			if p, err := exec.LookPath(name); err == nil {
				if err := os.Symlink(p, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		p, _ := printReport(t, agentEnv(home, t.TempDir(), "PATH="+bin))
		if len(p.Tools) != 1 || p.Tools[0] != (toolRow{"opencode", "needs-sqlite"}) || len(p.Usage) != 0 {
			t.Errorf("without sqlite3 or python3: tools = %+v, rows = %d, want opencode needs-sqlite and no rows", p.Tools, len(p.Usage))
		}
	})
}
