// Package editor runs VS Code in the browser on a machine: code-server,
// installed over SSH for the user Bield logs in as, and listening on a Unix
// socket in a folder only that user can open. The hub reaches the socket by
// forwarding it over SSH, so nothing new listens on the machine's network.
//
// Each machine's editor is served on a port of Bield's node of its own (see
// Manager). A port is an origin of its own in a browser, so what the editor
// runs, which comes from the machine and its extensions, can't call Bield's
// API as the admin who opened it, or reach another machine's editor.
package editor

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// Remote runs scripts on machines and opens their Unix sockets, over SSH.
// *ssh.Manager is one.
type Remote interface {
	Exec(ctx context.Context, deviceID, script string, args ...string) (ssh.Result, error)
	DialUnix(ctx context.Context, deviceID, path string) (net.Conn, error)
}

// Ports is the range of ports, both included, that Bield serves editors on.
type Ports struct{ First, Last int }

// DefaultPorts is the range Bield serves editors on unless told otherwise:
// one port for each of 90 machines, next to host mode's 4700.
var DefaultPorts = Ports{First: 4710, Last: 4799}

// Has reports whether port is in the range.
func (p Ports) Has(port int) bool { return port >= p.First && port <= p.Last }

func (p Ports) String() string {
	if p.First == p.Last {
		return strconv.Itoa(p.First)
	}
	return strconv.Itoa(p.First) + "-" + strconv.Itoa(p.Last)
}

// Options configures NewManager.
type Options struct {
	Remote Remote
	Store  *store.Store
	// Tailnet returns the latest snapshot of the tailnet, which says which
	// device an access ID is (see ssh.AccessID).
	Tailnet func() tailnet.Snapshot
	// Listen listens on a port of Bield's node, as tailnet.Node.ListenPort
	// does.
	Listen func(ctx context.Context, port int) (net.Listener, string, error)
	// Ports is where editors are served. The zero value is DefaultPorts.
	Ports Ports
	// IdleTimeout is how long code-server keeps running once no one is
	// connected to it. It's 30 minutes if zero.
	IdleTimeout time.Duration
	// Release is the code-server Bield installs. It's Pinned() if empty.
	Release Release
	Logger  *slog.Logger
}

// DefaultIdleTimeout is how long an editor runs with no one connected.
const DefaultIdleTimeout = 30 * time.Minute

// Errors from the Manager. The ones about a machine say why it can't have an
// editor, in a sentence for an admin.
var (
	ErrStarting     = errors.New("the hub is still starting")
	ErrNotInstalled = errors.New("VS Code isn't installed on this machine")
	ErrBusy         = errors.New("an install or a removal of VS Code is under way on this machine")
	ErrNoPort       = errors.New("every port Bield serves editors on is taken; widen the range with BIELD_EDITOR_PORTS")
	ErrMusl         = errors.New("code-server's builds need glibc, and this machine uses musl, as Alpine does")
)

// A NoBuildError says code-server has no build for a machine.
type NoBuildError struct{ System, Machine string }

func (e *NoBuildError) Error() string {
	return "code-server has no build for " + e.System + " on " + e.Machine + "; it's built for Linux and macOS on x86-64 and ARM64"
}

// A ScriptError is a script on the machine that failed, with what it said.
type ScriptError struct {
	Code   int
	Output string
}

func (e *ScriptError) Error() string {
	if e.Output == "" {
		return "the script on the machine failed with status " + strconv.Itoa(e.Code)
	}
	return e.Output
}

// Manager installs, starts and stops code-server on machines, and serves the
// editors that are installed, each on its port.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// handler serves each editor's port. Nothing listens before Handle sets
	// it, or after Run ends.
	handler func(accessID string) http.Handler
	editors map[string]*served // by access ID
	// sockets are the paths code-server listens on, by access ID, as the
	// machine last said.
	sockets map[string]string
	// busy are the machines an install or a removal is under way on.
	busy map[string]bool
	// reserved are ports an install is trying, which aren't in editors yet.
	reserved map[int]bool
}

// served is an installed editor and the server on its port.
type served struct {
	row store.Editor
	url string
	srv *http.Server
	// err is why Bield couldn't listen on the port, while srv is nil.
	err error
}

