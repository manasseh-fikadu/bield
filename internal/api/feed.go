package api

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manasseh-fikadu/bield/internal/agents"
	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/checks"
	"github.com/manasseh-fikadu/bield/internal/docker"
	"github.com/manasseh-fikadu/bield/internal/hostmetrics"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/systemd"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

const (
	// feedGap is the least time between two snapshots. Changes that come
	// closer together share one.
	feedGap = time.Second
	// feedHeartbeat is the longest the feed goes without a new snapshot, so
	// clients can tell a quiet stream from a dead one.
	feedHeartbeat = 15 * time.Second
	// metricsEvery is how often a device's metrics are stored for its
	// history, give or take the time between its samples.
	metricsEvery = 30 * time.Second
	// healthEvery is how often the health score is stored for its history.
	healthEvery = time.Minute
)

// FeedOptions configures NewFeed.
type FeedOptions struct {
	// Version is the hub's build version.
	Version string
	// Demo says the tailnet is made up, which the snapshots say too.
	Demo bool
	// Tailnet returns what Bield last learned about the tailnet. Required.
	Tailnet func() tailnet.Snapshot
	// TailnetChanged receives a value when Tailnet's answer changes.
	TailnetChanged <-chan struct{}
	// Host returns what Bield last measured of its own machine. It's nil if
	// Bield can't measure it.
	Host func() hostmetrics.Reading
	// HostChanged receives a value when Host's answer changes.
	HostChanged <-chan struct{}
	// Agents holds what agents report. Nil means there are none.
	Agents *agents.Registry
	// Checks runs the checks, and keeps the pings of each peer. Nil means
	// there are none.
	Checks *checks.Runner
	// Alerts opens and resolves alerts from what each snapshot shows. Nil
	// means there are none.
	Alerts *alerts.Engine
	// Docker returns what Bield last read of the containers on its machine.
	// It's nil if Bield doesn't read them.
	Docker func() docker.Reading
	// DockerChanged receives a value when Docker's answer changes.
	DockerChanged <-chan struct{}
	// Systemd returns what Bield last read of the systemd units on its
	// machine. It's nil if Bield doesn't read them.
	Systemd func() systemd.Reading
	// SystemdChanged receives a value when Systemd's answer changes.
	SystemdChanged <-chan struct{}
	// SSH says how Bield logs in to each machine, by access ID. It's nil
	// if Bield doesn't.
	SSH func() map[string]ssh.Access
	// SSHChanged receives a value when SSH's answer changes.
	SSHChanged <-chan struct{}
	// Store holds the devices' settings and history. Required.
	Store *store.Store
	// Settings holds the settings admins changed. Nil reads them from Store.
	Settings *settings.Cache
	// Logger receives errors. Nil discards them.
	Logger *slog.Logger
}

// Feed builds the snapshots that GET /api/stream sends: one each time what
// Bield knows changes, rather than one for each client. Each is encoded and
// compressed once, for admins and once for everyone else, and every client
// gets the same bytes.
//
// It also records each ping of a peer, the devices' metrics every
// metricsEvery and the health score every healthEvery, for their history.
type Feed struct {
	opts  FeedOptions
	log   *slog.Logger
	poked chan struct{}

	// buildMu is held while a snapshot is built, and guards the fields up
	// to mu.
	buildMu     sync.Mutex
	zw          *flate.Writer
	settings    map[string]store.DeviceSettings
	settingsErr error
	enrolled    map[string]store.Enrollment
	enrolledErr error
	// pingsUpTo is when the latest recorded ping was sent.
	pingsUpTo time.Time
	pingsErr  error
	// directAt holds when each device last answered a ping directly, by ID,
	// or is nil until it's read from the store.
	directAt    map[string]time.Time
	directAtErr error
	// metricsAt holds when each device's metrics were last recorded, by
	// the ID their history is kept under.
	metricsAt   map[string]time.Time
	metricsErr  error
	healthErr   error
	activity    activity
	eventsErr   error
	lastEventID int64
	// started is when the feed was made, which is about when Bield started.
	started time.Time

	// dirty is set by Poke and cleared when a snapshot is built, so a caller
	// can tell that what they changed isn't in the latest snapshot yet.
	dirty atomic.Bool

	mu  sync.Mutex
	cur *published
}

