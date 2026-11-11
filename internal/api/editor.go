package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/editor"
	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/store"
)

const (
	// editorStatusTimeout is how long Bield waits for a device to say what
	// it has of code-server.
	editorStatusTimeout = 20 * time.Second
	// editorInstallTimeout is how long an install can take. It downloads
	// about 230 MB, and runs in the background.
	editorInstallTimeout = 15 * time.Minute
	// editorStartTimeout is how long starting code-server can take: the
	// script waits up to 30 seconds for its socket.
	editorStartTimeout = time.Minute
)

// errNoEditors is the answer of every editor route when Bield runs without
// editors, as in tests that don't give it a Manager.
var errNoEditors = errors.New("this Bield doesn't serve editors")

// deviceEditor answers GET /api/devices/{id}/editor with what the device has
// of VS Code. It asks the device over SSH.
func (h *handler) deviceEditor(w http.ResponseWriter, r *http.Request) {
	d, _, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if h.opts.Demo {
		h.writeJSON(w, r, http.StatusOK, DeviceEditor{
			State: EditorStateUnavailable, Latest: editor.Pinned().Version, OpenBy: []string{},
			Reason: "VS Code is turned off in this demo, since its machines are made up. Run Bield on your own tailnet to use it.",
		})
		return
	}
	eds := h.opts.Editors
	if eds == nil {
		h.writeJSON(w, r, http.StatusServiceUnavailable, ErrorBody{Error: errNoEditors.Error()})
		return
	}
	self := h.opts.Snapshot().Status.Self.ID == d.ID
	rel := eds.Release()
	out := DeviceEditor{State: EditorStateUnavailable, Latest: rel.Version, OpenBy: []string{}}
	if !ssh.Supported(d, self) {
		out.Reason = "Bield can't log in to this device over SSH, which VS Code needs"
		h.writeJSON(w, r, http.StatusOK, out)
		return
	}
	accessID := ssh.AccessID(d, self)
	out.OpenBy = h.editorSessions.openBy(accessID)
	out.Install = h.editorJobs.get(accessID)
	ed, installed := eds.Editor(accessID)
	if installed {
		out.State, out.Version, out.Port = EditorStateInstalled, ed.Version, ed.Port
		out.URL, out.Secure = ed.URL, strings.HasPrefix(ed.URL, "https://")
		if ed.ListenErr != nil {
			out.ListenError = ed.ListenErr.Error()
		}
	}
	if h.sshOf(d.ID).State == SSHStateNotSetUp {
		msg := "Set up SSH access to this device first: Bield installs and runs VS Code over SSH"
		if installed {
			out.Error = msg
		} else {
			out.Reason = msg
		}
		h.writeJSON(w, r, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), editorStatusTimeout)
	defer cancel()
	st, err := eds.Status(ctx, d.ID)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		msg := "Bield couldn't ask the device what it has: " + editorMessage(err)
		if installed {
			out.Error = msg
		} else {
			out.Reason = msg
		}
		h.writeJSON(w, r, http.StatusOK, out)
		return
	}
	out.Home = st.Home
	if st.Free >= 0 {
		out.Free = &st.Free
	}
	switch {
	case installed:
		out.Running = st.Running
		if st.Version == "" {
			out.State = EditorStateMissing
		} else {
			out.Version = st.Version
		}
	default:
		asset, err := eds.Asset(st)
		if err != nil {
			out.Reason = err.Error()
			break
		}
		out.State, out.Download, out.Disk = EditorStateNotInstalled, asset.Size, asset.Size+editor.Unpacked
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// installEditor answers POST /api/devices/{id}/editor/install, which
// starts installing code-server on the device over SSH, or updating it to
// the version Bield pins, and serves its editor. It answers 202 at once:
// the install downloads about 230 MB, and goes on in the background, which
// GET /api/devices/{id}/editor reports on. A running editor stops first.
func (h *handler) installEditor(w http.ResponseWriter, r *http.Request) {
	d, accessID, ok := h.editorDevice(w, r)
	if !ok {
		return
	}
	if h.sshOf(d).State == SSHStateNotSetUp {
		h.sshError(w, r, ssh.ErrNotSetUp)
		return
	}
	who := callerOf(r).login()
	job, ok := h.editorJobs.start(accessID, who, h.opts.Editors.Release().Version)
	if !ok {
		h.editorError(w, r, editor.ErrBusy)
		return
	}
	// The install, and the activity log's line of who did it and how, go on
	// after the request is over.
	logCtx := context.WithoutCancel(r.Context())
	// The machine Bield runs on is two devices in embedded mode, and the
	// activity log names its editor one way, as opening it does.
	logID := h.opts.Editors.DeviceID(accessID)
	name := h.deviceName(r, logID)
	go func() {
		ctx, cancel := context.WithTimeout(logCtx, editorInstallTimeout)
		defer cancel()
		res, err := h.opts.Editors.Install(ctx, d, who)
		// The script stopped code-server, so the connections to it are dead.
		h.dropEditorConns(accessID)
		e := store.Event{Kind: string(EventKindEditor), DeviceID: logID, Actor: who}
		out := res.Output
		var script *editor.ScriptError
		switch {
		case errors.As(err, &script):
			out = script.Output
		case err != nil:
			out = editorMessage(err)
		}
		h.editorJobs.finish(accessID, err == nil, tail(strings.TrimSpace(out)))
		switch {
		case err != nil:
			e.Level, e.Message = string(EventLevelError), "Installing VS Code on "+name+" failed"
			h.log.Info("couldn't install code-server", "device", d, "error", err)
		case res.Previous != "" && res.Previous != res.Version:
			e.Message = "Updated VS Code on " + name + " from code-server " + res.Previous + " to " + res.Version
		default:
			e.Message = "Installed VS Code (code-server " + res.Version + ") on " + name
		}
		h.addEvent(logCtx, e)
	}()
	h.writeJSON(w, r, http.StatusAccepted, job)
}

// editorJobs are the installs under way, and the last one on each device,
// by access ID.
type editorJobs struct {
	mu   sync.Mutex
	jobs map[string]*EditorInstall
}

// start records an install of version that by started, unless one is under
// way, and returns it.
func (j *editorJobs) start(accessID, by, version string) (EditorInstall, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job := j.jobs[accessID]; job != nil && job.Running {
		return *job, false
	}
	if j.jobs == nil {
		j.jobs = map[string]*EditorInstall{}
	}
	job := &EditorInstall{Running: true, Version: version, By: by, StartedAt: time.Now()}
	j.jobs[accessID] = job
	return *job, true
}

func (j *editorJobs) finish(accessID string, ok bool, output string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job := j.jobs[accessID]; job != nil {
		now := time.Now()
		job.Running, job.OK, job.Output, job.EndedAt = false, ok, output, &now
	}
}

// get returns the install under way on a device, or the last one.
func (j *editorJobs) get(accessID string) *EditorInstall {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job := j.jobs[accessID]; job != nil {
		c := *job
		return &c
	}
	return nil
}

// running reports whether an install is under way on a device.
func (j *editorJobs) running(accessID string) bool {
	job := j.get(accessID)
	return job != nil && job.Running
}

// forget drops the last install on a device, once VS Code is removed.
func (j *editorJobs) forget(accessID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job := j.jobs[accessID]; job != nil && !job.Running {
		delete(j.jobs, accessID)
	}
}

// startEditor answers POST /api/devices/{id}/editor/start, which starts
// code-server on the device unless it runs, and says where to open it.
// Opening it is logged when the browser connects.
func (h *handler) startEditor(w http.ResponseWriter, r *http.Request) {
	d, accessID, ok := h.editorDevice(w, r)
	if !ok {
		return
	}
	ed, installed := h.opts.Editors.Editor(accessID)
	switch {
	case !installed:
		h.editorError(w, r, editor.ErrNotInstalled)
		return
	case ed.URL == "":
		msg := "Bield can't serve this device's editor yet"
		if ed.ListenErr != nil {
			msg += ": " + ed.ListenErr.Error()
		}
		h.writeJSON(w, r, http.StatusServiceUnavailable, ErrorBody{Error: msg})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), editorStartTimeout)
	defer cancel()
	if err := h.opts.Editors.Start(ctx, d); err != nil {
		h.editorError(w, r, err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, EditorOpen{URL: ed.URL})
}

// stopEditor answers POST /api/devices/{id}/editor/stop, which stops
// code-server on the device, and with it every window that has it open.
func (h *handler) stopEditor(w http.ResponseWriter, r *http.Request) {
	d, accessID, ok := h.editorDevice(w, r)
	if !ok {
		return
	}
	if _, installed := h.opts.Editors.Editor(accessID); !installed {
		h.editorError(w, r, editor.ErrNotInstalled)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), editorStartTimeout)
	defer cancel()
	err := h.opts.Editors.Stop(ctx, d)
	h.dropEditorConns(accessID)
	if err != nil {
		h.editorError(w, r, err)
		return
	}
	logID := h.opts.Editors.DeviceID(accessID)
	h.logAction(r, EventKindEditor, logID, "Stopped VS Code on "+h.deviceName(r, logID))
	w.WriteHeader(http.StatusNoContent)
}