// NewManager returns a Manager. Load what the store has, then Run it.
func NewManager(opts Options) *Manager {
	if opts.Ports == (Ports{}) {
		opts.Ports = DefaultPorts
	}
	opts.IdleTimeout = cmp.Or(opts.IdleTimeout, DefaultIdleTimeout)
	if opts.Release.Version == "" {
		opts.Release = Pinned()
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		opts:     opts,
		log:      log,
		editors:  map[string]*served{},
		sockets:  map[string]string{},
		busy:     map[string]bool{},
		reserved: map[int]bool{},
	}
}

// Release returns the code-server release the Manager installs.
func (m *Manager) Release() Release { return m.opts.Release }

// Ports returns the range editors are served on.
func (m *Manager) Ports() Ports { return m.opts.Ports }

// Load reads the editors that are installed from the store.
func (m *Manager) Load(ctx context.Context) error {
	rows, err := m.opts.Store.Editors(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, row := range rows {
		m.editors[id] = &served{row: row}
	}
	return nil
}

// retryListen is how often Run tries again to listen on a port it couldn't.
const retryListen = time.Minute

// Handle sets what serves each editor's port: handler(accessID). Editors
// installed from then on are served, and Run serves the others.
func (m *Manager) Handle(handler func(accessID string) http.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

// Run serves each installed editor's port until ctx ends. Then it closes
// them, and serves no more. A port Bield can't listen on, such as
// one another program has in host mode, is tried again every minute.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	if m.handler == nil {
		m.mu.Unlock()
		panic("editor: Run before Handle")
	}
	m.mu.Unlock()
	m.listenAll(ctx)
	t := time.NewTicker(retryListen)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.handler = nil
			for _, e := range m.editors {
				if e.srv != nil {
					_ = e.srv.Close()
					e.srv, e.url = nil, ""
				}
			}
			m.mu.Unlock()
			return
		case <-t.C:
			m.listenAll(ctx)
		}
	}
}

// listenAll listens on the port of each editor that isn't served yet.
func (m *Manager) listenAll(ctx context.Context) {
	m.mu.Lock()
	var todo []store.Editor
	for _, e := range m.editors {
		if e.srv == nil {
			todo = append(todo, e.row)
		}
	}
	m.mu.Unlock()
	for _, row := range todo {
		ln, url, err := m.opts.Listen(ctx, row.Port)
		m.mu.Lock()
		e, ok := m.editors[row.DeviceID]
		switch {
		case err != nil:
			if ok && e.srv == nil {
				if e.err == nil || e.err.Error() != err.Error() {
					m.log.Warn("listen on an editor's port", "device", row.DeviceID, "port", row.Port, "error", err)
				}
				e.err = err
			}
		case !ok || e.srv != nil || e.row.Port != row.Port || m.handler == nil:
			// It was removed or served meanwhile.
			_ = ln.Close()
		default:
			e.srv, e.url, e.err = m.serveLocked(row.DeviceID, ln), url, nil
		}
		m.mu.Unlock()
	}
}

// serveLocked serves accessID's editor on ln.
func (m *Manager) serveLocked(accessID string, ln net.Listener) *http.Server {
	srv := &http.Server{
		Handler:           m.handler(accessID),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(m.log.Handler(), slog.LevelWarn),
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.log.Warn("serve an editor's port", "device", accessID, "error", err)
		}
	}()
	return srv
}

// Editor is an installed editor.
type Editor struct {
	store.Editor
	// URL is where it's served, or "" while Bield can't listen on its port.
	URL string
	// ListenErr is why Bield can't, then.
	ListenErr error
}

// Editor returns the editor installed on the machine with an access ID, if
// one is.
func (m *Manager) Editor(accessID string) (Editor, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.editors[accessID]
	if !ok {
		return Editor{}, false
	}
	return Editor{Editor: e.row, URL: e.url, ListenErr: e.err}, true
}

// Editors returns every installed editor, by access ID.
func (m *Manager) Editors() map[string]Editor {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Editor, len(m.editors))
	for id, e := range m.editors {
		out[id] = Editor{Editor: e.row, URL: e.url, ListenErr: e.err}
	}
	return out
}

// AccessID returns the ID a device's editor is kept under: its SSH access
// ID. ok is false if the device isn't on the tailnet.
func (m *Manager) AccessID(deviceID string) (string, bool) {
	st := m.opts.Tailnet().Status
	if st == nil {
		return "", false
	}
	if st.Self.ID == deviceID {
		return ssh.AccessID(st.Self, true), true
	}
	for _, d := range st.Peers {
		if d.ID == deviceID {
			return ssh.AccessID(d, false), true
		}
	}
	return "", false
}

