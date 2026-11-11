package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/editor"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
)

// testRelease is what the editor tests install: a build for alice's laptop,
// which is a Mac.
var testRelease = editor.Release{Version: "9.9.9", Assets: []editor.Asset{{
	OS: "macos", Arch: "arm64", URL: "https://example.com/code-server.tar.gz",
	SHA256: strings.Repeat("ab", 32), Size: 230 << 20, Dir: "code-server-9.9.9-macos-arm64",
}}}

const editorSocket = "/Users/alice/.local/share/bield/code-server/run/code.sock"

// fakeLaptop answers the editor's scripts as alice's laptop would, and runs
// a fake code-server on the socket once it's started.
type fakeLaptop struct {
	sshd *sshtest.Server
	mu   sync.Mutex
	// version is what's installed, and running whether it runs.
	version string
	running bool
	// failInstall is what a failing install says, or "".
	failInstall string
	// hold, if set, holds an install until it's closed.
	hold     chan struct{}
	installs int
	scripts  []string
}

func (f *fakeLaptop) handle(x *sshtest.Exec) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := x.Command
	switch {
	case strings.Contains(cmd, `echo "system=$(uname -s)"`):
		f.scripts = append(f.scripts, "status")
		_, _ = fmt.Fprintf(x.Stdout, "system=Darwin\nmachine=arm64\nhome=/Users/alice\nsocket=%s\n", editorSocket)
		if f.version != "" {
			_, _ = fmt.Fprintf(x.Stdout, "version=%s\n", f.version)
		}
		if f.running {
			_, _ = fmt.Fprintln(x.Stdout, "running=1")
		}
		// In KiB, as df says.
		_, _ = fmt.Fprintln(x.Stdout, "free=102400")
	case strings.Contains(cmd, "Downloading $url"):
		if hold := f.hold; hold != nil {
			f.mu.Unlock()
			<-hold
			f.mu.Lock()
		}
		f.scripts = append(f.scripts, "install")
		f.installs++
		f.stopLocked()
		if !strings.Contains(cmd, "'"+testRelease.Assets[0].SHA256+"'") || !strings.Contains(cmd, "'9.9.9'") {
			_, _ = fmt.Fprintln(x.Stdout, "the install didn't get the pinned build")
			return 1
		}
		if f.failInstall != "" {
			_, _ = fmt.Fprintln(x.Stdout, f.failInstall)
			return 1
		}
		f.version = "9.9.9"
		_, _ = fmt.Fprintln(x.Stdout, "Installed code-server 9.9.9")
	case strings.Contains(cmd, `nohup "$bin"`):
		f.scripts = append(f.scripts, "start")
		if f.version == "" {
			_, _ = fmt.Fprintln(x.Stdout, "code-server isn't installed on this machine.")
			return 3
		}
		f.running = true
		f.sshd.HandleSocket(editorSocket, func(c net.Conn) { serveConn(c, codeServer{}) })
		_, _ = fmt.Fprintf(x.Stdout, "socket=%s\n", editorSocket)
	case strings.Contains(cmd, `echo "Stopped"`):
		f.scripts = append(f.scripts, "stop")
		f.stopLocked()
	case strings.Contains(cmd, "Removed code-server"):
		f.scripts = append(f.scripts, "remove")
		f.stopLocked()
		f.version = ""
	}
	return 0
}

func (f *fakeLaptop) stopLocked() {
	f.running = false
	f.sshd.HandleSocket(editorSocket, nil)
}

func (f *fakeLaptop) ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.scripts)
}

// codeServer is a fake code-server: its pages say what they were asked
// for, and /ws is a WebSocket that echoes.
type codeServer struct{}

func (codeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		// websocket.Accept checks the Origin against the Host, as code-server does.
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			typ, b, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), typ, b); err != nil {
				return
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "code-server-session", Value: "x", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	//nolint:gosec // G705: a test server answering plain text.
	_, _ = fmt.Fprintf(w, "code-server %s %s on %s, authorization %q, cookie %q",
		r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("Authorization"), r.Header.Get("Cookie"))
}

// editorEnv is an sshEnv whose Bield serves editors on ports 4710 and 4711,
// with alice's laptop as a fakeLaptop.
type editorEnv struct {
	*sshEnv
	eds      *editor.Manager
	laptop   *fakeLaptop
	stopping chan struct{}
}