// removeEditor answers DELETE /api/devices/{id}/editor, which stops
// code-server on the device and deletes it. With ?keep=data, its settings
// and extensions stay on the device for the next install.
func (h *handler) removeEditor(w http.ResponseWriter, r *http.Request) {
	d, accessID, ok := h.editorDevice(w, r)
	if !ok {
		return
	}
	if h.editorJobs.running(accessID) {
		h.editorError(w, r, editor.ErrBusy)
		return
	}
	keep := r.URL.Query().Get("keep") == "data"
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), editorStartTimeout)
	defer cancel()
	out, err := h.opts.Editors.Remove(ctx, d, keep)
	h.dropEditorConns(accessID)
	var script *editor.ScriptError
	switch {
	case errors.As(err, &script):
		h.writeJSON(w, r, http.StatusOK, EditorRun{Output: tail(strings.TrimSpace(script.Output))})
		return
	case err != nil:
		h.editorError(w, r, err)
		return
	}
	h.editorJobs.forget(accessID)
	logID := h.opts.Editors.DeviceID(accessID)
	msg := "Removed VS Code from " + h.deviceName(r, logID)
	if keep {
		msg += ", and kept its settings and extensions"
	}
	h.logAction(r, EventKindEditor, logID, msg)
	h.writeJSON(w, r, http.StatusOK, EditorRun{OK: true, Output: tail(strings.TrimSpace(out))})
}

