// SPDX-License-Identifier: MIT

package agent_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// rows is what a report says, by the key of each row, without the key in the values.
func rows(p payload) map[string]usageRow {
	got := map[string]usageRow{}
	for _, u := range p.Usage {
		k := u.key()
		u.Tool, u.Session, u.Model, u.Speed, u.Hour = "", "", "", "", ""
		got[k] = u
	}
	return got
}

// basicTools is a directory with what the agent needs on its PATH, and no python3 or sqlite3.
func basicTools(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"awk", "cat", "cksum", "curl", "cut", "date", "find", "grep", "head", "mkdir", "mv", "od", "rm", "sed", "sort", "stat", "tail", "tr", "uname", "wc", "chmod", "nice", "getconf", "ls", "kill"} {
		if p, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(p, filepath.Join(bin, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return bin
}

// skipIfAgentFinds skips a test of a machine without the commands named, when they're in a
// directory that the agent adds to its PATH (as on Chimera Linux, whose /usr/sbin is /usr/bin).
func skipIfAgentFinds(t *testing.T, names ...string) {
	t.Helper()
	for _, dir := range []string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/sbin", "/sbin"} {
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				t.Skipf("the agent finds %s in %s, which it adds to its PATH", name, dir)
			}
		}
	}
}

func runPython(t *testing.T, script string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 to make the files with")
	}
	if out, err := exec.CommandContext(t.Context(), "python3", append([]string{"-I", "-c", script}, args...)...).CombinedOutput(); err != nil { //nolint:gosec // G204: python3 is the one on PATH.
		t.Skipf("python3 can't make the files: %v\n%s", err, out)
	}
}

// pythonOpenCodeJSON makes what OpenCode wrote before its database: a message to a file, written
// with indents, and a database that one of them, m2, was moved into.
const pythonOpenCodeJSON = `
import calendar, json, os, sqlite3, sys
def ms(h, m=0): return calendar.timegm((2026, 11, 6, h, m, 0)) * 1000
root = sys.argv[1]
def put(path, doc):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    open(path, "w").write(json.dumps(doc, indent=2))
put(root + "/storage/session/prj_1/ses_j.json", {"id": "ses_j", "projectID": "prj_1", "directory": "/home/demo/legacy", "title": "PROMPT-LEAK-title"})
tokens = lambda i, o, r, cr, cw: {"input": i, "output": o, "reasoning": r, "cache": {"read": cr, "write": cw}}
put(root + "/storage/message/ses_j/m1.json", {"id": "m1", "sessionID": "ses_j", "role": "assistant", "system": ["PROMPT-LEAK \"input\": 777777"],
    "modelID": "claude-sonnet-5-5", "providerID": "anthropic", "time": {"created": ms(17, 5), "completed": ms(17, 6)}, "cost": 0.25, "tokens": tokens(10, 20, 5, 30, 40)})
put(root + "/storage/message/ses_j/m2.json", {"id": "m2", "sessionID": "ses_j", "role": "assistant", "modelID": "claude-sonnet-5-5",
    "time": {"created": ms(17, 9)}, "cost": 0, "tokens": tokens(1000, 1000, 0, 1000, 0)})
put(root + "/storage/message/ses_j/m3.json", {"id": "m3", "sessionID": "ses_j", "role": "user", "time": {"created": ms(17, 0)}})
put(root + "/storage/message/ses_j/m4.json", {"id": "m4", "sessionID": "ses_j", "role": "assistant", "model": {"providerID": "openai", "modelID": "gpt-5.5"},
    "time": {"created": ms(18, 1)}, "cost": 0, "tokens": tokens(3, 4, 0, 0, 0)})
c = sqlite3.connect(root + "/opencode.db")
c.executescript("""CREATE TABLE session_v2 (id TEXT PRIMARY KEY, directory TEXT);
CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT);""")
c.execute("INSERT INTO session_v2 VALUES ('ses_j', '/home/demo/legacy')")
c.execute("INSERT INTO session_message VALUES ('m2', 'ses_j', 'assistant', 0, ?, ?, ?)", (ms(17, 9), ms(17, 9), json.dumps({"modelID": "claude-sonnet-5-5", "cost": 0, "tokens": tokens(1000, 1000, 0, 1000, 0)})))
c.commit()
`