func newEditorEnv(t *testing.T) *editorEnv {
	t.Helper()
	stopping := make(chan struct{})
	e := &editorEnv{sshEnv: newSSHEnv(t, testStore(t), stopping), stopping: stopping}
	e.laptop = &fakeLaptop{sshd: e.sshd}
	e.sshd.Handle(e.laptop.handle)
	e.setUp(t)
	e.eds = editor.NewManager(editor.Options{
		Remote: e.opts.SSH, Store: e.st, Tailnet: e.opts.Snapshot, Listen: e.tn.ListenPort,
		Ports: editor.Ports{First: 4710, Last: 4711}, Release: testRelease,
	})
	if err := e.eds.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.opts.Editors = e.eds
	e.h = api.New(e.opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.eds.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-stopping:
		default:
			close(stopping)
		}
		cancel()
		<-done
	})
	return e
}

func (e *editorEnv) editor(t *testing.T, from netip.Addr) api.DeviceEditor {
	t.Helper()
	var out api.DeviceEditor
	get(t, e.h, "/api/devices/laptop/editor", from, &out)
	return out
}

// install installs code-server on the laptop as alice, and waits for the
// install to end.
func (e *editorEnv) install(t *testing.T) api.EditorInstall {
	t.Helper()
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/install", laptopIP, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("install = %d %s", rec.Code, rec.Body)
	}
	var job api.EditorInstall
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if !job.Running || job.By != alice || job.Version != "9.9.9" {
		t.Errorf("the install's answer = %+v", job)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := e.editor(t, laptopIP).Install
		switch {
		case got == nil:
			t.Fatal("the editor says nothing of the install")
		case !got.Running:
			if got.EndedAt == nil {
				t.Errorf("the install ended at no time: %+v", got)
			}
			return *got
		case time.Now().After(deadline):
			t.Fatalf("the install didn't end: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// portRequest returns a request to the editor's port from a browser on a
// device, opening a page unless it says otherwise.
func portRequest(t *testing.T, method, url string, header map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req
}

func (e *editorEnv) doPort(t *testing.T, from netip.Addr, req *http.Request) (int, string) {
	t.Helper()
	res, err := e.tn.Client(from).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

// waitEvents waits for the activity log to have n events of the editor's.
func (e *editorEnv) waitEvents(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		events := e.events(t, api.EventKindEditor)
		if len(events) >= n || time.Now().After(deadline) {
			var out []string
			for _, ev := range slices.Backward(events) {
				out = append(out, ev.Actor+": "+ev.Message)
			}
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEditor_installAndOpen(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)

	got := e.editor(t, laptopIP)
	if got.State != api.EditorStateNotInstalled || got.Latest != "9.9.9" || got.Download != 230<<20 ||
		got.Disk <= got.Download || got.Free == nil || *got.Free != 100<<20 || got.Home != "/Users/alice" {
		t.Fatalf("before the install, editor = %+v", got)
	}
	// Viewers don't see it, and can't install it.
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/editor", phoneIP)); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer's GET = %d", rec.Code)
	}
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/install", phoneIP, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer's install = %d", rec.Code)
	}

	if run := e.install(t); !run.OK || !strings.Contains(run.Output, "Installed code-server 9.9.9") || run.By != alice {
		t.Fatalf("install = %+v", run)
	}
	got = e.editor(t, laptopIP)
	url := "http://bield.example.ts.net:4710"
	if got.State != api.EditorStateInstalled || got.Version != "9.9.9" || got.Running || got.Port != 4710 || got.URL != url || got.Secure {
		t.Fatalf("after the install, editor = %+v", got)
	}

	// Starting it says where it is.
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/start", laptopIP, "")
	var open api.EditorOpen
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &open); err != nil {
		t.Fatal(err)
	}
	if open.URL != url {
		t.Errorf("start = %+v, want %s", open, url)
	}
	if !e.editor(t, laptopIP).Running {
		t.Error("after the start, it isn't running")
	}

	// alice opens it from a link on Bield, and code-server answers.
	req := portRequest(t, http.MethodGet, url+"/?folder=/Users/alice", map[string]string{
		"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Cookie": "other=1",
	})
	res, err := e.tn.Client(laptopIP).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if want := `code-server GET /?folder=/Users/alice on bield.example.ts.net:4710, authorization "", cookie ""`; res.StatusCode != http.StatusOK || string(b) != want {
		t.Errorf("opening the editor = %d %q, want %q", res.StatusCode, b, want)
	}
	// Another origin can't read it, frame it, or get its cookies.
	for k, want := range map[string]string{
		"Cross-Origin-Resource-Policy": "same-origin", "X-Frame-Options": "SAMEORIGIN",
		"Content-Security-Policy": "frame-ancestors 'self'", "Set-Cookie": "",
	} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	var (
		status int
		body   string
	)

	// Its WebSocket opens a session, which the activity log shows.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:bodyclose // The WebSocket owns the response's body.
	conn, _, err := websocket.Dial(ctx, "ws://bield.example.ts.net:4710/ws", &websocket.DialOptions{
		HTTPClient: e.tn.Client(laptopIP),
		HTTPHeader: http.Header{"Origin": {url}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, b, err := conn.Read(ctx); err != nil || string(b) != "hello" {
		t.Fatalf("echo = %q, %v", b, err)
	}
	if by := e.editor(t, laptopIP).OpenBy; !slices.Equal(by, []string{alice}) {
		t.Errorf("open by %q, want alice", by)
	}
	want := []string{
		alice + ": Installed VS Code (code-server 9.9.9) on laptop",
		alice + ": Opened VS Code on laptop",
	}
	if events := e.waitEvents(t, 2); !slices.Equal(events, want) {
		t.Errorf("events = %q, want %q", events, want)
	}

	// Stopping it closes the WebSocket.
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/stop", laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("stop = %d %s", rec.Code, rec.Body)
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Error("the WebSocket is still open after the stop")
	}
	if got := e.editor(t, laptopIP); got.Running || len(got.OpenBy) != 0 {
		t.Errorf("after the stop, editor = %+v", got)
	}

	// Now the editor's page says it isn't running, with a button.
	status, body = e.doPort(t, laptopIP, portRequest(t, http.MethodGet, url+"/", map[string]string{
		"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document",
	}))
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "VS Code isn&#39;t running on laptop") ||
		!strings.Contains(body, `id="start"`) {
		t.Errorf("the page of a stopped editor = %d %q", status, body)
	}
	// What the page's script asks for gets the message as JSON.
	status, body = e.doPort(t, laptopIP, portRequest(t, http.MethodGet, url+"/static/x.js", map[string]string{
		"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "script",
	}))
	if status != http.StatusServiceUnavailable || !strings.Contains(body, `"error":"VS Code isn't running on laptop`) {
		t.Errorf("a script of a stopped editor = %d %q", status, body)
	}
	// The button starts it, with the CSRF header.
	start := map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors", "Origin": url}
	if status, body := e.doPort(t, laptopIP, portRequest(t, http.MethodPost, url+"/__bield/start", start)); status != http.StatusForbidden {
		t.Errorf("start without the CSRF header = %d %q", status, body)
	}
	start[api.CSRFHeader] = "1"
	if status, body := e.doPort(t, laptopIP, portRequest(t, http.MethodPost, url+"/__bield/start", start)); status != http.StatusNoContent {
		t.Errorf("start = %d %q", status, body)
	}
	if !e.editor(t, laptopIP).Running {
		t.Error("the page's button didn't start it")
	}
	if events := e.waitEvents(t, 3); len(events) < 3 || events[2] != alice+": Stopped VS Code on laptop" {
		t.Errorf("events = %q, want the stop", events)
	}
	// Viewers don't see any of it in the activity log.
	var seen api.EventList
	get(t, e.h, "/api/events", phoneIP, &seen)
	for _, ev := range seen.Events {
		if ev.Kind == api.EventKindEditor {
			t.Errorf("a viewer sees %+v", ev)
		}
	}
}

func TestEditor_portRefuses(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)
	e.install(t)
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/start", laptopIP, ""); rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	const url = "http://bield.example.ts.net:4710"
	page := map[string]string{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
	tests := []struct {
		name   string
		from   netip.Addr
		method string
		url    string
		header map[string]string
		want   int
	}{
		{"alice opens it", laptopIP, http.MethodGet, url + "/", page, http.StatusOK},
		{"a viewer", phoneIP, http.MethodGet, url + "/", page, http.StatusForbidden},
		{"no role", tabletIP, http.MethodGet, url + "/", page, http.StatusForbidden},
		{"a tagged device", serverIP, http.MethodGet, url + "/", page, http.StatusForbidden},
		{"Bield's own node", bieldIP, http.MethodGet, url + "/", page, http.StatusForbidden},
		{"another name", laptopIP, http.MethodGet, "http://evil.example.com:4710/", page, http.StatusMisdirectedRequest},
		{"another port in the Host", laptopIP, http.MethodGet, "http://bield.example.ts.net:4711/", nil, http.StatusMisdirectedRequest},
		{"an API token", laptopIP, http.MethodGet, url + "/", map[string]string{"Authorization": "Bearer bield_x"}, http.StatusForbidden},
		{"a fetch from another site", laptopIP, http.MethodGet, url + "/vscode-remote-resource?path=/etc/passwd",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty"}, http.StatusForbidden},
		{"a script from Bield's own page", laptopIP, http.MethodGet, url + "/x.js",
			map[string]string{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "script"}, http.StatusForbidden},
		{"a frame on another site", laptopIP, http.MethodGet, url + "/",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, http.StatusForbidden},
		{"a form from another site", laptopIP, http.MethodPost, url + "/__bield/start",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, http.StatusForbidden},
		{"a link from another site", laptopIP, http.MethodGet, url + "/",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, http.StatusOK},
		{"an old browser's POST from elsewhere", laptopIP, http.MethodPost, url + "/x",
			map[string]string{"Origin": "https://evil.example.com"}, http.StatusForbidden},
		{"its own fetch", laptopIP, http.MethodPost, url + "/x",
			map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors", "Origin": url}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := portRequest(t, tt.method, tt.url, tt.header)
			if strings.Contains(tt.url, "evil") || strings.Contains(tt.url, ":4711") {
				// The address is the editor's, whatever the Host says.
				req.URL.Host = "bield.example.ts.net:4710"
				req.Host = strings.TrimPrefix(strings.TrimSuffix(tt.url, "/"), "http://")
			}
			if status, body := e.doPort(t, tt.from, req); status != tt.want {
				t.Errorf("status = %d %q, want %d", status, body, tt.want)
			}
		})
	}

	// A WebSocket from another origin is refused.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, origin := range []string{"http://bield.example.ts.net", "http://bield.example.ts.net:4711", "https://evil.example.com"} {
		//nolint:bodyclose // A refused dial has no WebSocket, and the client closed its body.
		_, res, err := websocket.Dial(ctx, "ws://bield.example.ts.net:4710/ws", &websocket.DialOptions{
			HTTPClient: e.tn.Client(laptopIP),
			HTTPHeader: http.Header{"Origin": {origin}},
		})
		if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Errorf("a WebSocket from %s = %v, %v, want 403", origin, res, err)
		}
	}
	// API tokens and viewers can't start it from Bield either.
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/start", phoneIP, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer's start = %d", rec.Code)
	}
}

