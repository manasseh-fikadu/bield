package cli

import (
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	t.Parallel()
	flags := []Flag{
		{Name: "tail", Short: 'n', Kind: Int},
		{Name: "follow", Short: 'f', Kind: Bool},
		{Name: "since"},
		{Name: "for", Kind: Duration},
	}
	for _, tt := range []struct {
		name    string
		args    []string
		vals    map[string]string
		pos     []string
		wantErr string
	}{
		{name: "none", args: []string{"a", "b"}, vals: map[string]string{}, pos: []string{"a", "b"}},
		{name: "among arguments", args: []string{"a", "-n", "5", "b", "--follow"}, vals: map[string]string{"tail": "5", "follow": "true"}, pos: []string{"a", "b"}},
		{name: "equals", args: []string{"--since=10m", "--follow=false"}, vals: map[string]string{"since": "10m", "follow": "false"}},
		{name: "a negative number is an argument", args: []string{"-5", "-0.5"}, vals: map[string]string{}, pos: []string{"-5", "-0.5"}},
		{name: "after --", args: []string{"--", "-f", "--tail"}, vals: map[string]string{}, pos: []string{"-f", "--tail"}},
		{name: "a lone dash", args: []string{"-", "x"}, vals: map[string]string{}, pos: []string{"-", "x"}},
		{name: "days", args: []string{"--for", "2d"}, vals: map[string]string{"for": "2d"}},
		{name: "unknown", args: []string{"--bogus"}, wantErr: "unknown flag --bogus"},
		{name: "unknown short", args: []string{"-x"}, wantErr: "unknown flag -x"},
		{name: "missing value", args: []string{"--since"}, wantErr: "--since needs a value"},
		{name: "not a number", args: []string{"-n", "x"}, wantErr: "-n takes a number"},
		{name: "not a time", args: []string{"--for", "soon"}, wantErr: "--for takes a time"},
		{name: "not a boolean", args: []string{"--follow=maybe"}, wantErr: "--follow takes true or false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vals, pos, err := parseFlags(flags, tt.args)
			if tt.wantErr != "" {
				if err == nil || err.Error()[:min(len(err.Error()), len(tt.wantErr))] != tt.wantErr {
					t.Fatalf("parseFlags(%q) error = %v, want %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags(%q) = %v", tt.args, err)
			}
			if !reflect.DeepEqual(vals, tt.vals) || !reflect.DeepEqual(pos, tt.pos) {
				t.Errorf("parseFlags(%q) = %v, %q, want %v, %q", tt.args, vals, pos, tt.vals, tt.pos)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]time.Duration{"90s": 90 * time.Second, "4h": 4 * time.Hour, "2d": 48 * time.Hour, "0.5d": 12 * time.Hour, "1h30m": 90 * time.Minute} {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "-1h", "-2d", "soon", "1x"} {
		if got, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) = %v, want an error", in, got)
		}
	}
}

// Every command is complete: it says what it does, takes arguments in a form
// the usage shows, and no two commands share a name or a flag.
func TestCommandsTable(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range commands {
		if seen[c.Name] {
			t.Errorf("two commands are called %q", c.Name)
		}
		seen[c.Name] = true
		if c.Summary == "" || c.Run == nil {
			t.Errorf("command %q has no summary or no Run", c.Name)
		}
		if c.Summary != "" && (c.Summary[len(c.Summary)-1] == '.' || c.Summary[0] < 'a' || c.Summary[0] > 'z') {
			t.Errorf("command %q summary %q should start in lower case and have no full stop", c.Name, c.Summary)
		}
		if c.Destructive && !c.Admin && c.Name != "access who-can-view" {
			t.Errorf("command %q is destructive but not for admins only", c.Name)
		}
		names := map[string]bool{}
		for _, f := range c.Flags {
			switch f.Name {
			case "url", "json", "yes", "timeout", "help", "no-color":
				t.Errorf("command %q has a flag %q that every command takes", c.Name, f.Name)
			}
			if names[f.Name] || f.Usage == "" {
				t.Errorf("command %q has a duplicate or undescribed flag %q", c.Name, f.Name)
			}
			names[f.Name] = true
		}
		if c.Max >= 0 && c.Max < c.Min {
			t.Errorf("command %q takes at most %d arguments but at least %d", c.Name, c.Max, c.Min)
		}
	}
}

// docs/cli.md names every command, so a new one can't be left out of it.
func TestDocsNameEveryCommand(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, c := range commands {
		words := c.words()
		// A group, such as "alerts ack, unack and snooze", is written with its first
		// word and the last word of the name, so look for both.
		if !strings.Contains(text, "bield "+words[0]) || !strings.Contains(text, words[len(words)-1]) {
			t.Errorf("docs/cli.md doesn't mention the command %q", c.Name)
		}
	}
	for _, flag := range []string{"--json", "--yes", "--timeout", "--url", "--follow"} {
		if !strings.Contains(text, flag) {
			t.Errorf("docs/cli.md doesn't mention %s", flag)
		}
	}
}

// Every command either is a tool or says why not, a follow flag is never part
// of a tool, and what a tool is told about its arguments is complete.
func TestToolsCoverCommands(t *testing.T) {
	t.Parallel()
	tools := map[string]mcpTool{}
	for _, tl := range mcpTools(true) {
		tools[tl.cmd.Name] = tl
	}
	nameOK := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	for _, c := range commands {
		tl, isTool := tools[c.Name]
		if c.NoTool != "" {
			if isTool {
				t.Errorf("command %q says it isn't a tool, but is", c.Name)
			}
			continue
		}
		if !isTool {
			t.Errorf("command %q is no tool, and doesn't say why", c.Name)
			continue
		}
		if !regexp.MustCompile(`^[a-z_]+$`).MatchString(tl.def.Name) {
			t.Errorf("command %q has the tool name %q", c.Name, tl.def.Name)
		}
		for _, banned := range []string{"--yes", "--json"} {
			if strings.Contains(tl.def.Description, banned) {
				t.Errorf("tool %q is told about %s: %q", tl.def.Name, banned, tl.def.Description)
			}
		}
		for _, f := range c.Flags {
			if c.Stream && f.Name == "follow" && !f.CLIOnly {
				t.Errorf("tool %q would follow", tl.def.Name)
			}
		}
		seen := map[string]bool{}
		for _, p := range tl.params {
			if !nameOK.MatchString(p.name) || seen[p.name] {
				t.Errorf("tool %q has a bad or duplicate argument %q", tl.def.Name, p.name)
			}
			seen[p.name] = true
			if p.schema["description"] == "" || p.schema["description"] == nil {
				t.Errorf("tool %q doesn't describe its argument %q", tl.def.Name, p.name)
			}
		}
		if c.Min > 0 && !slices.ContainsFunc(tl.params, func(p toolParam) bool { return p.required }) {
			t.Errorf("tool %q needs arguments but requires none", tl.def.Name)
		}
	}
}

// The table says who may run a command and whether it changes anything, and
// the tools go by that.
func TestReadOnlyTools(t *testing.T) {
	t.Parallel()
	for _, tl := range mcpTools(false) {
		if tl.cmd.Admin {
			t.Errorf("tool %q needs an admin, but is there by default", tl.def.Name)
		}
	}
	if len(mcpTools(true)) <= len(mcpTools(false)) {
		t.Error("--allow-changes adds no tools")
	}
}

func TestBindArgs(t *testing.T) {
	t.Parallel()
	tl := mcpTool{}
	for _, x := range mcpTools(true) {
		if x.cmd.Name == "settings set" {
			tl = x
		}
	}
	_, pos, _, err := tl.bind([]byte(`{"settings":["a=1","b=2"]}`))
	if err != nil || !reflect.DeepEqual(pos, []string{"a=1", "b=2"}) {
		t.Errorf("bind list = %q, %v", pos, err)
	}
	if _, _, _, err := tl.bind([]byte(`{"settings":"a=1"}`)); err == nil {
		t.Error("a list given as text was accepted")
	}
	if _, _, _, err := tl.bind([]byte(`{`)); err == nil {
		t.Error("bad JSON was accepted")
	}
}

func TestCappedBuffer(t *testing.T) {
	t.Parallel()
	var c cappedBuffer
	if _, err := c.Write(make([]byte, maxToolResult)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("x")); !errors.Is(err, errToolResultTooBig) {
		t.Errorf("write past the cap = %v", err)
	}
}