// DeviceID returns the ID of the device to log in to for an access ID:
// Bield's own node's for the machine it runs on.
func (m *Manager) DeviceID(accessID string) string {
	if accessID == store.HubID {
		if st := m.opts.Tailnet().Status; st != nil {
			return st.Self.ID
		}
	}
	return accessID
}

// Status says what a device has of code-server.
func (m *Manager) Status(ctx context.Context, deviceID string) (Status, error) {
	res, err := m.opts.Remote.Exec(ctx, deviceID, statusScript)
	if err != nil {
		return Status{}, err
	}
	if res.Code != 0 {
		return Status{}, scriptError(res)
	}
	st := parseStatus(res.Stdout)
	if id, ok := m.AccessID(deviceID); ok && st.Socket != "" {
		m.mu.Lock()
		m.sockets[id] = st.Socket
		m.mu.Unlock()
	}
	return st, nil
}

// Asset returns the build of code-server for a machine with status st, or
// why it has none.
func (m *Manager) Asset(st Status) (Asset, error) {
	if st.Musl {
		return Asset{}, ErrMusl
	}
	a, ok := m.opts.Release.Asset(st.System, st.Machine)
	if !ok {
		return Asset{}, &NoBuildError{System: st.System, Machine: st.Machine}
	}
	return a, nil
}

// Installed is what Install did.
type Installed struct {
	// Version is the version it installed, and Previous the one installed
	// before, or "".
	Version, Previous string
	// Output is what the script said.
	Output string
}

// Install installs the pinned code-server on a device, or updates it, as
// the admin with login by, and serves its editor. The first install picks a
// free port in the range. A running editor stops first.
func (m *Manager) Install(ctx context.Context, deviceID, by string) (Installed, error) {
	accessID, ok := m.AccessID(deviceID)
	if !ok {
		return Installed{}, ssh.ErrNoDevice
	}
	if err := m.claim(accessID); err != nil {
		return Installed{}, err
	}
	defer m.release(accessID)

	st, err := m.Status(ctx, deviceID)
	if err != nil {
		return Installed{}, err
	}
	asset, err := m.Asset(st)
	if err != nil {
		return Installed{}, err
	}
	m.mu.Lock()
	current, had := m.editors[accessID]
	var row store.Editor
	if had {
		row = current.row
	}
	m.mu.Unlock()

	// A new editor gets a port before anything is downloaded, so a full
	// range fails at once.
	var (
		ln  net.Listener
		url string
	)
	if !had {
		if !m.serving() {
			return Installed{}, ErrStarting
		}
		if ln, url, row.Port, err = m.pickPort(ctx); err != nil {
			return Installed{}, err
		}
		defer func() {
			if ln != nil {
				_ = ln.Close()
			}
			m.mu.Lock()
			delete(m.reserved, row.Port)
			m.mu.Unlock()
		}()
		row.DeviceID, row.CreatedAt, row.CreatedBy = accessID, time.Now(), by
	}

	res, err := m.opts.Remote.Exec(ctx, deviceID, installScript, m.opts.Release.Version, asset.URL, asset.SHA256, asset.Dir)
	if err != nil {
		return Installed{}, err
	}
	out := Installed{Version: m.opts.Release.Version, Previous: st.Version, Output: string(res.Stdout)}
	if res.Code != 0 {
		return out, scriptError(res)
	}
	row.Version = m.opts.Release.Version
	if err := m.opts.Store.SetEditor(ctx, row); err != nil {
		return out, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.editors[accessID]; ok {
		e.row = row
		return out, nil
	}
	e := &served{row: row}
	m.editors[accessID] = e
	if m.handler != nil {
		e.srv, e.url = m.serveLocked(accessID, ln), url
		ln = nil
	}
	return out, nil
}

// pickPort listens on the first port in the range that no editor has and
// that's free, and reserves it.
func (m *Manager) pickPort(ctx context.Context) (net.Listener, string, int, error) {
	for port := m.opts.Ports.First; port <= m.opts.Ports.Last; port++ {
		if !m.reserve(port) {
			continue
		}
		ln, url, err := m.opts.Listen(ctx, port)
		if err == nil {
			return ln, url, port, nil
		}
		m.mu.Lock()
		delete(m.reserved, port)
		m.mu.Unlock()
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, "", 0, err
		}
	}
	return nil, "", 0, ErrNoPort
}