func TestEditor_failedInstall(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)
	e.laptop.failInstall = "The download's SHA-256 is 00, not ab, so Bield didn't install it."
	run := e.install(t)
	if run.OK || !strings.Contains(run.Output, "so Bield didn't install it") {
		t.Errorf("install = %+v, want the script's complaint", run)
	}
	if got := e.editor(t, laptopIP); got.State != api.EditorStateNotInstalled || got.Port != 0 {
		t.Errorf("after the failed install, editor = %+v", got)
	}
	if editors, err := e.st.Editors(t.Context()); err != nil || len(editors) != 0 {
		t.Errorf("the store has %v, %v", editors, err)
	}
	if got := e.editor(t, laptopIP).Install; got == nil || got.OK || !strings.Contains(got.Output, "didn't install it") {
		t.Errorf("the editor's last install = %+v", got)
	}
	// Its port is free for the next one.
	e.laptop.failInstall = ""
	e.install(t)
	if got := e.editor(t, laptopIP); got.Port != 4710 {
		t.Errorf("after the install, port = %d, want 4710", got.Port)
	}
	events := e.events(t, api.EventKindEditor)
	if len(events) != 2 || events[1].Level != string(api.EventLevelError) || events[1].Message != "Installing VS Code on laptop failed" {
		t.Errorf("events = %+v", events)
	}
}

