package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/manasseh-fikadu/bield/internal/coding"
	"github.com/manasseh-fikadu/bield/internal/store"
)

const (
	// maxCodingReport is the largest report of coding sessions Bield reads. The
	// agent's are about 100 kilobytes at most.
	maxCodingReport = 1 << 20
	// codingActiveWithin is how recently a session did something for it to be
	// active now.
	codingActiveWithin = 5 * time.Minute
	// sessionsListed is how many sessions a summary lists.
	sessionsListed = 50
	// sessionsRead is how many sessions Bield reads the names of to list them.
	sessionsRead = 5000
)

// codingFlag is what a heartbeat's reply tells the agent about its coding
// tools: whether to watch them.
type codingFlag struct {
	Enabled bool `json:"enabled"`
}

// heartbeatReply is what Bield answers a heartbeat with when the device's
// coding tools are watched. If they're not, it answers 204, as it always did.
type heartbeatReply struct {
	Coding codingFlag `json:"coding"`
}

// agentRefusal is the answer to an agent that Bield turns away, with the one
// thing it can still tell it.
type agentRefusal struct {
	Error  string      `json:"error"`
	Coding *codingFlag `json:"coding,omitempty"`
}

// codingEnabled reports whether a device's coding tools are watched. If Bield
// can't tell, it says they aren't, and logs why.
func (h *handler) codingEnabled(r *http.Request, historyID string) bool {
	on, err := h.opts.Store.CodingEnabled(r.Context(), historyID)
	if err != nil {
		h.log.Error("read whether coding tools are watched", "device", historyID, "error", err)
		return false
	}
	return on
}

// codingSessions answers POST /api/agent/sessions, which an agent sends about
// once a minute while its device's coding tools are watched, with what they
// used. It's for the machine Bield runs on too, which its heartbeats are
// refused for.
func (h *handler) codingSessions(w http.ResponseWriter, r *http.Request) {
	_, historyID, ok := h.deviceByID(callerOf(r).node)
	if !ok {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: notOnTailnet + "; it tries again later"})
		return
	}
	if !h.codingEnabled(r, historyID) {
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: "an admin hasn't turned on watching coding tools for this device, so Bield ignores the report"})
		return
	}
	var rep coding.Report
	// A newer agent can send fields this version doesn't know.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCodingReport)).Decode(&rep); err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "can't read the report: " + err.Error()})
		return
	}
	now := time.Now()
	keep := time.Duration(h.opts.Settings.Get(r.Context()).Retention.CodingDays) * 24 * time.Hour
	clean, err := rep.Clean(now, now.Add(-keep))
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	// As with a heartbeat, a revoke that lands while the report is read must
	// not leave a report from an agent that has no enrollment.
	h.agentsMu.RLock()
	defer h.agentsMu.RUnlock()
	switch _, err := h.opts.Store.EnrolledNode(r.Context(), bearer(r)); {
	case errors.Is(err, store.ErrUnknownSecret):
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeJSON(w, r, http.StatusUnauthorized, ErrorBody{Error: unknownSecret})
		return
	case err != nil:
		h.fail(w, r, "look up an agent's secret", err)
		return
	}
	if err := h.opts.Store.SaveCoding(r.Context(), historyID, clean, now.Add(-keep)); err != nil {
		h.fail(w, r, "save what a device's coding tools used", err)
		return
	}
	h.log.Debug("took a report of coding tools", "device", historyID,
		"usage", len(clean.Usage), "sessions", len(clean.Sessions), "limits", len(clean.Limits))
	w.WriteHeader(http.StatusNoContent)
}

// setDeviceCoding answers PUT /api/devices/{id}/coding, which turns watching a
// device's coding tools on or off.
func (h *handler) setDeviceCoding(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var body SetCoding
	if !h.readJSON(w, r, &body) {
		return
	}
	if err := h.opts.Store.SetCoding(r.Context(), historyID, body.Enabled); err != nil {
		h.fail(w, r, "change whether coding tools are watched", err)
		return
	}
	verb := "Turned off"
	if body.Enabled {
		verb = "Turned on"
	}
	h.logAction(r, EventKindDevice, d.ID, verb+" watching the coding tools on "+h.deviceName(r, d.ID))
	h.writeJSON(w, r, http.StatusOK, body)
}

// deviceCoding answers GET /api/devices/{id}/coding?range=.
func (h *handler) deviceCoding(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	span, ok := h.codingSpan(w, r)
	if !ok {
		return
	}
	in, err := h.codingInput(r, historyID, span)
	if err != nil {
		h.fail(w, r, "read what a device's coding tools used", err)
		return
	}
	in.deviceOf = func(string) string { return d.ID }
	out := summarizeCoding(in)
	out.Enabled = in.enabled[historyID]
	h.writeJSON(w, r, http.StatusOK, out)
}

// fleetCoding answers GET /api/coding?range=.
func (h *handler) fleetCoding(w http.ResponseWriter, r *http.Request) {
	span, ok := h.codingSpan(w, r)
	if !ok {
		return
	}
	in, err := h.codingInput(r, "", span)
	if err != nil {
		h.fail(w, r, "read what coding tools used", err)
		return
	}
	in.fleet = true
	ids := h.deviceIDs()
	in.deviceOf = func(key string) string { return cmp.Or(ids[key], key) }
	h.writeJSON(w, r, http.StatusOK, summarizeCoding(in))
}

// codingSpan reads the range and time zone of a request, or answers with an
// error and returns false.
func (h *handler) codingSpan(w http.ResponseWriter, r *http.Request) (codingSpan, bool) {
	q := r.URL.Query()
	span, ok := newCodingSpan(cmp.Or(q.Get("range"), "7d"), q.Get("tz"), time.Now())
	if !ok {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "the range must be 24h, 7d, 30d, 90d or 1y"})
	}
	return span, ok
}

// codingInput reads what a summary is worked out from, for one device or for
// all of them if historyID is "".
func (h *handler) codingInput(r *http.Request, historyID string, span codingSpan) (codingInput, error) {
	ctx := r.Context()
	st := h.opts.Store
	in := codingInput{span: span, now: time.Now(), prices: h.opts.Prices, sessionsShown: sessionsListed}
	var err error
	if in.rows, err = st.CodingUsageBetween(ctx, historyID, span.from, span.to); err != nil {
		return in, err
	}
	if in.sessions, err = st.CodingSessionsSince(ctx, historyID, span.from, sessionsRead); err != nil {
		return in, err
	}
	if in.limits, err = st.CodingLimits(ctx, historyID); err != nil {
		return in, err
	}
	if in.tools, err = st.CodingTools(ctx, historyID); err != nil {
		return in, err
	}
	if in.enabled, err = st.CodingDevices(ctx); err != nil {
		return in, err
	}
	return in, nil
}

// deviceIDs maps the keys that history is kept under to the IDs of the
// devices on the tailnet, for the one machine that has two nodes the first
// wins.
func (h *handler) deviceIDs() map[string]string {
	st := h.opts.Snapshot().Status
	ids := map[string]string{}
	if st == nil {
		return ids
	}
	add := func(key, id string) {
		if _, ok := ids[key]; !ok {
			ids[key] = id
		}
	}
	add(historyID(st.Self, true), st.Self.ID)
	for _, d := range st.Peers {
		add(historyID(d, false), d.ID)
	}
	return ids
}
