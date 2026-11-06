package api

import (
	"cmp"
	"slices"
	"time"

	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// codingSpan is the time a CodingSummary covers, in whole hours or whole days
// of a time zone.
type codingSpan struct {
	from, to time.Time
	daily    bool
	loc      *time.Location
}

// codingRanges are the ranges GET /api/coding takes, in days. 24h is the
// only one in hours.
var codingRanges = map[string]int{"24h": 0, "7d": 7, "30d": 30, "90d": 90, "1y": 365}

// newCodingSpan works out the span of a range at now, with days in the time
// zone tz. It uses UTC if tz is empty or unknown. ok is false for an unknown
// range. A range in days ends at the end of today, and includes it, so 7d is
// today and the 6 days before.
func newCodingSpan(name, tz string, now time.Time) (codingSpan, bool) {
	days, ok := codingRanges[name]
	if !ok {
		return codingSpan{}, false
	}
	loc := time.UTC
	if l, err := time.LoadLocation(tz); err == nil && tz != "" {
		loc = l
	}
	if days == 0 {
		to := now.UTC().Truncate(time.Hour).Add(time.Hour)
		return codingSpan{from: to.Add(-24 * time.Hour), to: to, loc: loc}, true
	}
	n := now.In(loc)
	to := time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, loc)
	return codingSpan{from: to.AddDate(0, 0, -days), to: to, daily: true, loc: loc}, true
}

// bucket returns the start of the hour or day that t is in.
func (s codingSpan) bucket(t time.Time) time.Time {
	if !s.daily {
		return t.UTC().Truncate(time.Hour)
	}
	l := t.In(s.loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, s.loc)
}

// next returns the start of the bucket after the one that starts at t.
func (s codingSpan) next(t time.Time) time.Time {
	if !s.daily {
		return t.Add(time.Hour)
	}
	return t.In(s.loc).AddDate(0, 0, 1)
}

// codingInput is what a CodingSummary is worked out from.
type codingInput struct {
	rows     []store.CodingUsage
	sessions []store.CodingSession
	limits   []store.CodingLimit
	tools    []store.CodingToolStatus
	// enabled are the history keys of the devices that are watched.
	enabled map[string]bool
	prices  *pricing.Table
	span    codingSpan
	now     time.Time
	// deviceOf is the ID of the device that a history key belongs to.
	deviceOf func(key string) string
	// fleet says the rows are of every device, so the summary lists them.
	fleet bool
	// sessionsShown is how many sessions the summary lists.
	sessionsShown int
}

// tally adds up usage.
type tally struct {
	price     pricing.Price
	tokens    pricing.Tokens
	reasoning int64
	requests  int64
	unpriced  int64
	fastCost  float64
	sessions  map[sessionKey]struct{}
	priced    bool
}

// sessionKey identifies a session, which a tool numbers on its own, on a
// device.
type sessionKey struct{ device, tool, id string }

func (t *tally) add(u store.CodingUsage, tk pricing.Tokens, p pricing.Price) {
	t.price.Add(p)
	t.tokens.Input += tk.Input
	t.tokens.CacheRead += tk.CacheRead
	t.tokens.CacheWrite += tk.CacheWrite
	t.tokens.Output += tk.Output
	t.reasoning += u.Reasoning
	t.requests += u.Requests
	if p.Source == pricing.Unpriced {
		t.unpriced += tokenTotal(tk)
	} else {
		t.priced = true
	}
	if u.Speed == pricing.Fast {
		t.fastCost += p.Total
	}
	if t.sessions == nil {
		t.sessions = map[sessionKey]struct{}{}
	}
	t.sessions[sessionKey{u.DeviceID, u.Tool, u.Session}] = struct{}{}
}

func tokenTotal(tk pricing.Tokens) int64 { return tk.Input + tk.CacheRead + tk.CacheWrite + tk.Output }

func (t *tally) codingTokens() CodingTokens {
	return CodingTokens{
		Input: t.tokens.Input, CacheRead: t.tokens.CacheRead, CacheWrite: t.tokens.CacheWrite,
		Output: t.tokens.Output, Reasoning: t.reasoning, Total: tokenTotal(t.tokens),
	}
}

