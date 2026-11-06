package coding_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/coding"
)

var (
	now    = time.Date(2026, 11, 6, 18, 30, 0, 0, time.UTC)
	oldest = now.Add(-90 * 24 * time.Hour)
)

func parse(t *testing.T, body string) coding.Report {
	t.Helper()
	var r coding.Report
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

const good = `{
 "v": 1,
 "somethingNew": true,
 "tools": [{"tool":"claude","status":"ok"},{"tool":"opencode","status":"needs-sqlite"},{"tool":"pi","status":"weird"}],
 "sessions": [{"tool":"claude","id":"s1","project":"bield","first":"2026-11-06T17:56:29Z","last":"2026-11-06T18:40:12Z"}],
 "usage": [{"tool":"claude","session":"s1","model":"claude-opus-5-5","speed":"fast","hour":"2026-11-06T17",
            "in":53,"cr":123,"cw":456,"out":789,"reason":900,"n":12,"cost":1.5}],
 "limits": [{"tool":"codex","id":"codex:primary","window":10080,"used":18.5,"resets":1794910528,"status":"ok","plan":"free","seen":"2026-11-01T07:59:33Z"},
            {"tool":"claude","id":"five_hour","window":300,"used":null,"resets":0,"status":"rejected","plan":"","seen":"2026-10-30T20:43:23Z"}]
}`

func TestClean(t *testing.T) {
	t.Parallel()
	got, err := parse(t, good).Clean(now, oldest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 3 || got.Tools[1].Status != "needs-sqlite" || got.Tools[2].Status != "unreadable" {
		t.Errorf("Tools = %+v", got.Tools)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Project != "bield" ||
		!got.Sessions[0].Last.Equal(time.Date(2026, 11, 6, 18, 40, 12, 0, time.UTC)) {
		t.Errorf("Sessions = %+v", got.Sessions)
	}
	if len(got.Usage) != 1 {
		t.Fatalf("Usage = %+v", got.Usage)
	}
	u := got.Usage[0]
	if !u.Hour.Equal(time.Date(2026, 11, 6, 17, 0, 0, 0, time.UTC)) || u.Speed != "fast" ||
		u.Input != 53 || u.CacheRead != 123 || u.CacheWrite != 456 || u.Output != 789 || u.Requests != 12 {
		t.Errorf("Usage = %+v", u)
	}
	if u.Reasoning != 789 {
		t.Errorf("Reasoning = %d, want it held to the output, 789", u.Reasoning)
	}
	if u.CostMicro != 1_500_000 {
		t.Errorf("CostMicro = %d", u.CostMicro)
	}
	if len(got.Limits) != 2 {
		t.Fatalf("Limits = %+v", got.Limits)
	}
	if l := got.Limits[0]; l.Used == nil || *l.Used != 18.5 || l.ResetsAt == nil || l.ResetsAt.Unix() != 1794910528 || l.WindowMinutes != 10080 {
		t.Errorf("Limits[0] = %+v", l)
	}
	if l := got.Limits[1]; l.Used != nil || l.ResetsAt != nil || l.Status != "rejected" {
		t.Errorf("Limits[1] = %+v", l)
	}
}

func TestClean_leavesOutWhatIsOutsideTheWindow(t *testing.T) {
	t.Parallel()
	r := parse(t, `{"v":1,
	 "sessions":[
	  {"tool":"claude","id":"old","project":"","first":"2026-01-01T00:00:00Z","last":"2026-01-01T01:00:00Z"},
	  {"tool":"claude","id":"future","project":"","first":"2027-01-01T00:00:00Z","last":"2027-01-01T01:00:00Z"},
	  {"tool":"claude","id":"swapped","project":"","first":"2026-11-06T18:00:00Z","last":"2026-11-06T17:00:00Z"}],
	 "usage":[
	  {"tool":"claude","session":"old","model":"m","speed":"standard","hour":"2026-01-01T00","in":1,"cr":0,"cw":0,"out":0,"reason":0,"n":1,"cost":0},
	  {"tool":"claude","session":"future","model":"m","speed":"standard","hour":"2027-01-01T00","in":1,"cr":0,"cw":0,"out":0,"reason":0,"n":1,"cost":0},
	  {"tool":"claude","session":"now","model":"m","speed":"odd","hour":"2026-11-06T18","in":1,"cr":0,"cw":0,"out":0,"reason":0,"n":1,"cost":0}]}`)
	got, err := r.Clean(now, oldest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "swapped" || !got.Sessions[0].First.Before(got.Sessions[0].Last) {
		t.Errorf("Sessions = %+v", got.Sessions)
	}
	if len(got.Usage) != 1 || got.Usage[0].Session != "now" || got.Usage[0].Speed != "standard" {
		t.Errorf("Usage = %+v", got.Usage)
	}
}

func TestClean_errors(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 200)
	row := func(field, value string) string {
		usage := map[string]string{
			"tool": `"claude"`, "session": `"s"`, "model": `"m"`, "speed": `"standard"`, "hour": `"2026-11-06T17"`,
			"in": "1", "cr": "0", "cw": "0", "out": "0", "reason": "0", "n": "1", "cost": "0",
		}
		usage[field] = value
		var parts []string
		for k, v := range usage {
			parts = append(parts, `"`+k+`":`+v)
		}
		return `{"v":1,"usage":[{` + strings.Join(parts, ",") + `}]}`
	}
	tests := []struct {
		name, body, want string
	}{
		{"wrong version", `{"v":2}`, "format 2"},
		{"bad tool", row("tool", `"Claude Code"`), "tool name"},
		{"empty session", row("session", `""`), "session ID"},
		{"long model", row("model", `"`+long+`"`), "model name"},
		{"control in model", row("model", `"a\nb"`), "model name"},
		{"bad hour", row("hour", `"2026-11-06"`), "hour"},
		{"negative count", row("in", "-1"), "count"},
		{"huge count", row("out", "100000000000000"), "count"},
		{"negative cost", row("cost", "-0.5"), "cost"},
		{"huge cost", row("cost", "1e9"), "cost"},
		{"bad session time", `{"v":1,"sessions":[{"tool":"claude","id":"s","project":"","first":"yesterday","last":"today"}]}`, "time"},
		{"bad limit id", `{"v":1,"limits":[{"tool":"codex","id":"a b","window":1,"used":1,"resets":0,"status":"ok","plan":"","seen":"2026-11-01T07:59:33Z"}]}`, "limit 0"},
		{"limit used too high", `{"v":1,"limits":[{"tool":"codex","id":"a","window":1,"used":5000,"resets":0,"status":"ok","plan":"","seen":"2026-11-01T07:59:33Z"}]}`, "share used"},
		{"bad tool status row", `{"v":1,"tools":[{"tool":"","status":"ok"}]}`, "tool 0"},
	}
	for _, tt := range tests {
		_, err := parse(t, tt.body).Clean(now, oldest)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Clean error = %v, want one that mentions %q", tt.name, err, tt.want)
		}
	}
}

func TestClean_tooBig(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`{"v":1,"usage":[`)
	for i := range 2001 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"tool":"claude","session":"s","model":"m","speed":"standard","hour":"2026-11-06T17","in":1,"cr":0,"cw":0,"out":0,"reason":0,"n":1,"cost":0}`)
	}
	b.WriteString(`]}`)
	if _, err := parse(t, b.String()).Clean(now, oldest); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("Clean error = %v", err)
	}
}

func TestClean_projectIsCleaned(t *testing.T) {
	t.Parallel()
	r := parse(t, `{"v":1,"sessions":[{"tool":"claude","id":"s","project":"  my\tproject\u0007 ","first":"2026-11-06T17:00:00Z","last":"2026-11-06T17:00:00Z"}]}`)
	got, err := r.Clean(now, oldest)
	if err != nil {
		t.Fatal(err)
	}
	if p := got.Sessions[0].Project; p != "my project" {
		t.Errorf("Project = %q", p)
	}
}