func TestEditor_remove(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)
	e.install(t)
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/start", laptopIP, ""); rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	// An update installs again, on the same port.
	if run := e.install(t); !run.OK {
		t.Fatalf("update = %+v", run)
	}
	if got := e.editor(t, laptopIP); got.Port != 4710 || got.Running {
		t.Errorf("after the update, editor = %+v, want the same port, stopped", got)
	}

	rec := send(t, e.h, http.MethodDelete, "/api/devices/laptop/editor?keep=data", laptopIP, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	if got := e.editor(t, laptopIP); got.State != api.EditorStateNotInstalled || got.URL != "" || got.Install != nil {
		t.Errorf("after the removal, editor = %+v", got)
	}
	// Its port answers no more.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req := portRequest(t, http.MethodGet, "http://bield.example.ts.net:4710/", nil).WithContext(ctx)
	if res, err := e.tn.Client(laptopIP).Do(req); err == nil {
		_ = res.Body.Close()
		t.Errorf("the removed editor's port answered %d", res.StatusCode)
	}
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/start", laptopIP, ""); rec.Code != http.StatusConflict {
		t.Errorf("starting a removed editor = %d %s", rec.Code, rec.Body)
	}
	want := []string{"install", "start", "install", "remove"}
	if got := slices.DeleteFunc(e.laptop.ran(), func(s string) bool { return s == "status" }); !slices.Equal(got, want) {
		t.Errorf("scripts besides the status = %q, want %q", got, want)
	}
	events := e.events(t, api.EventKindEditor)
	if len(events) == 0 || events[0].Message != "Removed VS Code from laptop, and kept its settings and extensions" {
		t.Errorf("events = %+v", events)
	}
}