func (t *tally) totals() CodingTotals {
	return CodingTotals{
		Cost: CodingCost{
			Total: t.price.Total, Input: t.price.Input, CacheRead: t.price.CacheRead,
			CacheWrite: t.price.CacheWrite, Output: t.price.Output,
		},
		Tokens: t.codingTokens(), Sessions: len(t.sessions), Requests: t.requests,
		CacheSavings: t.price.CacheSavings, FastCost: t.fastCost, SpeedPremium: t.price.SpeedPremium,
		UnpricedTokens: t.unpriced,
	}
}

// sessionTally is what one session used.
type sessionTally struct {
	device, tool, id string
	cost             float64
	tokens           pricing.Tokens
	reasoning        int64
	models           map[string]float64
	last             time.Time
}

type modelKey struct{ tool, model string }

// summarizeCoding works out a CodingSummary: it prices every hour of usage,
// and adds it up by tool, model, device, session and time.
func summarizeCoding(in codingInput) CodingSummary {
	var (
		all     tally
		byTool  = map[string]*tally{}
		byModel = map[modelKey]*tally{}
		byDev   = map[string]*tally{}
		series  = map[string]map[int64]*CodingPoint{}
		bySess  = map[sessionKey]*sessionTally{}
	)
	// A few models make up thousands of rows, so each is looked up once.
	type listed struct {
		rates pricing.Rates
		ok    bool
	}
	rates := map[string]listed{}
	for _, u := range in.rows {
		tk := pricing.Tokens{Input: u.Input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
		l, seen := rates[u.Model]
		if !seen {
			l.rates, l.ok = in.prices.Lookup(u.Model)
			rates[u.Model] = l
		}
		p := pricing.PriceAt(l.rates, l.ok, u.Speed, tk, float64(u.CostMicro)/1e6)

		all.add(u, tk, p)
		get(byTool, u.Tool).add(u, tk, p)
		get(byModel, modelKey{u.Tool, u.Model}).add(u, tk, p)
		if in.fleet {
			get(byDev, u.DeviceID).add(u, tk, p)
		}

		start := in.span.bucket(u.Hour).UnixMilli()
		if series[u.Tool] == nil {
			series[u.Tool] = map[int64]*CodingPoint{}
		}
		pt := series[u.Tool][start]
		if pt == nil {
			pt = &CodingPoint{TS: start}
			series[u.Tool][start] = pt
		}
		pt.Cost += p.Total
		pt.Tokens += tokenTotal(tk)

		sk := sessionKey{u.DeviceID, u.Tool, u.Session}
		st := bySess[sk]
		if st == nil {
			st = &sessionTally{device: u.DeviceID, tool: u.Tool, id: u.Session, models: map[string]float64{}}
			bySess[sk] = st
		}
		st.cost += p.Total
		st.tokens.Input += tk.Input
		st.tokens.CacheRead += tk.CacheRead
		st.tokens.CacheWrite += tk.CacheWrite
		st.tokens.Output += tk.Output
		st.reasoning += u.Reasoning
		st.models[u.Model] += p.Total
		if u.Hour.After(st.last) {
			st.last = u.Hour
		}
	}

	out := CodingSummary{
		From: in.span.from.UTC(), To: in.now.UTC(), Timezone: in.span.loc.String(), Step: "hour",
		Totals: all.totals(), PricesAsOf: in.prices.AsOf,
		Tools: []CodingTool{}, Models: []CodingModel{}, Series: []CodingSeries{}, Devices: []CodingDevice{},
		Sessions: []CodingSession{}, Limits: []CodingLimit{}, Sources: []CodingSource{},
	}
	if in.span.daily {
		out.Step = "day"
	}

	// The tools, biggest first, and their series in the same order.
	tools := make([]string, 0, len(byTool))
	for tool := range byTool {
		tools = append(tools, tool)
	}
	slices.SortFunc(tools, func(a, b string) int {
		return cmp.Or(cmp.Compare(byTool[b].price.Total, byTool[a].price.Total), cmp.Compare(tokenTotal(byTool[b].tokens), tokenTotal(byTool[a].tokens)), cmp.Compare(a, b))
	})
	for _, tool := range tools {
		out.Tools = append(out.Tools, CodingTool{Tool: tool, Totals: byTool[tool].totals()})
		points := []CodingPoint{}
		for t := in.span.from; t.Before(in.span.to); t = in.span.next(t) {
			if pt := series[tool][t.UnixMilli()]; pt != nil {
				points = append(points, *pt)
			} else {
				points = append(points, CodingPoint{TS: t.UnixMilli()})
			}
		}
		out.Series = append(out.Series, CodingSeries{Tool: tool, Points: points})
	}

	for k, t := range byModel {
		out.Models = append(out.Models, CodingModel{
			Tool: k.tool, Model: k.model, Cost: t.price.Total, Tokens: t.codingTokens(),
			Sessions: len(t.sessions), Priced: t.priced,
		})
	}
	slices.SortFunc(out.Models, func(a, b CodingModel) int {
		return cmp.Or(cmp.Compare(b.Cost, a.Cost), cmp.Compare(b.Tokens.Total, a.Tokens.Total), cmp.Compare(a.Tool, b.Tool), cmp.Compare(a.Model, b.Model))
	})

	if in.fleet {
		keys := map[string]bool{}
		for k := range byDev {
			keys[k] = true
		}
		for k := range in.enabled {
			keys[k] = true
		}
		for k := range keys {
			d := CodingDevice{DeviceID: in.deviceOf(k), Enabled: in.enabled[k]}
			if t := byDev[k]; t != nil {
				d.Totals = t.totals()
			}
			out.Devices = append(out.Devices, d)
		}
		slices.SortFunc(out.Devices, func(a, b CodingDevice) int {
			return cmp.Or(cmp.Compare(b.Totals.Cost.Total, a.Totals.Cost.Total), cmp.Compare(a.DeviceID, b.DeviceID))
		})
	}

	// Sessions, with what the agent said about them, the latest first.
	meta := map[sessionKey]store.CodingSession{}
	for _, s := range in.sessions {
		meta[sessionKey{s.DeviceID, s.Tool, s.ID}] = s
	}
	for k, st := range bySess {
		cs := CodingSession{
			DeviceID: in.deviceOf(st.device), Tool: st.tool, ID: st.id, Last: st.last, First: st.last,
			Cost: st.cost, Models: modelsByCost(st.models),
			Tokens: CodingTokens{
				Input: st.tokens.Input, CacheRead: st.tokens.CacheRead, CacheWrite: st.tokens.CacheWrite,
				Output: st.tokens.Output, Reasoning: st.reasoning, Total: tokenTotal(st.tokens),
			},
		}
		if m, ok := meta[k]; ok {
			cs.Project, cs.First, cs.Last = m.Project, m.First, m.Last
		}
		cs.Active = in.now.Sub(cs.Last) <= codingActiveWithin && !cs.Last.After(in.now.Add(time.Minute))
		out.Sessions = append(out.Sessions, cs)
	}
	slices.SortFunc(out.Sessions, func(a, b CodingSession) int {
		return cmp.Or(b.Last.Compare(a.Last), cmp.Compare(a.DeviceID, b.DeviceID), cmp.Compare(a.ID, b.ID))
	})
	if in.sessionsShown > 0 && len(out.Sessions) > in.sessionsShown {
		out.Sessions = out.Sessions[:in.sessionsShown]
	}

	for _, l := range in.limits {
		cl := CodingLimit{
			DeviceID: in.deviceOf(l.DeviceID), Tool: l.Tool, ID: l.ID, WindowMinutes: l.WindowMinutes,
			Used: l.Used, ResetsAt: l.ResetsAt, Status: l.Status, Plan: l.Plan, Seen: l.Seen,
		}
		cl.Expired = l.ResetsAt != nil && l.ResetsAt.Before(in.now)
		out.Limits = append(out.Limits, cl)
	}
	for _, t := range in.tools {
		out.Sources = append(out.Sources, CodingSource{DeviceID: in.deviceOf(t.DeviceID), Tool: t.Tool, Status: t.Status, At: t.At})
	}
	return out
}

// get returns the tally under key, adding it if it isn't there.
func get[K comparable](m map[K]*tally, key K) *tally {
	t := m[key]
	if t == nil {
		t = &tally{}
		m[key] = t
	}
	return t
}

// modelsByCost lists the models of a session, the one that cost most first.
func modelsByCost(cost map[string]float64) []string {
	names := make([]string, 0, len(cost))
	for m := range cost {
		names = append(names, m)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Or(cmp.Compare(cost[b], cost[a]), cmp.Compare(a, b)) })
	return names
}