// editorDevice returns the ID of the device a request is about, and its
// access ID, or answers the request and returns false.
func (h *handler) editorDevice(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	d, _, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return "", "", false
	}
	if h.opts.Editors == nil {
		h.writeJSON(w, r, http.StatusServiceUnavailable, ErrorBody{Error: errNoEditors.Error()})
		return "", "", false
	}
	self := h.opts.Snapshot().Status.Self.ID == d.ID
	if !ssh.Supported(d, self) {
		h.sshError(w, r, ssh.ErrUnsupported)
		return "", "", false
	}
	return d.ID, ssh.AccessID(d, self), true
}

// editorError answers with the status and message for an error from the
// editor manager or SSH.
func (h *handler) editorError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	var (
		noBuild *editor.NoBuildError
		script  *editor.ScriptError
	)
	switch {
	case errors.Is(err, editor.ErrNotInstalled), errors.Is(err, editor.ErrBusy), errors.Is(err, editor.ErrNoPort),
		errors.Is(err, editor.ErrMusl), errors.As(err, &noBuild):
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: err.Error()})
	case errors.Is(err, editor.ErrStarting):
		h.writeJSON(w, r, http.StatusServiceUnavailable, ErrorBody{Error: err.Error()})
	case errors.As(err, &script):
		h.writeJSON(w, r, http.StatusBadGateway, ErrorBody{Error: script.Output})
	default:
		if _, known := sshStatus(err); known {
			h.sshError(w, r, err)
			return
		}
		h.log.Warn("use a device's editor", "path", r.URL.Path, "error", err)
		h.writeJSON(w, r, http.StatusBadGateway, ErrorBody{Error: editorMessage(err)})
	}
}

// editorMessage says what went wrong, for an admin.
func editorMessage(err error) string {
	var ce *ssh.ConnectError
	switch {
	case errors.As(err, &ce):
		return "Bield couldn't log in: " + ce.Msg
	case errors.Is(err, context.DeadlineExceeded):
		return "the device took too long to answer"
	}
	return err.Error()
}