// OpenCode's JSON files count, and a message that is in its database as well counts once.
func TestSessions_opencodeJSON(t *testing.T) {
	requireShell(t)
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "opencode")
	runPython(t, pythonOpenCodeJSON, root)
	want := map[string]usageRow{
		// m1 from its file, with reasoning in the output, and m2 from the database.
		"opencode|ses_j|claude-sonnet-5-5|standard|2026-11-06T17": {In: 1010, Cr: 1030, Cw: 40, Out: 1025, Reason: 5, N: 2, Cost: 0.25},
		"opencode|ses_j|gpt-5.5|standard|2026-11-06T18":           {In: 3, Out: 4, N: 1},
	}
	env := agentEnv(home, t.TempDir())
	for _, run := range []string{"first", "second"} {
		p, raw := printReport(t, env)
		if got := rows(p); !reflect.DeepEqual(got, want) {
			t.Errorf("%s run: rows = %+v\nwant %+v", run, got, want)
		}
		if len(p.Sessions) != 1 || p.Sessions[0].Project != "legacy" || p.Sessions[0].First != "2026-11-06T17:05:00Z" {
			t.Errorf("%s run: sessions = %+v", run, p.Sessions)
		}
		if len(p.Tools) != 1 || p.Tools[0] != (toolRow{"opencode", "ok"}) {
			t.Errorf("%s run: tools = %+v", run, p.Tools)
		}
		if strings.Contains(raw, "PROMPT-LEAK") || strings.Contains(raw, "777777") {
			t.Errorf("the report has a prompt in it: %s", raw)
		}
	}
}

// pythonAntigravity makes Antigravity conversations the way T3 Code's tests do: SQLite with the
// metadata of each request in protobuf, encoded by hand. The second argument gets a copy of the
// first conversation, as a backup would.
const pythonAntigravity = `
import calendar, os, shutil, sqlite3, sys
def varint(v):
    out = bytearray()
    while True:
        b = v & 127; v >>= 7
        if v: out.append(b | 128)
        else: out.append(b); return bytes(out)
def num(f, v): return varint(f * 8) + varint(v)
def raw(f, b): return varint(f * 8 + 2) + varint(len(b)) + b
def text(f, s): return raw(f, s.encode())
def secs(h, m=0): return calendar.timegm((2026, 11, 6, h, m, 0))
def usage(i, o, cw, cr, r, rid, model=0): return (num(1, model) if model else b"") + num(2, i) + num(3, o) + num(4, cw) + num(5, cr) + num(9, r) + text(11, rid)
a, b = sys.argv[1], sys.argv[2]
os.makedirs(a); os.makedirs(b)
c = sqlite3.connect(a + "/conv-1.db")
c.executescript("CREATE TABLE gen_metadata (idx INTEGER, data BLOB); CREATE TABLE steps (idx INTEGER, metadata BLOB); CREATE TABLE trajectory_metadata_blob (data BLOB);")
gen = raw(1, raw(4, usage(100, 40, 5, 20, 10, "response-1")) + text(19, "Gemini 3 Pro") + raw(9, raw(4, num(1, secs(17, 5)))))
c.execute("INSERT INTO gen_metadata VALUES (0, ?)", (gen,))
# The step that holds the same response, and a retry of its own.
step = raw(9, usage(100, 40, 5, 20, 10, "response-1")) + raw(8, num(1, secs(17, 5))) + raw(28, raw(2, usage(7, 3, 0, 0, 0, "retry-1")))
c.execute("INSERT INTO steps VALUES (0, ?)", (step,))
c.execute("INSERT INTO steps VALUES (1, NULL)")
# A request with only a model number, whose time is the conversation's.
c.execute("INSERT INTO steps VALUES (2, ?)", (raw(9, usage(50, 5, 0, 0, 0, "response-2", 1026)),))
c.execute("INSERT INTO trajectory_metadata_blob VALUES (?)", (raw(2, num(1, secs(18, 30))),))
c.commit(); c.close()
shutil.copy(a + "/conv-1.db", b + "/conv-1-copy.db")
open(a + "/broken.db", "w").write("not a database")
`

