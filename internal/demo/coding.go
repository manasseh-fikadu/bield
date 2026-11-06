package demo

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// A modelUse is a model that a tool's sessions use, and how often.
type modelUse struct {
	name   string
	weight float64
	// fast is the share of its requests that are billed at a fast tier.
	fast float64
}

// codingTools are the tools of the demo's coding sessions, and the models
// that each uses. Every model is one that package pricing has a price for.
var codingTools = map[string][]modelUse{
	"claude":   {{"claude-opus-5-5", 0.5, 0.08}, {"claude-sonnet-5-5", 0.4, 0}, {"claude-haiku-5-5", 0.1, 0}},
	"codex":    {{"gpt-5.6-sol", 0.85, 0.12}, {"gpt-5.5", 0.15, 0}},
	"opencode": {{"deepseek-v3.2", 0.6, 0}, {"glm-4.6", 0.4, 0}},
	"pi":       {{"claude-sonnet-5-5", 1, 0}},
}

// codingProfile is the coding tools on a machine: how much each is used, and how
// many sessions a working day has.
type codingProfile struct {
	tools  map[string]float64
	perDay float64
}

// codingProfiles are the machines of the cast that have coding tools, by
// host name. A generated Mac gets the profile of generatedCoding.
var codingProfiles = map[string]codingProfile{
	"macbook":  {map[string]float64{"claude": 0.6, "codex": 0.3, "opencode": 0.1}, 4},
	"work-mac": {map[string]float64{"claude": 1}, 5},
	"desktop":  {map[string]float64{"codex": 0.5, "opencode": 0.3, "pi": 0.2}, 3},
	"vps-fra":  {map[string]float64{"claude": 1}, 2},
}

var generatedCoding = codingProfile{map[string]float64{"claude": 0.7, "codex": 0.3}, 2.5}

// codingProjects are folders that the sessions work in.
var codingProjects = []string{"bield", "api-gateway", "dotfiles", "mobile-app", "infra", "docs-site", "billing"}

// codingProfileOf returns the coding tools on machine m, if it has any.
func (d *Demo) codingProfileOf(m *machine) (codingProfile, bool) {
	if p, ok := codingProfiles[m.host]; ok {
		return p, m.agent
	}
	// Some of the Macs in a bigger demo.
	if m.index > 0 && m.agent && m.os == "macOS" && unit(d.seed, int64(m.index)*977+5) < 0.6 {
		return generatedCoding, true
	}
	return codingProfile{}, false
}