// reserve reserves port for an install, unless an editor has it or another
// install reserved it.
func (m *Manager) reserve(port int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reserved[port] {
		return false
	}
	for _, e := range m.editors {
		if e.row.Port == port {
			return false
		}
	}
	m.reserved[port] = true
	return true
}

// serving reports whether Run is serving the editors.
func (m *Manager) serving() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handler != nil
}

func (m *Manager) claim(accessID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[accessID] {
		return ErrBusy
	}
	m.busy[accessID] = true
	return nil
}

func (m *Manager) release(accessID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.busy, accessID)
}

// Start starts code-server on a device, unless it runs already, and waits
// until it listens.
func (m *Manager) Start(ctx context.Context, deviceID string) error {
	accessID, ok := m.AccessID(deviceID)
	if !ok {
		return ssh.ErrNoDevice
	}
	if _, ok := m.Editor(accessID); !ok {
		return ErrNotInstalled
	}
	secs := strconv.Itoa(int(max(m.opts.IdleTimeout, 2*time.Minute) / time.Second))
	res, err := m.opts.Remote.Exec(ctx, deviceID, startScript, secs)
	if err != nil {
		return err
	}
	if res.Code == 3 {
		return ErrNotInstalled
	}
	if res.Code != 0 {
		return scriptError(res)
	}
	if path := socketOf(res.Stdout); path != "" {
		m.mu.Lock()
		m.sockets[accessID] = path
		m.mu.Unlock()
	}
	return nil
}

// Stop stops code-server on a device. Connections to it through Bield end
// as it does.
func (m *Manager) Stop(ctx context.Context, deviceID string) error {
	res, err := m.opts.Remote.Exec(ctx, deviceID, stopScript)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return scriptError(res)
	}
	return nil
}

// Remove stops code-server on a device and deletes it, with its settings
// and extensions unless keepData, and stops serving its editor. It returns
// what the script said.
func (m *Manager) Remove(ctx context.Context, deviceID string, keepData bool) (string, error) {
	accessID, ok := m.AccessID(deviceID)
	if !ok {
		return "", ssh.ErrNoDevice
	}
	if _, ok := m.Editor(accessID); !ok {
		return "", ErrNotInstalled
	}
	if err := m.claim(accessID); err != nil {
		return "", err
	}
	defer m.release(accessID)
	keep := "all"
	if keepData {
		keep = "keep"
	}
	res, err := m.opts.Remote.Exec(ctx, deviceID, removeScript, keep)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return string(res.Stdout), scriptError(res)
	}
	if err := m.Forget(ctx, accessID); err != nil {
		return string(res.Stdout), err
	}
	return string(res.Stdout), nil
}

// Forget stops serving a machine's editor and deletes it from the store,
// leaving the machine alone, as for a machine that's gone for good.
func (m *Manager) Forget(ctx context.Context, accessID string) error {
	if err := m.opts.Store.DeleteEditor(ctx, accessID); err != nil && !errors.Is(err, store.ErrNoEditor) {
		return err
	}
	m.mu.Lock()
	e, ok := m.editors[accessID]
	delete(m.editors, accessID)
	delete(m.sockets, accessID)
	m.mu.Unlock()
	if ok && e.srv != nil {
		_ = e.srv.Close()
	}
	return nil
}

// Dial connects to the editor of the machine with an access ID: code-server's
// socket on the machine, forwarded over SSH. If nothing listens there, the
// error is an *ssh.SocketError.
func (m *Manager) Dial(ctx context.Context, accessID string) (net.Conn, error) {
	deviceID := m.DeviceID(accessID)
	m.mu.Lock()
	path := m.sockets[accessID]
	m.mu.Unlock()
	if path == "" {
		st, err := m.Status(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		if path = st.Socket; path == "" {
			return nil, errors.New("the machine didn't say where code-server listens")
		}
	}
	return m.opts.Remote.DialUnix(ctx, deviceID, path)
}

// scriptError returns the error for a script that failed.
func scriptError(res ssh.Result) error {
	out := string(res.Stdout)
	if len(res.Stderr) > 0 {
		out += string(res.Stderr)
	}
	return &ScriptError{Code: res.Code, Output: trimOutput(out)}
}

// maxOutput is how much of a script's output an error keeps.
const maxOutput = 8 << 10

func trimOutput(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	if len(s) > maxOutput {
		s = "…" + strings.ToValidUTF8(s[len(s)-maxOutput:], "")
	}
	return s
}
