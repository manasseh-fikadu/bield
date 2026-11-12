// Package coding reads what a device's agent reports about the AI coding tools
// on it (#73): how many tokens each session used, and where the tools say the
// limits stand. The agent only ever sends counts, names and times.
package coding

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// How much one report can hold. The agent sends at most 400 of each in a
// request, so these leave room for a newer agent.
const (
	maxUsage    = 2000
	maxSessions = 2000
	maxLimits   = 64
	maxTools    = 16
	// maxCount is the most tokens, or requests, one row can hold. A year of a
	// busy session is billions.
	maxCount = 1e13
	// maxCost is the most dollars one row can say it cost.
	maxCost = 1e6
)

// A Report is what the agent sends to POST /api/agent/sessions. Every number
// in Usage is a total for its key, as far as the tool's files say, and not what
// changed. Unknown fields are left out, for a newer agent.
type Report struct {
	// V is the version of the format, 1.
	V        int       `json:"v"`
	Tools    []Tool    `json:"tools"`
	Sessions []Session `json:"sessions"`
	Usage    []Usage   `json:"usage"`
	Limits   []Limit   `json:"limits"`
}

// Tool says whether the agent could read a tool's files.
type Tool struct {
	Tool   string `json:"tool"`
	Status string `json:"status"`
}

// A Session is one run of a tool.
type Session struct {
	Tool string `json:"tool"`
	ID   string `json:"id"`
	// Project is the name of the folder the session worked in.
	Project string `json:"project"`
	// First and Last are RFC 3339 times, to the second, in UTC.
	First string `json:"first"`
	Last  string `json:"last"`
}

// Usage is what a session used in an hour on a model.
type Usage struct {
	Tool    string `json:"tool"`
	Session string `json:"session"`
	Model   string `json:"model"`
	Speed   string `json:"speed"`
	// Hour is the UTC hour as 2006-01-02T15.
	Hour string `json:"hour"`
	// In is the input that the cache didn't serve. Out includes Reason, the
	// reasoning.
	In     int64 `json:"in"`
	CR     int64 `json:"cr"`
	CW     int64 `json:"cw"`
	Out    int64 `json:"out"`
	Reason int64 `json:"reason"`
	// N is how many requests it made.
	N int64 `json:"n"`
	// Cost is the dollars the tool said it cost, or 0.
	Cost float64 `json:"cost"`
}

// A Limit is where one of a tool's limits stands.
type Limit struct {
	Tool string `json:"tool"`
	ID   string `json:"id"`
	// Window is how many minutes the limit's window is, or 0.
	Window int `json:"window"`
	// Used is the percent used, if the tool says.
	Used *float64 `json:"used"`
	// Resets is when the window starts over, in Unix seconds, or 0.
	Resets int64  `json:"resets"`
	Status string `json:"status"`
	Plan   string `json:"plan"`
	// Seen is when the tool wrote it down, as an RFC 3339 time.
	Seen string `json:"seen"`
}