// seedCoding turns watching coding tools on for the machines that have them
// and writes the sessions they'd have had over the last span, with the limits
// the tools last said and one session that is still going.
func (d *Demo) seedCoding(ctx context.Context, st *store.Store, span time.Duration) error {
	span = min(span, 365*24*time.Hour)
	for _, m := range d.machines {
		prof, ok := d.codingProfileOf(m)
		if !ok {
			continue
		}
		id := historyID(m)
		if err := st.SetCoding(ctx, id, true); err != nil {
			return err
		}
		report := d.codingHistory(m, prof, span)
		if err := st.SaveCoding(ctx, id, report, d.start.Add(-span).Truncate(time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

// codingHistory returns the report that the agent of machine m would have
// sent by the time the demo starts.
func (d *Demo) codingHistory(m *machine, prof codingProfile, span time.Duration) store.CodingReport {
	var r store.CodingReport
	u := func(k int64) float64 { return unit(d.seed, int64(m.index)*1_000_003+k) }
	peak := m.peak
	if peak == 0 {
		peak = 14
	}
	days := int(math.Ceil(span.Hours() / 24))
	today := d.start.UTC().Truncate(24 * time.Hour)

	for back := days; back >= 0; back-- {
		day := today.AddDate(0, 0, -back)
		factor := 1.0
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			factor = 0.25
		}
		n := int(math.Round(prof.perDay * factor * (0.4 + 1.2*u(int64(back)*101))))
		for s := range n {
			key := int64(back)*1009 + int64(s)*17
			tool := pickWeighted(prof.tools, u(key+1))
			start := day.Add(time.Duration(int(peak-4+u(key+2)*8+24)%24) * time.Hour)
			hours := 1 + int(u(key+3)*3.5)
			project := codingProjects[int(u(key+4)*float64(len(codingProjects)))%len(codingProjects)]
			id := fmt.Sprintf("%08x-%04x", mix(uint64(key)^d.seed^uint64(m.index)<<20)&0xffffffff, s) //nolint:gosec // G115: only a number to show.
			used := false
			var first, last time.Time
			for h := range hours {
				hour := start.Add(time.Duration(h) * time.Hour)
				if hour.After(d.start) {
					break
				}
				r.Usage = append(r.Usage, d.codingHour(m, tool, id, hour, key+int64(h)*7)...)
				if !used {
					first, used = hour.Add(time.Duration(u(key+5)*10)*time.Minute), true
				}
				last = hour.Add(time.Duration(30+u(key+6)*29) * time.Minute)
			}
			if used {
				last = minTime(last, d.start.Add(-time.Minute))
				first = minTime(first, last)
				r.Sessions = append(r.Sessions, store.CodingSession{Tool: tool, ID: id, Project: project, First: first, Last: last})
			}
		}
	}

	// A session that is still going on the first machine.
	if m.host == "macbook" {
		hour := d.start.UTC().Truncate(time.Hour)
		id := "live-" + m.host
		r.Usage = append(r.Usage, d.codingHour(m, "claude", id, hour, 424242)...)
		r.Sessions = append(r.Sessions, store.CodingSession{
			Tool: "claude", ID: id, Project: "bield", First: d.start.Add(-50 * time.Minute), Last: d.start.Add(-90 * time.Second),
		})
	}

	for _, tool := range []string{"claude", "codex", "opencode", "pi"} {
		if prof.tools[tool] > 0 {
			r.Tools = append(r.Tools, store.CodingToolStatus{Tool: tool, Status: "ok", At: d.start})
		}
	}
	if m.host == "vps-fra" {
		// A server with neither the sqlite3 command nor Python.
		r.Tools = append(r.Tools, store.CodingToolStatus{Tool: "opencode", Status: "needs-sqlite", At: d.start})
	}
	r.Limits = d.codingLimits(m)
	return r
}

// codingLimits are the limits that the tools on machine m last said.
func (d *Demo) codingLimits(m *machine) []store.CodingLimit {
	var out []store.CodingLimit
	switch m.host {
	case "macbook", "desktop":
		resets := d.start.Add(2*24*time.Hour + 5*time.Hour)
		out = append(out, store.CodingLimit{
			Tool: "codex", ID: "codex:primary", WindowMinutes: 10080, Used: new(63.0), ResetsAt: &resets,
			Status: "ok", Plan: "plus", Seen: d.start.Add(-9 * time.Minute),
		})
	case "work-mac":
		// Claude Code ran into its five-hour limit a while ago, and says only that.
		resets := d.start.Add(80 * time.Minute)
		out = append(out, store.CodingLimit{
			Tool: "claude", ID: "five_hour", WindowMinutes: 300, ResetsAt: &resets,
			Status: "rejected", Seen: d.start.Add(-40 * time.Minute),
		})
	}
	return out
}

// codingHour returns what a session of a tool used in an hour on machine m:
// rows for its models. Most of the tokens are the cache being read again.
func (d *Demo) codingHour(m *machine, tool, session string, hour time.Time, key int64) []store.CodingUsage {
	u := func(k int64) float64 { return unit(d.seed, int64(m.index)*1_000_003+key*31+k) }
	models := codingTools[tool]
	model := pickModel(models, u(1))
	requests := int64(8 + u(2)*50)
	row := func(mu modelUse, share float64, speed string) store.CodingUsage {
		n := max(int64(float64(requests)*share), 1)
		return store.CodingUsage{
			Tool: tool, Session: session, Model: mu.name, Speed: speed, Hour: hour, Requests: n,
			Input:      n * int64(3+u(3)*40),
			CacheRead:  n * int64(40_000+u(4)*160_000),
			CacheWrite: n * int64(1_500+u(5)*6_000),
			Output:     n * int64(400+u(6)*1_500),
			Reasoning:  0,
		}
	}
	out := []store.CodingUsage{row(model, 1, "standard")}
	if model.fast > 0 && u(7) < model.fast*4 {
		out = append(out, row(model, model.fast, "fast"))
	}
	if tool == "claude" {
		// Claude Code asks a small model for titles and summaries.
		small := models[len(models)-1]
		if small.name != model.name {
			sm := row(small, 0.1, "standard")
			sm.CacheRead, sm.CacheWrite = 0, 0
			out = append(out, sm)
		}
	}
	if tool == "codex" || tool == "opencode" {
		for i := range out {
			out[i].Reasoning = out[i].Output / 3
		}
	}
	return out
}

// pickWeighted returns the key of a map of weights that the number x, from 0 up
// to 1, falls on, in the order of the keys so it doesn't change.
func pickWeighted(weights map[string]float64, x float64) string {
	names := []string{"claude", "codex", "opencode", "pi"}
	var total float64
	for _, n := range names {
		total += weights[n]
	}
	x *= total
	for _, n := range names {
		if w := weights[n]; w > 0 {
			if x < w {
				return n
			}
			x -= w
		}
	}
	return names[0]
}

func pickModel(models []modelUse, x float64) modelUse {
	var total float64
	for _, m := range models {
		total += m.weight
	}
	x *= total
	for _, m := range models {
		if x < m.weight {
			return m
		}
		x -= m.weight
	}
	return models[0]
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