// Antigravity's conversations, read with python3: a response in a step and in its generation, or
// in a copy of the conversation, counts once. A database that can't be read leaves the rest counted.
func TestSessions_antigravity(t *testing.T) {
	requireShell(t)
	home := t.TempDir()
	ide := filepath.Join(home, ".gemini", "antigravity", "conversations")
	cli := filepath.Join(home, ".gemini", "antigravity-cli")
	runPython(t, pythonAntigravity, ide, cli)
	want := map[string]usageRow{
		"antigravity|conv-1|gemini-3-pro|standard|2026-11-06T17":    {In: 107, Cr: 20, Cw: 5, Out: 43, Reason: 10, N: 2},
		"antigravity|conv-1|claude-opus-4-6|standard|2026-11-06T18": {In: 50, Out: 5, N: 1},
	}
	p, raw := printReport(t, agentEnv(home, t.TempDir()))
	if got := rows(p); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v\nwant %+v", got, want)
	}
	if len(p.Tools) != 1 || p.Tools[0] != (toolRow{"antigravity", "unreadable"}) {
		t.Errorf("tools = %+v, want antigravity unreadable, for the broken file", p.Tools)
	}
	if len(p.Sessions) != 1 || p.Sessions[0].First != "2026-11-06T17:05:00Z" || p.Sessions[0].Last != "2026-11-06T18:30:00Z" {
		t.Errorf("sessions = %+v", p.Sessions)
	}
	if strings.Contains(raw, "response-1") {
		t.Errorf("the report has a response ID in it: %s", raw)
	}

	t.Run("without python3", func(t *testing.T) {
		skipIfAgentFinds(t, "python3")
		p, _ := printReport(t, agentEnv(home, t.TempDir(), "PATH="+basicTools(t)))
		if len(p.Tools) != 1 || p.Tools[0] != (toolRow{"antigravity", "needs-python"}) || len(p.Usage) != 0 {
			t.Errorf("tools = %+v, rows = %d, want antigravity needs-python and no rows", p.Tools, len(p.Usage))
		}
	})
}

// A reply that a forked sub-agent copies from its parent after the parent was read counts once,
// in the parent, on the next round as well.
func TestSessions_copiesAcrossRounds(t *testing.T) {
	requireShell(t)
	home := fixture(t, "claude")
	env := agentEnv(home, t.TempDir())
	first, _ := printReport(t, env)
	fork := filepath.Join(filepath.Dir(claudeFile(home)), claudeSession, "subagents", "agent-late.jsonl")
	// The parent's first reply, msg_01 of request req_01.
	copied := strings.Replace(claudeLine("msg_01", "claude-opus-5-5", "2026-11-06T17:01:02.000Z", 3, 100, 1000, 5), "req_msg_01", "req_01", 1)
	if err := os.WriteFile(fork, []byte(copied), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _ := printReport(t, env)
	if !reflect.DeepEqual(rows(first), rows(second)) {
		t.Errorf("the copy changed the report:\n%+v\n%+v", rows(first), rows(second))
	}
	// A new reply in the same file counts.
	appendTo(t, fork, claudeLine("77", "claude-opus-5-5", "2026-11-06T19:00:00.000Z", 1, 0, 0, 1))
	third, _ := printReport(t, env)
	if u, ok := rows(third)["claude|"+claudeSession+"|claude-opus-5-5|standard|2026-11-06T19"]; !ok || u.N != 1 {
		t.Errorf("the sub-agent's own reply: %+v", rows(third))
	}
}

// What an older agent left is read again from the start, once, since it doesn't say which replies
// each file counted.
func TestSessions_olderState(t *testing.T) {
	requireShell(t)
	home := fixture(t, "claude")
	state := t.TempDir()
	env := agentEnv(home, state)
	_, want := printReport(t, env)
	dats, err := filepath.Glob(filepath.Join(state, "sessions", "f", "*.dat"))
	if err != nil || len(dats) == 0 {
		t.Fatalf("no state was left: %v", err)
	}
	// Totals that are wrong, as a different reading of the same lines would leave.
	for _, d := range dats {
		b, err := os.ReadFile(d) //nolint:gosec // G304: a file the agent made in a temp dir.
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, []byte(strings.ReplaceAll(string(b), "\tclaude-", "\tstale-claude-")), 0o600); err != nil { //nolint:gosec // G703: a file the agent made in a temp dir.
			t.Fatal(err)
		}
	}
	version := filepath.Join(state, "sessions", "version")
	if err := os.WriteFile(version, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, got := printReport(t, env); got != want {
		t.Errorf("after an older agent:\n%s\nwant\n%s", got, want)
	}
	if b, _ := os.ReadFile(version); string(b) != "2\n" { //nolint:gosec // G304: a file the agent made in a temp dir.
		t.Errorf("version = %q, want 2", b)
	}
}