var (
	toolName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)
	limitID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,63}$`)
)

// hourLayout and the layout of the times to the second, which the agent can write
// without knowing a calendar.
const (
	hourLayout   = "2006-01-02T15"
	secondLayout = "2006-01-02T15:04:05Z"
)

// Clean checks r and returns what the store keeps. Anything that was in the
// last window before oldest is left out, since it would go at the next prune,
// and so is what is more than a day ahead of now, which a machine's clock set
// wrong writes. A row that's malformed is an error that names it, since that's a
// bug in the agent that someone should see.
func (r Report) Clean(now, oldest time.Time) (store.CodingReport, error) {
	switch {
	case r.V != 1:
		return store.CodingReport{}, fmt.Errorf("this report is in format %d, and Bield reads format 1", r.V)
	case len(r.Usage) > maxUsage, len(r.Sessions) > maxSessions, len(r.Limits) > maxLimits, len(r.Tools) > maxTools:
		return store.CodingReport{}, fmt.Errorf("the report is too big: at most %d usage rows, %d sessions, %d limits and %d tools", maxUsage, maxSessions, maxLimits, maxTools)
	}
	ahead := now.Add(24 * time.Hour)
	var out store.CodingReport

	for i, t := range r.Tools {
		if !toolName.MatchString(t.Tool) {
			return out, fmt.Errorf("tool %d has a name that isn't a tool's", i)
		}
		status := t.Status
		if status != "ok" && status != "needs-sqlite" && status != "needs-python" {
			status = "unreadable"
		}
		out.Tools = append(out.Tools, store.CodingToolStatus{Tool: t.Tool, Status: status, At: now})
	}

	for i, s := range r.Sessions {
		first, err1 := time.Parse(secondLayout, s.First)
		last, err2 := time.Parse(secondLayout, s.Last)
		switch {
		case !toolName.MatchString(s.Tool):
			return out, fmt.Errorf("session %d has a tool name that isn't a tool's", i)
		case !plain(s.ID, 128):
			return out, fmt.Errorf("session %d has an ID that is empty, long or has control characters", i)
		case err1 != nil || err2 != nil:
			return out, fmt.Errorf("session %d has a time that isn't like 2026-11-06T17:56:29Z", i)
		}
		if last.Before(first) {
			first, last = last, first
		}
		if last.Before(oldest) || first.After(ahead) {
			continue
		}
		out.Sessions = append(out.Sessions, store.CodingSession{
			Tool: s.Tool, ID: s.ID, Project: project(s.Project), First: first.UTC(), Last: notAfter(last, ahead).UTC(),
		})
	}

	for i, u := range r.Usage {
		hour, err := time.Parse(hourLayout, u.Hour)
		switch {
		case !toolName.MatchString(u.Tool):
			return out, fmt.Errorf("usage row %d has a tool name that isn't a tool's", i)
		case !plain(u.Session, 128):
			return out, fmt.Errorf("usage row %d has a session ID that is empty, long or has control characters", i)
		case !plain(u.Model, 160):
			return out, fmt.Errorf("usage row %d has a model name that is empty, long or has control characters", i)
		case err != nil:
			return out, fmt.Errorf("usage row %d has an hour that isn't like 2026-11-06T17", i)
		case !inRange(u.In, u.CR, u.CW, u.Out, u.Reason, u.N):
			return out, fmt.Errorf("usage row %d has a count below zero or past %.0e", i, maxCount)
		case math.IsNaN(u.Cost) || u.Cost < 0 || u.Cost > maxCost:
			return out, fmt.Errorf("usage row %d has a cost that isn't from 0 to %.0e dollars", i, maxCost)
		}
		if hour.Before(oldest.Truncate(time.Hour)) || hour.After(ahead) {
			continue
		}
		speed := pricing.Standard
		if u.Speed == pricing.Fast || u.Speed == pricing.Ultrafast {
			speed = u.Speed
		}
		out.Usage = append(out.Usage, store.CodingUsage{
			Tool: u.Tool, Session: u.Session, Model: u.Model, Speed: speed, Hour: hour.UTC(),
			Input: u.In, CacheRead: u.CR, CacheWrite: u.CW, Output: u.Out,
			// Reasoning is part of the output.
			Reasoning: min(u.Reason, u.Out), Requests: u.N, CostMicro: int64(math.Round(u.Cost * 1e6)),
		})
	}

	for i, l := range r.Limits {
		seen, err := time.Parse(secondLayout, l.Seen)
		switch {
		case !toolName.MatchString(l.Tool):
			return out, fmt.Errorf("limit %d has a tool name that isn't a tool's", i)
		case !limitID.MatchString(l.ID):
			return out, fmt.Errorf("limit %d has an ID that isn't a limit's", i)
		case err != nil:
			return out, fmt.Errorf("limit %d has a time that isn't like 2026-11-06T17:56:29Z", i)
		case l.Window < 0 || l.Window > 1_000_000 || l.Resets < 0 || l.Resets > 1<<40:
			return out, fmt.Errorf("limit %d has a window or a reset time that's out of range", i)
		case l.Used != nil && (math.IsNaN(*l.Used) || *l.Used < 0 || *l.Used > 1000):
			return out, fmt.Errorf("limit %d has a share used that isn't from 0 to 1000 percent", i)
		}
		status := "ok"
		if l.Status == "rejected" {
			status = "rejected"
		}
		lim := store.CodingLimit{
			Tool: l.Tool, ID: l.ID, WindowMinutes: l.Window, Used: l.Used, Status: status,
			Plan: shorten(clean(l.Plan), 32), Seen: notAfter(seen, ahead).UTC(),
		}
		if l.Resets > 0 {
			lim.ResetsAt = new(time.Unix(l.Resets, 0).UTC())
		}
		out.Limits = append(out.Limits, lim)
	}
	return out, nil
}

func inRange(counts ...int64) bool {
	for _, n := range counts {
		if n < 0 || n > maxCount {
			return false
		}
	}
	return true
}

// notAfter returns t, or limit if t is later.
func notAfter(t, limit time.Time) time.Time {
	if t.After(limit) {
		return limit
	}
	return t
}

// plain reports whether s is one to limit characters, none of them a control
// character.
func plain(s string, limit int) bool {
	n := utf8.RuneCountInString(s)
	return n > 0 && n <= limit && !strings.ContainsFunc(s, unicode.IsControl)
}

// clean replaces control characters in s with spaces.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func shorten(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// project is the name of a folder, cleaned for showing.
func project(s string) string {
	return strings.TrimSpace(shorten(clean(s), 80))
}
