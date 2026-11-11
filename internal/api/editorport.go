package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// editorAction is where Bield's own routes are on an editor's port, which
// code-server's never are.
const editorAction = "/__bield/"

// editorIdleConns is how long the proxy keeps a connection to code-server
// that no request uses. code-server stops once it has had no connection for
// a while, so the proxy mustn't hold one open for nobody.
const editorIdleConns = 20 * time.Second

// EditorHandler returns the handler for the port that the editor of the
// machine with access ID accessID is served on (see editor.Manager). It
// proxies to code-server on the machine, for admins in person, and only for
// the editor's own pages and links to them, since anything that can talk to
// it runs commands on the machine. When code-server isn't running, it
// answers a page with a button that starts it. It returns nil if Bield has
// no editors.
func (h *handler) editorPort(accessID string) http.Handler {
	eds := h.opts.Editors
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return eds.Dial(ctx, accessID)
		},
		IdleConnTimeout:     editorIdleConns,
		MaxIdleConnsPerHost: 16,
		DisableCompression:  true,
	}
	h.editorTransports.Store(accessID, transport)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = "http", "code-server"
			// code-server checks a WebSocket's Origin against the Host.
			pr.Out.Host = pr.In.Host
			pr.Out.Header.Del("Authorization")
			// Cookies go to every port of a host, so another machine's editor
			// can set one that this one would get. code-server only reads its
			// login cookie, which --auth none never uses.
			pr.Out.Header.Del("Cookie")
			// A page comes uncompressed, so loadingStyle can go in it. It's a
			// few KB: what's large is the scripts it loads.
			if opensPage(pr.In) {
				pr.Out.Header.Set("Accept-Encoding", "identity")
			}
		},
		Transport: transport,
		ModifyResponse: func(res *http.Response) error {
			res.Header.Del("Set-Cookie")
			editorHeaders(res.Header)
			return addLoadingStyle(res)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			editorHeaders(w.Header())
			h.editorUnreachable(w, r, accessID, err)
		},
		ErrorLog: slog.NewLogLogger(h.log.Handler(), slog.LevelDebug),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		editorHeaders(w.Header())
		snap := h.opts.Snapshot()
		if snap.Status == nil {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Bield is starting", http.StatusServiceUnavailable)
			return
		}
		self := snap.Status.Self
		ed, ok := eds.Editor(accessID)
		if !ok {
			http.Error(w, "VS Code isn't installed on this machine any more", http.StatusNotFound)
			return
		}
		if !ownHost(self, r.Host) || portOf(r.Host) != ed.Port {
			http.Error(w, "Bield only answers to its own name on the tailnet", http.StatusMisdirectedRequest)
			return
		}
		origin := schemeOf(r) + "://" + r.Host
		if why := editorCrossSite(r, origin); why != "" {
			http.Error(w, why, http.StatusForbidden)
			return
		}
		if r.Header.Get("Authorization") != "" {
			// A token isn't a person at a keyboard.
			http.Error(w, "VS Code is for people: an API token can't open it", http.StatusForbidden)
			return
		}
		c := h.identify(r.Context(), r.RemoteAddr, self.ID)
		switch {
		case c.reason == ReasonRoleLookupFailed:
			h.editorPage(w, r, http.StatusServiceUnavailable, editorPageData{
				Title: "Bield couldn't check your role", Message: "Try again in a moment.",
			})
			return
		case c.level < admins:
			msg := "Only Bield's admins can open VS Code, since it runs commands on the machine."
			if c.who == nil {
				msg += " Tailscale didn't say who you are."
			}
			h.editorPage(w, r, http.StatusForbidden, editorPageData{Title: "You can't open this editor", Message: msg})
			return
		}
		if strings.HasPrefix(r.URL.Path, editorAction) {
			h.editorPortAction(w, r, accessID)
			return
		}
		if isWebSocket(r) {
			w = &trackingWriter{ResponseWriter: w, track: func(conn net.Conn) net.Conn {
				return h.editorSessions.add(accessID, c, conn)
			}}
		}
		// The proxy adds code-server's headers to these, and ModifyResponse
		// puts Bield's back on them, so each is there once.
		for _, k := range editorHeaderNames {
			w.Header().Del(k)
		}
		proxy.ServeHTTP(w, r)
	})
}

