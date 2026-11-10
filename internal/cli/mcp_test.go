package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/api/apitest"
	"github.com/manasseh-fikadu/bield/internal/cli"
)

// serveMCP runs bield mcp as the device at from, on pipes that stand for its
// stdin and stdout. It returns the client's ends. The server stops at the end
// of the test, and the test fails if it doesn't stop when stdin closes.
func serveMCP(t *testing.T, s *apitest.Server, from netip.Addr, args ...string) (io.WriteCloser, io.ReadCloser) {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	e, _, stderr := env(t, s, from)
	e.Stdin, e.Stdout = stdinR, stdoutW
	done := make(chan int, 1)
	go func() {
		defer func() { _ = stdoutW.Close() }()
		done <- cli.Run(t.Context(), e, append([]string{"mcp"}, args...))
	}()
	t.Cleanup(func() {
		_ = stdinW.Close()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("bield mcp exited with %d: %s", code, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("bield mcp didn't stop when its stdin closed")
		}
	})
	return stdinW, stdoutR
}

// connectMCP connects an MCP client to bield mcp, at the protocol version
// given or else the newest.
func connectMCP(t *testing.T, s *apitest.Server, from netip.Addr, version string, args ...string) *mcp.ClientSession {
	t.Helper()
	w, r := serveMCP(t, s, from, args...)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(t.Context(), &mcp.IOTransport{Reader: r, Writer: w}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

// The server speaks the newest revision of the protocol, and older clients
// still get in.
func TestMCP_protocolVersions(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	for _, tt := range []struct{ name, ask, want string }{
		{"the newest", "", "2026-07-28"},
		{"the revision before it", "2025-11-25", "2025-11-25"},
		{"an old client", "2025-03-26", "2025-03-26"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := connectMCP(t, s, apitest.LaptopIP, tt.ask)
			if got := cs.InitializeResult().ProtocolVersion; got != tt.want {
				t.Errorf("negotiated %q, want %q", got, tt.want)
			}
			text, isErr := callTool(t, cs, "whoami", nil)
			if isErr || !strings.Contains(text, apitest.Alice) {
				t.Errorf("whoami = %q, error %v", text, isErr)
			}
		})
	}
}

// On the wire, with no SDK at the other end: every line on stdout is JSON-RPC,
// and server/discover offers 2026-07-28.
func TestMCP_wire(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	w, r := serveMCP(t, s, apitest.LaptopIP)
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 0, 1<<16), 1<<24)
	rpc := func(msg string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(w, msg+"\n"); err != nil {
			t.Fatal(err)
		}
		if !lines.Scan() {
			t.Fatalf("no answer to %s: %v", msg, lines.Err())
		}
		var got map[string]any
		if err := json.Unmarshal(lines.Bytes(), &got); err != nil {
			t.Fatalf("not JSON on stdout: %q", lines.Text())
		}
		if got["jsonrpc"] != "2.0" {
			t.Fatalf("not JSON-RPC: %q", lines.Text())
		}
		return got
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"fake","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`

	disc := rpc(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + meta + `}}`)
	res, ok := disc["result"].(map[string]any)
	if !ok {
		t.Fatalf("server/discover = %v", disc)
	}
	if v, _ := res["supportedVersions"].([]any); !slices.Contains(v, any("2026-07-28")) {
		t.Errorf("supportedVersions = %v", res["supportedVersions"])
	}

	call := rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{` + meta + `,"name":"status","arguments":{}}}`)
	result, ok := call["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("tools/call = %v", call)
	}
}

// Read-only is the default: without --allow-changes no tool that needs an
// admin is there at all, and calling one by name fails.
func TestMCP_readOnlyByDefault(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	cs := connectMCP(t, s, apitest.LaptopIP, "")
	tools := toolNames(t, cs)
	for _, want := range []string{"status", "whoami", "devices", "devices_show", "alerts", "checks", "checks_history", "events", "docker_ps"} {
		if tools[want] == nil {
			t.Errorf("tool %q is missing", want)
		}
	}
	for name, tool := range tools {
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s changes things, without --allow-changes", name)
		}
	}
	for _, hidden := range []string{"alerts_ack", "docker_stop", "files_cat", "docker_logs", "settings", "access_set"} {
		if tools[hidden] != nil {
			t.Errorf("tool %q is there without --allow-changes", hidden)
		}
	}
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "alerts_ack", Arguments: map[string]any{"id": 1}}); err == nil {
		t.Error("alerts_ack worked without --allow-changes")
	}
}

func TestMCP_annotationsWithChanges(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	tools := toolNames(t, connectMCP(t, s, apitest.LaptopIP, "", "--allow-changes"))
	for _, tt := range []struct {
		name                  string
		readOnly, destructive bool
	}{
		{"devices", true, false},
		{"docker_logs", true, false},
		{"settings", true, false},
		{"alerts_ack", false, false},
		{"docker_stop", false, true},
		{"checks_delete", false, true},
		{"files_write", false, true},
		{"access_set", false, true},
	} {
		tool := tools[tt.name]
		if tool == nil {
			t.Errorf("no tool %q", tt.name)
			continue
		}
		a := tool.Annotations
		if a.ReadOnlyHint != tt.readOnly || a.DestructiveHint == nil || *a.DestructiveHint != tt.destructive {
			t.Errorf("%s: annotations = %+v", tt.name, a)
		}
	}
	for _, gone := range []string{"connect", "mcp", "completion", "files_get", "files_put"} {
		if tools[gone] != nil {
			t.Errorf("tool %q shouldn't be there", gone)
		}
	}
}

// The first thing the issue asks for: an agent lists the devices, finds the
// failing check and reads its history, in the CLI's --json shapes.
func TestMCP_readTools(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	cs := connectMCP(t, s, apitest.PhoneIP, "")

	text, isErr := callTool(t, cs, "devices", map[string]any{"status": "online"})
	var devices []api.Device
	if err := json.Unmarshal([]byte(text), &devices); err != nil || isErr || len(devices) == 0 {
		t.Fatalf("devices = %q, %v, error %v", text, err, isErr)
	}
	text, isErr = callTool(t, cs, "alerts", nil)
	var alerts []api.Alert
	if err := json.Unmarshal([]byte(text), &alerts); err != nil || isErr || len(alerts) != 1 {
		t.Fatalf("alerts = %q, %v, error %v", text, err, isErr)
	}
	// A viewer gets a read refused by Bield as an error it can read.
	if text, isErr = callTool(t, cs, "devices_history", map[string]any{"device": "no-such", "range": "6h"}); !isErr || !strings.Contains(text, "no device is called") {
		t.Errorf("devices_history = %q, error %v", text, isErr)
	}
}

// A change lands in Activity with the caller, marked as coming from MCP.
func TestMCP_changeIsMarked(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	cs := connectMCP(t, s, apitest.LaptopIP, "", "--allow-changes")

	text, isErr := callTool(t, cs, "alerts_ack", map[string]any{"id": id})
	var a api.Alert
	if err := json.Unmarshal([]byte(text), &a); err != nil || isErr || a.State != api.AlertStateAcknowledged {
		t.Fatalf("alerts_ack = %q, %v, error %v", text, err, isErr)
	}
	text, isErr = callTool(t, cs, "events", map[string]any{"kind": "alert"})
	var page api.EventList
	if err := json.Unmarshal([]byte(text), &page); err != nil || isErr {
		t.Fatalf("events = %q, %v, error %v", text, err, isErr)
	}
	if !slices.ContainsFunc(page.Events, func(e api.Event) bool {
		return e.Via == api.ViaMCP && e.Actor == apitest.Alice && strings.HasPrefix(e.Message, "Acknowledged")
	}) {
		t.Errorf("no acknowledgement by %s via mcp in %+v", apitest.Alice, page.Events)
	}
}

// Who the caller is decides what a tool does, as in the browser, and a refusal
// is a tool error with the hint from the command line.
func TestMCP_refusalsAreToolErrors(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	for _, tt := range []struct {
		name string
		from netip.Addr
		tool string
		args map[string]any
		want string
		code int
	}{
		{"a viewer changing something", apitest.PhoneIP, "alerts_ack", map[string]any{"id": id}, "a viewer; this needs an admin", cli.ExitDenied},
		{"a tagged server", apitest.ServerIP, "devices", nil, "tagged in Tailscale", cli.ExitDenied},
		{"no role", apitest.TabletIP, "devices", nil, "have no role in Bield", cli.ExitDenied},
		{"a missing argument", apitest.LaptopIP, "alerts_show", nil, `needs the argument \"id\"`, cli.ExitUsage},
		{"an unknown argument", apitest.LaptopIP, "alerts", map[string]any{"follow": true}, `no argument called \"follow\"`, cli.ExitUsage},
		{"a value of the wrong kind", apitest.LaptopIP, "alerts", map[string]any{"limit": "lots"}, "takes a whole number", cli.ExitUsage},
		{"a bad time", apitest.LaptopIP, "alerts_snooze", map[string]any{"id": id, "for": "soon"}, "--for takes a time", cli.ExitUsage},
		{"nothing there", apitest.LaptopIP, "alerts_show", map[string]any{"id": 9999}, "", cli.ExitNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := connectMCP(t, s, tt.from, "", "--allow-changes")
			text, isErr := callTool(t, cs, tt.tool, tt.args)
			if !isErr || !strings.Contains(text, tt.want) {
				t.Fatalf("%s = %q, error %v, want an error with %q", tt.tool, text, isErr, tt.want)
			}
			var f struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal([]byte(text), &f); err != nil || f.Code != tt.code {
				t.Errorf("failure %q: code %d, want %d (%v)", text, f.Code, tt.code, err)
			}
		})
	}
}