// published is a snapshot, ready to send.
type published struct {
	// snap is the snapshot as admins see it.
	snap Snapshot
	// admin and viewer are the "snapshot" event for admins and for everyone
	// else.
	admin, viewer *event
	// next is closed when a newer snapshot is published.
	next chan struct{}
}

// NewFeed returns a Feed. It builds a snapshot the first time one is asked
// for, and then each time something changes while Run runs. It panics if
// opts lacks a required field.
func NewFeed(opts FeedOptions) *Feed {
	if opts.Tailnet == nil || opts.Store == nil {
		panic("api.NewFeed: FeedOptions.Tailnet and Store are required")
	}
	f := &Feed{
		opts: opts, log: opts.Logger, poked: make(chan struct{}, 1), metricsAt: map[string]time.Time{},
		started: time.Now(),
	}
	if f.log == nil {
		f.log = slog.New(slog.DiscardHandler)
	}
	if f.opts.Settings == nil {
		f.opts.Settings = settings.NewCache(opts.Store, f.log)
	}
	return f
}

// Run builds a snapshot now, and then each time what Bield knows changes,
// until ctx ends.
func (f *Feed) Run(ctx context.Context) {
	agentsChanged, checksChanged := f.agentsChanged(), f.checksChanged()
	heartbeat := time.NewTimer(feedHeartbeat)
	defer heartbeat.Stop()
	health := time.NewTicker(healthEvery)
	defer health.Stop()
	gap := time.NewTimer(0)
	defer gap.Stop()
	sampleHealth := false
	for first := true; ; first = false {
		builtAt := time.Now()
		// A request that came before Run started may have built the first snapshot
		// already, and building another now would only send it twice.
		var p *published
		if first {
			p = f.latest(ctx)
		} else {
			p = f.publish(ctx)
		}
		if sampleHealth && p != nil {
			f.recordHealth(ctx, p.snap)
			sampleHealth = false
		}
		heartbeat.Reset(feedHeartbeat)
		select {
		case <-ctx.Done():
			return
		case <-f.opts.TailnetChanged:
		case <-f.opts.HostChanged:
		case <-agentsChanged:
		case <-checksChanged:
		case <-f.opts.DockerChanged:
		case <-f.opts.SystemdChanged:
		case <-f.opts.SSHChanged:
		case <-f.poked:
		case <-heartbeat.C:
		case <-health.C:
			sampleHealth = true
		}
		gap.Reset(time.Until(builtAt.Add(feedGap)))
		select {
		case <-ctx.Done():
			return
		case <-gap.C:
		}
		// The next snapshot has every change made while waiting.
		f.drain()
	}
}

// agentsChanged returns the channel that says agents reported, or nil.
func (f *Feed) agentsChanged() <-chan struct{} {
	if f.opts.Agents == nil {
		return nil
	}
	return f.opts.Agents.Changes()
}

// checksChanged returns the channel that says the checks changed, or nil.
func (f *Feed) checksChanged() <-chan struct{} {
	if f.opts.Checks == nil {
		return nil
	}
	return f.opts.Checks.Changes()
}

// measuresHub reports whether Bield measures the machine it runs on.
func (f *Feed) measuresHub() bool { return f.opts.Host != nil }

// drain takes the changes waiting to be heard of.
func (f *Feed) drain() {
	for _, ch := range []<-chan struct{}{
		f.opts.TailnetChanged, f.opts.HostChanged, f.agentsChanged(), f.checksChanged(),
		f.opts.DockerChanged, f.opts.SystemdChanged, f.opts.SSHChanged, f.poked,
	} {
		select {
		case <-ch:
		default:
		}
	}
}

// Poke makes Run build a new snapshot soon, such as after an admin changes a
// device's settings.
func (f *Feed) Poke() {
	f.dirty.Store(true)
	select {
	case f.poked <- struct{}{}:
	default:
	}
}