// dropEditorConns closes every connection Bield has to the editor of
// accessID, as when it stops: the admins' WebSockets, and the connections
// the proxy keeps for the next request.
func (h *handler) dropEditorConns(accessID string) {
	h.editorSessions.closeDevice(accessID)
	if v, ok := h.editorTransports.Load(accessID); ok {
		if tr, ok := v.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

// editorPortAction answers Bield's own routes on an editor's port. POST
// /__bield/start starts code-server, from the page that says it isn't
// running.
func (h *handler) editorPortAction(w http.ResponseWriter, r *http.Request, accessID string) {
	if r.URL.Path != editorAction+"start" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(CSRFHeader) == "" {
		http.Error(w, "requests that change things need the "+CSRFHeader+" header", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), editorStartTimeout)
	defer cancel()
	if err := h.opts.Editors.Start(ctx, h.opts.Editors.DeviceID(accessID)); err != nil {
		h.editorError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// editorUnreachable answers a request that the proxy couldn't take to
// code-server: with the page that starts it if it isn't running, and with
// why Bield couldn't reach the machine otherwise.
func (h *handler) editorUnreachable(w http.ResponseWriter, r *http.Request, accessID string, err error) {
	if r.Context().Err() != nil {
		// The browser went away.
		return
	}
	deviceID := h.opts.Editors.DeviceID(accessID)
	data := editorPageData{Device: h.deviceName(r, deviceID), DeviceURL: h.devicePageURL(deviceID)}
	if _, ok := errors.AsType[*ssh.SocketError](err); ok {
		data.Title = "VS Code isn't running on " + data.Device
		data.Message = "It stops by itself when no one has had it open for a while."
		data.Start = true
		h.editorPage(w, r, http.StatusServiceUnavailable, data)
		return
	}
	h.log.Info("couldn't reach an editor", "device", deviceID, "error", err)
	data.Title = "Bield can't reach " + data.Device
	data.Message = editorMessage(err) + "."
	h.editorPage(w, r, http.StatusBadGateway, data)
}

// devicePageURL returns the address of a device's page in Bield.
func (h *handler) devicePageURL(deviceID string) string {
	base := h.opts.AppURL
	if base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/devices/" + url.PathEscape(deviceID)
}

// editorHeaders sets the headers every answer from an editor's port has.
// code-server frames its own pages, for webviews, so only it may frame them.
// Its own Content-Security-Policy stays, and this one is added to it.
func editorHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Add("Content-Security-Policy", editorFramePolicy)
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

const editorFramePolicy = "frame-ancestors 'self'"

// editorHeaderNames are the headers editorHeaders sets.
var editorHeaderNames = []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy", "Cross-Origin-Resource-Policy"}

// startupFallback is the element of VS Code's page that says the editor
// failed to load. VS Code's script removes it as soon as it runs, and the
// page shows it after 10 seconds if it hasn't run yet.
const startupFallback = `<div id="vscode-startup-fallback">`

// loadingStyle goes in VS Code's page. The script the page waits for is
// about 6 MB compressed, which takes longer than 10 seconds the first time
// over a relayed connection, so VS Code said it had failed while it was
// still loading. This says it's loading, and keeps the failure for two
// minutes. code-server's policy allows inline styles. If it stopped, the
// page would be VS Code's own again.
const loadingStyle = `<style>
#vscode-startup-fallback{visibility:visible;animation:none}
#vscode-startup-fallback>*{visibility:hidden;animation:bield-failed 0s linear 120s forwards}
#vscode-startup-fallback::before{content:"Loading VS Code\2026\AThe first time takes longest. Your browser keeps it for the next.";white-space:pre-line;font-size:13px;line-height:1.6;margin-bottom:16px;opacity:.85;animation:bield-gone 0s linear 120s forwards}
@keyframes bield-failed{to{visibility:visible}}
@keyframes bield-gone{to{visibility:hidden;font-size:0;margin:0}}
</style>
`

// readCloser reads from one reader and closes another.
type readCloser struct {
	io.Reader
	io.Closer
}

// maxEditorPage is the largest page addLoadingStyle reads. VS Code's is
// about 5 KB.
const maxEditorPage = 1 << 20

// addLoadingStyle puts loadingStyle in res if it's VS Code's page: a whole
// HTML page, uncompressed, with startupFallback in it. Anything else passes
// as it came.
func addLoadingStyle(res *http.Response) error {
	ct, _, _ := strings.Cut(res.Header.Get("Content-Type"), ";")
	if res.StatusCode != http.StatusOK || !strings.EqualFold(strings.TrimSpace(ct), "text/html") ||
		res.Header.Get("Content-Encoding") != "" || res.ContentLength > maxEditorPage {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxEditorPage+1))
	if err != nil {
		return err
	}
	if len(body) > maxEditorPage {
		// Too big to be VS Code's page: it goes on whole, as it came.
		res.Body = readCloser{io.MultiReader(bytes.NewReader(body), res.Body), res.Body}
		return nil
	}
	_ = res.Body.Close()
	page := string(body)
	head := strings.Index(page, "</head>")
	if head >= 0 && strings.Contains(page, startupFallback) {
		page = page[:head] + loadingStyle + page[head:]
		res.Header.Del("ETag")
	}
	res.Body = io.NopCloser(strings.NewReader(page))
	res.ContentLength = int64(len(page))
	res.Header.Set("Content-Length", strconv.Itoa(len(page)))
	return nil
}

// editorCrossSite says why a request to an editor's port mustn't be served,
// or "". The editor is its own origin, and whatever talks to it can run
// commands on the machine, so only its own pages may use it. A page
// elsewhere, even on Bield or on another machine of the tailnet, can only
// link to it. Browsers say where a request comes from in Sec-Fetch-Site, and
// in Origin for a WebSocket, which they can't leave out.
func editorCrossSite(r *http.Request, origin string) string {
	if isWebSocket(r) {
		if !strings.EqualFold(r.Header.Get("Origin"), origin) {
			return "WebSockets to this editor must come from its own pages"
		}
		return ""
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return ""
	case "":
		// A browser that doesn't say, or not a browser. One that says where
		// a change came from must say it came from here.
		if o := r.Header.Get("Origin"); !safeMethod(r.Method) && o != "" && !strings.EqualFold(o, origin) {
			return "another site can't use this editor"
		}
		return ""
	}
	if safeMethod(r.Method) && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document" {
		// A link to the editor, opened in a tab of its own.
		return ""
	}
	return "another site can't use this editor"
}

// portOf returns the port in a Host header, with the default of its
// scheme if there's none.
func portOf(hostport string) int {
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// editorPageData is what a page of Bield's on an editor's port says.
type editorPageData struct {
	Title, Message string
	// Device is the device's name, and DeviceURL its page in Bield.
	Device, DeviceURL string
	// Start shows the button that starts code-server.
	Start bool
}

// editorStyle and editorScript are the page's, which its policy allows by
// their hashes.
const (
	editorStyle = `body{font:15px/1.5 system-ui,sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#f6f6f4;color:#1d1d1b}` +
		`@media (prefers-color-scheme:dark){body{background:#151514;color:#ececea}a{color:#9ec5ff}}` +
		`main{max-width:32rem;padding:2rem 1.5rem}h1{font-size:1.25rem;margin:0 0 .5rem}p{margin:.5rem 0}` +
		`button{font:inherit;padding:.5rem 1rem;margin:.75rem 0;border-radius:.375rem;border:1px solid #2563eb;background:#2563eb;color:#fff;cursor:pointer}` +
		`button:disabled{opacity:.6;cursor:wait}#msg:empty{display:none}`
	editorScript = `const b=document.getElementById("start"),m=document.getElementById("msg");` +
		`b&&b.addEventListener("click",async()=>{b.disabled=true;m.textContent="Starting VS Code…";` +
		`try{const r=await fetch("/__bield/start",{method:"POST",headers:{"X-Bield":"1"}});` +
		`if(r.ok){location.reload();return}const j=await r.json().catch(()=>({}));` +
		`m.textContent=j.error||("Bield answered "+r.status)}catch(e){m.textContent="Bield couldn't be reached."}` +
		`b.disabled=false});`
)

var (
	editorTemplate = template.Must(template.New("editor").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title><style>` + editorStyle + `</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Message}}</p>
{{if .Start}}<button id="start" type="button">Start VS Code</button>{{end}}
<p id="msg" role="status"></p>
{{if .DeviceURL}}<p><a href="{{.DeviceURL}}">{{if .Device}}{{.Device}} in Bield{{else}}Back to Bield{{end}}</a></p>{{end}}
</main><script>` + editorScript + `</script></body></html>`))

	editorPagePolicy = "default-src 'none'; script-src " + cspHash(editorScript) + "; style-src " + cspHash(editorStyle) +
		"; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// editorPage answers with one of Bield's pages on an editor's port, or with
// its message as JSON for anything but a browser opening a page.
func (h *handler) editorPage(w http.ResponseWriter, r *http.Request, status int, data editorPageData) {
	if !opensPage(r) {
		msg := data.Title
		if data.Message != "" {
			msg += ": " + data.Message
		}
		h.writeJSON(w, r, status, ErrorBody{Error: msg})
		return
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", editorPagePolicy)
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Cache-Control", "no-store")
	hd.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := editorTemplate.Execute(w, data); err != nil {
		h.log.Debug("write an editor's page", "error", err)
	}
}

// opensPage reports whether r is a browser opening a page, rather than a
// request a page makes.
func opensPage(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return safeMethod(r.Method) && strings.Contains(r.Header.Get("Accept"), "text/html")
}

// trackingWriter hands the connection of a WebSocket that the proxy takes
// over to track, which returns the connection the proxy uses instead.
type trackingWriter struct {
	http.ResponseWriter
	track func(net.Conn) net.Conn
}

func (w *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	return w.track(conn), brw, nil
}

func (w *trackingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// editorCloseGrace is how long an admin can be gone from an editor before
// Bield logs that they closed it, so a reload isn't a close and an open.
const editorCloseGrace = time.Minute

// editorSessions are the WebSockets that admins have open to editors. Bield
// logs when an admin opens an editor and when they close it, closes the
// WebSockets when an editor stops or an admin loses their role, and says who
// has an editor open. VS Code opens several WebSockets for each window.
type editorSessions struct {
	mu    sync.Mutex
	conns map[*editorConn]struct{}
	open  map[editorKey]*editorSession
	// log writes a line in the activity log about an admin and an editor.
	log func(k editorKey, opened bool)
}

type editorKey struct{ accessID, login string }

type editorSession struct {
	conns int
	// closing logs the close when it fires, unless the admin connects
	// again first.
	closing *time.Timer
}

// editorConn is a WebSocket an admin has open to an editor.
type editorConn struct {
	net.Conn
	key    editorKey
	caller caller
	s      *editorSessions
	once   sync.Once
}

func (c *editorConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.s.remove(c) })
	return err
}

// add tracks conn, a WebSocket that c opened to the editor of accessID,
// and returns the connection to use in its place.
func (s *editorSessions) add(accessID string, c caller, conn net.Conn) net.Conn {
	k := editorKey{accessID: accessID, login: c.login()}
	ec := &editorConn{Conn: conn, key: k, caller: c, s: s}
	s.mu.Lock()
	if s.conns == nil {
		s.conns, s.open = map[*editorConn]struct{}{}, map[editorKey]*editorSession{}
	}
	s.conns[ec] = struct{}{}
	sess := s.open[k]
	opened := sess == nil
	switch {
	case opened:
		sess = &editorSession{}
		s.open[k] = sess
	case sess.closing != nil:
		sess.closing.Stop()
		sess.closing = nil
	}
	sess.conns++
	s.mu.Unlock()
	if opened {
		// Before the WebSocket is used, so the open is logged before its close.
		s.log(k, true)
	}
	return ec
}

func (s *editorSessions) remove(c *editorConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
	sess := s.open[c.key]
	if sess == nil {
		return
	}
	if sess.conns--; sess.conns > 0 {
		return
	}
	sess.closing = time.AfterFunc(editorCloseGrace, func() {
		s.mu.Lock()
		if s.open[c.key] != sess || sess.conns > 0 {
			s.mu.Unlock()
			return
		}
		delete(s.open, c.key)
		s.mu.Unlock()
		s.log(c.key, false)
	})
}

// closeWhere closes the WebSockets for which match is true.
func (s *editorSessions) closeWhere(match func(*editorConn) bool) {
	s.mu.Lock()
	var doomed []*editorConn
	for c := range s.conns {
		if match(c) {
			doomed = append(doomed, c)
		}
	}
	s.mu.Unlock()
	for _, c := range doomed {
		_ = c.Close()
	}
}

// closeDevice closes the WebSockets to the editor of accessID.
func (s *editorSessions) closeDevice(accessID string) {
	s.closeWhere(func(c *editorConn) bool { return c.key.accessID == accessID })
}

// openBy returns the logins of the admins who have the editor of accessID
// open, in order.
func (s *editorSessions) openBy(accessID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for k, sess := range s.open {
		if k.accessID == accessID && sess.conns > 0 {
			out = append(out, k.login)
		}
	}
	slices.Sort(out)
	return out
}

// callers returns one caller for each admin who has an editor open.
func (s *editorSessions) callers() []caller {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []caller
	for c := range s.conns {
		if login := c.caller.login(); !seen[login] {
			seen[login] = true
			out = append(out, c.caller)
		}
	}
	return out
}

// stop closes every WebSocket, and logs the close of every editor that was
// open, at once, since Bield is stopping.
func (s *editorSessions) stop() {
	s.closeWhere(func(*editorConn) bool { return true })
	s.mu.Lock()
	open := s.open
	s.open = map[editorKey]*editorSession{}
	s.mu.Unlock()
	for k, sess := range open {
		if sess.closing != nil {
			sess.closing.Stop()
		}
		s.log(k, false)
	}
}

// watchEditors keeps the editors' WebSockets in step with roles until
// Bield stops: it closes those of an admin who isn't one any more, as the
// terminal does, since a WebSocket outlives the request that was checked.
// Then it closes them all.
func (h *handler) watchEditors(stopping <-chan struct{}) {
	t := time.NewTicker(roleCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-stopping:
			h.editorSessions.stop()
			return
		case <-t.C:
		}
		for _, c := range h.editorSessions.callers() {
			ctx, cancel := context.WithTimeout(context.Background(), roleCheckInterval)
			l, err := h.levelNow(ctx, c)
			cancel()
			// If the store can't say, the editor stays open, and the next
			// check asks again.
			if err == nil && l < admins {
				login := c.login()
				h.editorSessions.closeWhere(func(ec *editorConn) bool { return ec.caller.login() == login })
			}
		}
	}
}

// logEditorSession writes the line in the activity log for an admin who
// opened or closed an editor.
func (h *handler) logEditorSession(k editorKey, opened bool) {
	deviceID := h.opts.Editors.DeviceID(k.accessID)
	name := h.deviceNameNow(deviceID)
	msg := "Closed VS Code on " + name
	if opened {
		msg = "Opened VS Code on " + name
	}
	h.addEvent(context.Background(), store.Event{Kind: string(EventKindEditor), DeviceID: deviceID, Actor: k.login, Message: msg})
}

// deviceNameNow returns a device's name, as deviceName does, for code that
// has no request.
func (h *handler) deviceNameNow(id string) string {
	ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
	defer cancel()
	if p := h.opts.Feed.latest(ctx); p != nil {
		for _, d := range p.snap.Devices {
			if d.ID == id {
				return d.Name
			}
		}
	}
	return id
}