// A value that starts with a dash stays a value, since it could otherwise be
// read as a flag of the command.
func TestMCP_valuesAreNotFlags(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	id := openAlert(t, s, "disk:bield", "Disk 90% full on bield")
	cs := connectMCP(t, s, apitest.LaptopIP, "", "--allow-changes")
	text, isErr := callTool(t, cs, "alerts_note", map[string]any{"id": id, "text": "--yes -f"})
	var n api.AlertNote
	if err := json.Unmarshal([]byte(text), &n); err != nil || isErr || n.Text != "--yes -f" {
		t.Errorf("alerts_note = %q, %v, error %v", text, err, isErr)
	}
}

// With no address, tools say how to give one, and the server stays up, so
// that running bield connect is enough.
func TestMCP_noAddress(t *testing.T) {
	t.Parallel()
	s := apitest.New(t)
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	e, _, _ := env(t, s, apitest.LaptopIP)
	e.Stdin, e.Stdout, e.Getenv = stdinR, stdoutW, func(string) string { return "" }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() { done <- cli.Run(ctx, e, []string{"mcp"}) }()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(t.Context(), &mcp.IOTransport{Reader: stdoutR, Writer: stdinW}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := callTool(t, cs, "status", nil); !isErr || !strings.Contains(text, "run bield connect") {
		t.Errorf("status = %q, error %v", text, isErr)
	}
	// Stopped by a signal, it exits cleanly.
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Error("bield mcp didn't stop when its context ended")
	}
	_ = cs.Close()
}