// fresh returns the latest snapshot, or builds a new one first if something
// was changed since the latest was built. A caller that changed something a
// moment ago, such as the command line, then sees it, instead of waiting for
// the next snapshot, which is up to a second away.
func (f *Feed) fresh(ctx context.Context) *published {
	if f.dirty.Load() {
		if p := f.publish(ctx); p != nil {
			return p
		}
	}
	return f.latest(ctx)
}

// current returns the latest snapshot, or nil if there's none yet. Unlike
// latest, it never builds one.
func (f *Feed) current() *published {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

// latest returns the latest snapshot, building the first one if there's
// none yet. It returns nil if the first can't be encoded.
func (f *Feed) latest(ctx context.Context) *published {
	f.mu.Lock()
	p := f.cur
	f.mu.Unlock()
	if p != nil {
		return p
	}
	// If one is being built, it is the first, so wait for it instead of building
	// another, which every client of the stream would then get as well.
	f.buildMu.Lock()
	f.mu.Lock()
	p = f.cur
	f.mu.Unlock()
	f.buildMu.Unlock()
	if p != nil {
		return p
	}
	return f.publish(ctx)
}

// publish builds a snapshot and makes it the latest, and returns it. If it
// can't encode the snapshot, it keeps the previous one and returns that.
func (f *Feed) publish(ctx context.Context) *published {
	f.buildMu.Lock()
	defer f.buildMu.Unlock()
	// Before anything is read, so a change made while this builds still marks
	// the snapshot as behind.
	f.dirty.Store(false)
	src := sources{version: f.opts.Version, demo: f.opts.Demo, tailnet: f.opts.Tailnet()}
	if f.opts.Host != nil {
		h := f.opts.Host()
		src.host = &h
	}
	if f.opts.Agents != nil {
		src.agents = f.opts.Agents.Reports()
	}
	now := time.Now()
	f.recordPings(ctx, src.tailnet.Pings)
	src.directAt = f.directPaths(ctx)
	f.recordMetrics(ctx, src, now)
	src.settings = f.loadSettings(ctx)
	src.enrolled = f.loadEnrollments(ctx)
	if c := f.opts.Checks; c != nil {
		c.ObservePings(src.tailnet)
		src.checks, src.pings = c.Checks(), c.Pings()
	}
	src.global = f.opts.Settings.Get(ctx)
	if f.opts.Docker != nil {
		r := f.opts.Docker()
		src.docker = &r
	}
	if f.opts.Systemd != nil {
		r := f.opts.Systemd()
		src.systemd = &r
	}
	if f.opts.SSH != nil {
		src.ssh = f.opts.SSH()
	}

	snap, machines := newSnapshot(src, now)
	if e := f.opts.Alerts; e != nil {
		src.open = make(map[string]bool)
		for _, a := range e.Open() {
			src.open[a.Key] = true
		}
		conds, isFresh := alertConditions(snap, machines, src, f.started, now)
		e.Apply(ctx, conds, isFresh, now)
		snap.Alerts = newAlerts(e.Open(), now)
	}
	f.logActivity(ctx, f.activity.observe(snap, src, now))
	snap.LastEventID = f.loadLastEventID(ctx)
	p := &published{snap: snap, next: make(chan struct{})}
	admin, err := f.newEvent("snapshot", snap)
	if err == nil {
		p.admin = admin
		p.viewer, err = f.newEvent("snapshot", redact(snap))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.log.Error("encode a snapshot", "error", err)
		return f.cur
	}
	if f.cur != nil {
		close(f.cur.next)
	}
	f.cur = p
	return p
}

// An event is a server-sent event, ready to write.
type event struct {
	// raw is the event as text.
	raw []byte
	// deflated is raw compressed with DEFLATE on its own, ending on a byte
	// boundary with a sync flush. It refers to nothing before it, so it can
	// follow any other event's deflated bytes in a gzip stream.
	deflated []byte
}

// newEvent encodes v as the data of an event called name.
func (f *Feed) newEvent(name string, v any) (*event, error) {
	// JSON escapes every newline inside strings, so data fits on one line.
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	raw := fmt.Appendf(nil, "event: %s\ndata: %s\n\n", name, data)
	var buf bytes.Buffer
	// Reset forgets everything written before, so the event stands alone.
	if f.zw == nil {
		if f.zw, err = flate.NewWriter(&buf, flate.DefaultCompression); err != nil {
			return nil, err
		}
	} else {
		f.zw.Reset(&buf)
	}
	if _, err := f.zw.Write(raw); err != nil {
		return nil, err
	}
	// Flush, unlike Close, doesn't end the stream with a final block.
	if err := f.zw.Flush(); err != nil {
		return nil, err
	}
	return &event{raw: raw, deflated: buf.Bytes()}, nil
}

// recordPings stores the pings sent since the last ones it stored. A ping
// that failed on Bield's side says nothing about the device, so it's left
// out.
func (f *Feed) recordPings(ctx context.Context, pings map[string]tailnet.PingRecord) {
	var (
		batch []store.Ping
		upTo  = f.pingsUpTo
	)
	for id, rec := range pings {
		if !rec.At.After(f.pingsUpTo) {
			continue
		}
		if rec.At.After(upTo) {
			upTo = rec.At
		}
		p := store.Ping{DeviceID: id, At: rec.At}
		switch {
		case rec.Result != nil:
			p.Replied, p.Direct, p.RTT = true, !rec.Result.Relayed(), rec.Result.Latency
		case !errors.Is(rec.Err, tailnet.ErrNoReply):
			continue
		}
		batch = append(batch, p)
	}
	if len(batch) == 0 {
		f.pingsUpTo = upTo
		return
	}
	if err := f.opts.Store.AddPings(ctx, batch); err != nil {
		if ctx.Err() == nil && f.pingsErr == nil {
			f.log.Warn("record pings", "error", err)
		}
		f.pingsErr = err
		return
	}
	if f.pingsErr != nil {
		f.log.Info("recorded pings again")
	}
	f.pingsUpTo, f.pingsErr = upTo, nil
	if f.directAt != nil {
		for _, p := range batch {
			if p.Direct && p.At.After(f.directAt[p.DeviceID]) {
				f.directAt[p.DeviceID] = p.At
			}
		}
	}
}

// directPaths returns when each device last answered a ping directly, by ID.
// It reads them from the store the first time, and recordPings keeps them up
// to date after that.
func (f *Feed) directPaths(ctx context.Context) map[string]time.Time {
	if f.directAt == nil {
		m, err := f.opts.Store.DirectPaths(ctx)
		if err != nil {
			if ctx.Err() == nil && f.directAtErr == nil {
				f.log.Warn("read when devices were last direct", "error", err)
			}
			f.directAtErr = err
			return nil
		}
		f.directAt, f.directAtErr = m, nil
	}
	return f.directAt
}

// recordMetrics stores the metrics of each device that Bield measures, if
// they're new since it last stored them metricsEvery ago, or nearly. Metrics
// older than that, such as an agent's from before Bield restarted, aren't
// new.
func (f *Feed) recordMetrics(ctx context.Context, src sources, now time.Time) {
	var rows []store.MetricsRow
	add := func(id string, s hostmetrics.Sample, b *agents.Battery) {
		// Samples come every few seconds, so allow for a little jitter.
		if now.Sub(s.At) > metricsEvery || s.At.Sub(f.metricsAt[id]) < metricsEvery-time.Second {
			return
		}
		rows = append(rows, metricsRow(id, s, b))
	}
	if h := src.host; h != nil && h.Sample != nil {
		add(store.HubID, *h.Sample, nil)
	}
	for id, r := range src.agents {
		add(id, r.Sample, r.Battery)
	}
	if len(rows) == 0 {
		return
	}
	if err := f.opts.Store.AddMetrics(ctx, rows); err != nil {
		if ctx.Err() == nil && f.metricsErr == nil {
			f.log.Warn("record metrics", "error", err)
		}
		f.metricsErr = err
		return
	}
	if f.metricsErr != nil {
		f.log.Info("recorded metrics again")
	}
	f.metricsErr = nil
	for _, r := range rows {
		f.metricsAt[r.DeviceID] = r.At
	}
}

// metricsRow returns a sample of the device whose history is kept under id,
// as the store keeps it.
func metricsRow(id string, s hostmetrics.Sample, b *agents.Battery) store.MetricsRow {
	row := store.MetricsRow{DeviceID: id, At: s.At, CPU: s.CPU, NetRx: s.NetRx, NetTx: s.NetTx, Temp: s.Temp}
	if pct, ok := percent(&Usage{Used: s.Mem.Used, Total: s.Mem.Total}); ok {
		row.Mem = &pct
	}
	if s.Disk != nil {
		if pct, ok := percent(&Usage{Used: s.Disk.Used, Total: s.Disk.Total}); ok {
			row.Disk = &pct
		}
	}
	if s.Load != nil {
		row.Load1 = new(s.Load[0])
	}
	if b != nil {
		row.Battery = new(b.Percent)
	}
	return row
}

// recordHealth stores the health score of snap, if it has one.
func (f *Feed) recordHealth(ctx context.Context, snap Snapshot) {
	h := snap.Health
	score, ok := scoreOf(h)
	if !ok {
		return
	}
	err := f.opts.Store.AddHealth(ctx, store.HealthSample{
		At: snap.GeneratedAt, Score: score, Passing: h.Passing, Failing: h.Failing, Unavailable: h.Unavailable,
	})
	switch {
	case err != nil && ctx.Err() == nil && f.healthErr == nil:
		f.log.Warn("record the health score", "error", err)
	case err == nil && f.healthErr != nil:
		f.log.Info("recorded the health score again")
	}
	f.healthErr = err
}

// logActivity adds events to the activity log.
func (f *Feed) logActivity(ctx context.Context, events []store.Event) {
	if len(events) == 0 {
		return
	}
	if err := f.opts.Store.AddEvents(ctx, events...); err != nil && ctx.Err() == nil {
		f.log.Warn("add to the activity log", "error", err)
	}
}

// loadLastEventID returns the ID of the activity log's latest event, or the
// last one it read if the store can't say.
func (f *Feed) loadLastEventID(ctx context.Context) int64 {
	id, err := f.opts.Store.LastEventID(ctx)
	switch {
	case err != nil && ctx.Err() == nil && f.eventsErr == nil:
		f.log.Warn("read the activity log", "error", err)
	case err == nil && f.eventsErr != nil:
		f.log.Info("read the activity log again")
	}
	f.eventsErr = err
	if err == nil {
		f.lastEventID = id
	}
	return f.lastEventID
}

// loadSettings returns the devices' settings, or the last ones it read if
// the store can't say.
func (f *Feed) loadSettings(ctx context.Context) map[string]store.DeviceSettings {
	settings, err := f.opts.Store.DeviceSettings(ctx)
	if err != nil {
		if ctx.Err() == nil && f.settingsErr == nil {
			f.log.Warn("read device settings", "error", err)
		}
		f.settingsErr = err
		return f.settings
	}
	if f.settingsErr != nil {
		f.log.Info("read device settings again")
	}
	f.settings, f.settingsErr = settings, nil
	return settings
}

// loadEnrollments returns the agents' enrollments, or the last ones it read
// if the store can't say.
func (f *Feed) loadEnrollments(ctx context.Context) map[string]store.Enrollment {
	enrolled, err := f.opts.Store.Enrollments(ctx)
	if err != nil {
		if ctx.Err() == nil && f.enrolledErr == nil {
			f.log.Warn("read the agents' enrollments", "error", err)
		}
		f.enrolledErr = err
		return f.enrolled
	}
	if f.enrolledErr != nil {
		f.log.Info("read the agents' enrollments again")
	}
	f.enrolled, f.enrolledErr = enrolled, nil
	return enrolled
}