func TestEditor_needsSSH(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)
	if err := e.opts.SSH.Remove(t.Context(), "laptop"); err != nil {
		t.Fatal(err)
	}
	got := e.editor(t, laptopIP)
	if got.State != api.EditorStateUnavailable || !strings.Contains(got.Reason, "Set up SSH access") {
		t.Errorf("without SSH, editor = %+v", got)
	}
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/install", laptopIP, ""); rec.Code != http.StatusConflict {
		t.Errorf("install without SSH = %d %s", rec.Code, rec.Body)
	}
	if got := e.editor(t, laptopIP); got.Install != nil {
		t.Errorf("a refused install shows as %+v", got.Install)
	}
	// The phone runs iOS, which Bield can't log in to.
	var phone api.DeviceEditor
	get(t, e.h, "/api/devices/phone/editor", laptopIP, &phone)
	if phone.State != api.EditorStateUnavailable || phone.Reason == "" {
		t.Errorf("the phone's editor = %+v", phone)
	}
}

func TestEditor_oneInstallAtATime(t *testing.T) {
	t.Parallel()
	e := newEditorEnv(t)
	hold := make(chan struct{})
	e.laptop.mu.Lock()
	e.laptop.hold = hold
	e.laptop.mu.Unlock()
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/editor/install", laptopIP, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("install = %d %s", rec.Code, rec.Body)
	}
	if got := e.editor(t, laptopIP).Install; got == nil || !got.Running {
		t.Errorf("while it runs, install = %+v", got)
	}
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/devices/laptop/editor/install"},
		{http.MethodDelete, "/api/devices/laptop/editor"},
	} {
		if rec := send(t, e.h, req.method, req.path, laptopIP, ""); rec.Code != http.StatusConflict {
			t.Errorf("%s %s during the install = %d %s", req.method, req.path, rec.Code, rec.Body)
		}
	}
	e.laptop.mu.Lock()
	e.laptop.hold = nil
	e.laptop.mu.Unlock()
	close(hold)
	deadline := time.Now().Add(10 * time.Second)
	for e.editor(t, laptopIP).State != api.EditorStateInstalled {
		if time.Now().After(deadline) {
			t.Fatal("the install didn't end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := e.laptop.installs; n != 1 {
		t.Errorf("%d installs ran, want 1", n)
	}
}

// A demo says VS Code is off in it, with a 200, so the device page loads
// without a refused request.
func TestEditor_demo(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{Demo: true}))
	var got api.DeviceEditor
	get(t, h, "/api/devices/laptop/editor", laptopIP, &got)
	if got.State != api.EditorStateUnavailable || !strings.Contains(got.Reason, "turned off in this demo") || got.Latest == "" {
		t.Errorf("in a demo, editor = %+v", got)
	}
}
