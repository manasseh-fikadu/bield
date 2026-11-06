package store_test

import (
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

var codingHour = time.Date(2026, 11, 6, 17, 0, 0, 0, time.UTC)

func codingUsage(session string, in, out int64) store.CodingUsage {
	return store.CodingUsage{
		Tool: "claude", Session: session, Model: "claude-opus-5-5", Speed: "standard", Hour: codingHour,
		Input: in, CacheRead: in * 10, CacheWrite: in * 2, Output: out, Reasoning: out / 2, Requests: 3,
	}
}

func openCoding(t *testing.T) *store.Store {
	t.Helper()
	return openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
}

func TestStore_codingEnabled(t *testing.T) {
	t.Parallel()
	s := openCoding(t)
	ctx := t.Context()
	on, err := s.CodingEnabled(ctx, "d1")
	if err != nil || on {
		t.Fatalf("CodingEnabled on a new device = %v, %v", on, err)
	}
	for range 2 { // turning it on twice is as good as once
		if err := s.SetCoding(ctx, "d1", true); err != nil {
			t.Fatal(err)
		}
	}
	if on, _ := s.CodingEnabled(ctx, "d1"); !on {
		t.Error("CodingEnabled after turning it on = false")
	}
	all, err := s.CodingDevices(ctx)
	if err != nil || !all["d1"] || len(all) != 1 {
		t.Errorf("CodingDevices = %v, %v", all, err)
	}
	if err := s.SetCoding(ctx, "d1", false); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.CodingEnabled(ctx, "d1"); on {
		t.Error("CodingEnabled after turning it off = true")
	}
}

// A report is totals, so sending one again changes nothing, and an agent that
// starts over with less doesn't take anything away.
func TestStore_codingKeepsTheLargerTotals(t *testing.T) {
	t.Parallel()
	s := openCoding(t)
	ctx := t.Context()
	oldest := codingHour.Add(-time.Hour)
	save := func(r store.CodingReport) {
		t.Helper()
		if err := s.SaveCoding(ctx, "d1", r, oldest); err != nil {
			t.Fatal(err)
		}
	}
	read := func() []store.CodingUsage {
		t.Helper()
		got, err := s.CodingUsageBetween(ctx, "d1", codingHour, codingHour.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	first := store.CodingReport{Usage: []store.CodingUsage{codingUsage("a", 10, 100)}}
	save(first)
	save(first)
	got := read()
	if len(got) != 1 || got[0].Input != 10 || got[0].Output != 100 || got[0].Requests != 3 {
		t.Fatalf("after the same report twice: %+v", got)
	}

	// More of the same session: the totals grow.
	save(store.CodingReport{Usage: []store.CodingUsage{codingUsage("a", 15, 80)}})
	got = read()
	if got[0].Input != 15 || got[0].Output != 100 {
		t.Errorf("after a report with more input and less output: %+v", got[0])
	}

	// Another session, model and speed are other rows.
	other := codingUsage("b", 1, 1)
	fast := codingUsage("a", 1, 1)
	fast.Speed = "fast"
	save(store.CodingReport{Usage: []store.CodingUsage{other, fast}})
	if got := read(); len(got) != 3 {
		t.Errorf("rows = %d, want 3: %+v", len(got), got)
	}

	// Another device doesn't see them.
	if got, _ := s.CodingUsageBetween(ctx, "d2", codingHour, codingHour.Add(time.Hour)); len(got) != 0 {
		t.Errorf("d2 has %+v", got)
	}
	// The whole fleet does.
	if got, _ := s.CodingUsageBetween(ctx, "", codingHour, codingHour.Add(time.Hour)); len(got) != 3 {
		t.Errorf("the fleet has %d rows", len(got))
	}
	// A span that ends where the hour starts holds nothing.
	if got, _ := s.CodingUsageBetween(ctx, "d1", codingHour.Add(-time.Hour), codingHour); len(got) != 0 {
		t.Errorf("the hour before has %+v", got)
	}
}

func TestStore_codingLeavesOutWhatWouldBePruned(t *testing.T) {
	t.Parallel()
	s := openCoding(t)
	ctx := t.Context()
	old := codingUsage("old", 1, 1)
	old.Hour = codingHour.Add(-48 * time.Hour)
	err := s.SaveCoding(ctx, "d1", store.CodingReport{
		Usage:    []store.CodingUsage{old, codingUsage("new", 1, 1)},
		Sessions: []store.CodingSession{{Tool: "claude", ID: "old", First: old.Hour, Last: old.Hour}},
	}, codingHour.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.CodingUsageBetween(ctx, "d1", codingHour.Add(-72*time.Hour), codingHour.Add(time.Hour))
	if len(got) != 1 || got[0].Session != "new" {
		t.Errorf("usage = %+v", got)
	}
	if ss, _ := s.CodingSessionsSince(ctx, "d1", codingHour.Add(-72*time.Hour), 10); len(ss) != 0 {
		t.Errorf("sessions = %+v", ss)
	}
}

func TestStore_codingSessions(t *testing.T) {
	t.Parallel()
	s := openCoding(t)
	ctx := t.Context()
	at := func(m int) time.Time { return codingHour.Add(time.Duration(m) * time.Minute) }
	save := func(r store.CodingReport) {
		t.Helper()
		if err := s.SaveCoding(ctx, "d1", r, codingHour.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	save(store.CodingReport{Sessions: []store.CodingSession{
		{Tool: "claude", ID: "a", Project: "bield", First: at(10), Last: at(20)},
		{Tool: "codex", ID: "b", Project: "", First: at(30), Last: at(40)},
	}})
	// Later, the same session has gone on, and an empty project name keeps
	// the one Bield has.
	save(store.CodingReport{Sessions: []store.CodingSession{
		{Tool: "claude", ID: "a", Project: "", First: at(5), Last: at(50)},
	}})
	got, err := s.CodingSessionsSince(ctx, "d1", at(0), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("sessions, latest first = %+v", got)
	}
	if got[0].Project != "bield" || !got[0].First.Equal(at(5)) || !got[0].Last.Equal(at(50)) {
		t.Errorf("session a = %+v", got[0])
	}
	if got, _ := s.CodingSessionsSince(ctx, "d1", at(45), 10); len(got) != 1 {
		t.Errorf("since 45 minutes: %+v", got)
	}
	if got, _ := s.CodingSessionsSince(ctx, "d1", at(0), 1); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("limit 1: %+v", got)
	}
}

func TestStore_codingLimitsAndTools(t *testing.T) {
	t.Parallel()
	s := openCoding(t)
	ctx := t.Context()
	used := func(f float64) *float64 { return &f }
	resets := codingHour.Add(5 * time.Hour)
	save := func(r store.CodingReport) {
		t.Helper()
		if err := s.SaveCoding(ctx, "d1", r, codingHour.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	save(store.CodingReport{
		Limits: []store.CodingLimit{{Tool: "codex", ID: "codex:primary", WindowMinutes: 10080, Used: used(18), ResetsAt: &resets, Status: "ok", Plan: "free", Seen: codingHour}},
		Tools:  []store.CodingToolStatus{{Tool: "codex", Status: "ok", At: codingHour}},
	})
	// An older reading doesn't replace the newer one, and a newer does.
	save(store.CodingReport{Limits: []store.CodingLimit{{Tool: "codex", ID: "codex:primary", WindowMinutes: 10080, Used: used(5), Status: "ok", Seen: codingHour.Add(-time.Hour)}}})
	got, err := s.CodingLimits(ctx, "d1")
	if err != nil || len(got) != 1 || got[0].Used == nil || *got[0].Used != 18 || got[0].ResetsAt == nil || !got[0].ResetsAt.Equal(resets) {
		t.Fatalf("after an older reading: %+v, %v", got, err)
	}
	save(store.CodingReport{Limits: []store.CodingLimit{{Tool: "codex", ID: "codex:primary", WindowMinutes: 10080, Used: used(30), Status: "ok", Seen: codingHour.Add(time.Minute)}}})
	got, _ = s.CodingLimits(ctx, "d1")
	if *got[0].Used != 30 || got[0].ResetsAt != nil {
		t.Errorf("after a newer reading: %+v", got[0])
	}
	if all, _ := s.CodingLimits(ctx, ""); len(all) != 1 {
		t.Errorf("fleet limits = %+v", all)
	}
	if other, _ := s.CodingLimits(ctx, "d2"); len(other) != 0 {
		t.Errorf("d2 limits = %+v", other)
	}

	tools, err := s.CodingTools(ctx, "d1")
	if err != nil || len(tools) != 1 || tools[0].Status != "ok" {
		t.Errorf("tools = %+v, %v", tools, err)
	}
	save(store.CodingReport{Tools: []store.CodingToolStatus{{Tool: "codex", Status: "unreadable", At: codingHour.Add(time.Hour)}}})
	if tools, _ = s.CodingTools(ctx, ""); tools[0].Status != "unreadable" {
		t.Errorf("tools after a new status = %+v", tools)
	}
}

func TestStore_codingPrune(t *testing.T) {
	t.Parallel()
	const day = 24 * time.Hour
	for _, tt := range []struct {
		name string
		keep time.Duration
		want []string
	}{
		{"a year", 365 * day, []string{"10d", "40d"}},
		{"a month", 30 * day, []string{"10d"}},
		{"a week", 7 * day, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				keep := store.Retention{Activity: day, History: day, Checks: day, Coding: tt.keep}
				s, err := store.OpenWith(t.Context(), filepath.Join(t.TempDir(), "bield.db"), store.Options{Retention: fixedRetention(keep)}, store.Migrations())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close() })
				synctest.Wait()
				now := time.Now().UTC().Truncate(time.Hour)
				mk := func(name string, ago time.Duration) store.CodingUsage {
					u := codingUsage(name, 1, 1)
					u.Hour = now.Add(-ago)
					return u
				}
				report := store.CodingReport{
					Usage:    []store.CodingUsage{mk("10d", 10*day), mk("40d", 40*day)},
					Sessions: []store.CodingSession{{Tool: "claude", ID: "10d", First: now.Add(-10 * day), Last: now.Add(-10 * day)}, {Tool: "claude", ID: "40d", First: now.Add(-40 * day), Last: now.Add(-40 * day)}},
				}
				if err := s.SaveCoding(t.Context(), "d1", report, now.Add(-100*day)); err != nil {
					t.Fatal(err)
				}
				time.Sleep(store.PruneInterval)
				synctest.Wait()
				var got []string
				rows, _ := s.CodingUsageBetween(t.Context(), "", now.Add(-100*day), now)
				for _, u := range rows {
					got = append(got, u.Session)
				}
				slices.Sort(got)
				if !slices.Equal(got, tt.want) {
					t.Errorf("usage left = %q, want %q", got, tt.want)
				}
				ss, _ := s.CodingSessionsSince(t.Context(), "", now.Add(-100*day), 10)
				if len(ss) != len(tt.want) {
					t.Errorf("sessions left = %d, want %d", len(ss), len(tt.want))
				}
			})
		})
	}
}
