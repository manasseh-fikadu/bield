package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestNewCodingSpan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 6, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name, rng, tz string
		ok            bool
		from, to      time.Time
		daily         bool
		zone          string
	}{
		{"a day is 24 whole hours", "24h", "", true,
			time.Date(2026, 11, 5, 19, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 19, 0, 0, 0, time.UTC), false, "UTC"},
		{"a week is today and six days before, in UTC", "7d", "", true,
			time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), true, "UTC"},
		{"an unknown zone is UTC", "7d", "Mars/Olympus", true,
			time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), true, "UTC"},
		{"days start at midnight where the viewer is", "7d", "Asia/Tokyo", true,
			// It's already the 7th in Tokyo, 03:30.
			time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("", 9*3600)), time.Date(2026, 11, 8, 0, 0, 0, 0, time.FixedZone("", 9*3600)), true, "Asia/Tokyo"},
		{"a year", "1y", "", true,
			time.Date(2025, 11, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), true, "UTC"},
		{"an unknown range", "2d", "", false, time.Time{}, time.Time{}, false, ""},
	}
	for _, tt := range tests {
		got, ok := newCodingSpan(tt.rng, tt.tz, now)
		if ok != tt.ok {
			t.Errorf("%s: ok = %v", tt.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if !got.from.Equal(tt.from) || !got.to.Equal(tt.to) || got.daily != tt.daily || got.loc.String() != tt.zone {
			t.Errorf("%s: span = %v to %v daily %v in %v; want %v to %v daily %v in %v",
				tt.name, got.from.UTC(), got.to.UTC(), got.daily, got.loc, tt.from.UTC(), tt.to.UTC(), tt.daily, tt.zone)
		}
	}
}

// Days in a zone that changes its clock are 23 or 25 hours long, and each
// still gets one bucket.
func TestCodingSpan_daylightSaving(t *testing.T) {
	t.Parallel()
	// Europe/London put its clocks back on 2026-10-25.
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	span, ok := newCodingSpan("7d", "Europe/London", now)
	if !ok {
		t.Fatal("no span")
	}
	var starts []time.Time
	for ts := span.from; ts.Before(span.to); ts = span.next(ts) {
		starts = append(starts, ts)
		if got := span.bucket(ts); !got.Equal(ts) {
			t.Errorf("bucket(%v) = %v, want the start itself", ts, got)
		}
	}
	if len(starts) != 7 {
		t.Fatalf("%d buckets, want 7: %v", len(starts), starts)
	}
	if long := starts[1].Sub(starts[0]); long != 24*time.Hour && long != 25*time.Hour {
		t.Errorf("first day is %v long", long)
	}
	// 2026-10-25 has 25 hours in London.
	for i, s := range starts[:6] {
		if s.In(span.loc).Format("2006-01-02") == "2026-10-25" {
			if got := starts[i+1].Sub(s); got != 25*time.Hour {
				t.Errorf("the day the clocks went back is %v long, want 25h", got)
			}
		}
	}
	// A late-evening hour in UTC belongs to the next day in Tokyo.
	tokyo, _ := newCodingSpan("7d", "Asia/Tokyo", now)
	late := time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC)
	if got := tokyo.bucket(late).In(tokyo.loc).Format("2006-01-02"); got != "2026-10-28" {
		t.Errorf("20:00 UTC is in the Tokyo day %s, want 2026-10-28", got)
	}
}

func TestSummarizeCoding_series(t *testing.T) {
	t.Parallel()
	prices, err := pricing.Parse([]byte(`{"asOf":"d","models":{"m":{"in":1,"out":1,"cacheRead":1,"cacheWrite":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 18, 30, 0, 0, time.UTC)
	span, _ := newCodingSpan("7d", "UTC", now)
	row := func(tool string, hour time.Time, tokens int64) store.CodingUsage {
		return store.CodingUsage{DeviceID: "d1", Tool: tool, Session: "s", Model: "m", Speed: "standard", Hour: hour, Input: tokens, Requests: 1}
	}
	sum := summarizeCoding(codingInput{
		rows: []store.CodingUsage{
			row("claude", time.Date(2026, 11, 6, 17, 0, 0, 0, time.UTC), 1_000_000),
			row("claude", time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC), 1_000_000),
			row("claude", time.Date(2026, 11, 1, 23, 0, 0, 0, time.UTC), 500_000),
			row("codex", time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), 100_000),
		},
		prices: prices, span: span, now: now, deviceOf: func(k string) string { return k },
	})
	if len(sum.Tools) != 2 || sum.Tools[0].Tool != "claude" {
		t.Fatalf("tools = %+v", sum.Tools)
	}
	claude := sum.Series[0]
	if claude.Tool != "claude" || len(claude.Points) != 7 {
		t.Fatalf("series = %+v", sum.Series)
	}
	var costs []string
	for _, p := range claude.Points {
		costs = append(costs, fmt.Sprintf("%.1f", p.Cost))
	}
	// 31 October to 6 November: nothing, 0.5 on the 1st, nothing, nothing,
	// nothing, nothing, and 2.0 on the 6th.
	if want := []string{"0.0", "0.5", "0.0", "0.0", "0.0", "0.0", "2.0"}; fmt.Sprint(costs) != fmt.Sprint(want) {
		t.Errorf("claude's cost by day = %v, want %v", costs, want)
	}
	// The same ID under two tools is two sessions.
	if sum.Totals.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", sum.Totals.Sessions)
	}
	if sum.Totals.Tokens.Total != 2_600_000 {
		t.Errorf("tokens = %d", sum.Totals.Tokens.Total)
	}
}

// BenchmarkSummarizeCoding is a 200-device tailnet with 90 days of tools used
// 6 hours a day, as `make bench` sizes the rest.
func BenchmarkSummarizeCoding(b *testing.B) {
	prices := pricing.Default()
	now := time.Date(2026, 11, 6, 18, 30, 0, 0, time.UTC)
	span, _ := newCodingSpan("90d", "Europe/London", now)
	var rows []store.CodingUsage
	for dev := range 200 {
		for day := range 90 {
			for h := range 6 {
				rows = append(rows, store.CodingUsage{
					DeviceID: fmt.Sprintf("dev%03d", dev), Tool: []string{"claude", "codex"}[dev%2],
					Session: fmt.Sprintf("s%d", day), Model: "claude-opus-5-5", Speed: "standard",
					Hour:  span.from.Add(time.Duration(day*24+10+h) * time.Hour).UTC(),
					Input: 1000, CacheRead: 900_000, CacheWrite: 20_000, Output: 9_000, Requests: 20,
				})
			}
		}
	}
	b.ReportMetric(float64(len(rows)), "rows")
	b.ResetTimer()
	for range b.N {
		summarizeCoding(codingInput{
			rows: rows, prices: prices, span: span, now: now, fleet: true, sessionsShown: 50,
			deviceOf: func(k string) string { return k },
		})
	}
}
