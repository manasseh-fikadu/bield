package api

import (
	"log/slog"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestBudgetConditions(t *testing.T) {
	t.Parallel()
	laptop := Device{ID: "laptop", Name: "laptop"}
	keys := map[string]Device{"laptop": laptop}
	spend := func(fleet, mine spent) *budgetSpend {
		return &budgetSpend{fleet: fleet, byKey: map[string]spent{"laptop": mine, "gone": {cost: 1e6, tokens: 1e12}}}
	}
	for _, tt := range []struct {
		name   string
		budget settings.AIBudget
		spend  *budgetSpend
		want   []string
	}{
		{"nothing worked out", settings.AIBudget{Window: "30d", Cost: 10}, nil, nil},
		{"under 80%", settings.AIBudget{Window: "30d", Cost: 100, Tokens: 1e9}, spend(spent{79.99, 799e6}, spent{}), nil},
		{"at 80%", settings.AIBudget{Window: "30d", Cost: 100}, spend(spent{cost: 80}, spent{}),
			[]string{"budget:fleet:cost warning: AI usage is at 80% of its $100 budget for 30 days"}},
		{"just under all of it", settings.AIBudget{Window: "7d", Cost: 12.5}, spend(spent{cost: 12.49}, spent{}),
			[]string{"budget:fleet:cost warning: AI usage is at 99% of its $12.50 budget for 7 days"}},
		{"all of it", settings.AIBudget{Window: "24h", Tokens: 10e6}, spend(spent{tokens: 10e6}, spent{}),
			[]string{"budget:fleet:tokens critical: AI usage passed its 10M-token budget for 24 hours (10M tokens)"}},
		{"a machine", settings.AIBudget{Window: "7d", MachineCost: 20, MachineTokens: 5e6}, spend(spent{}, spent{23.4, 4.2e6}),
			[]string{
				"budget:laptop:cost critical: AI usage on laptop passed its $20 budget for 7 days ($23.40)",
				"budget:laptop:tokens warning: AI usage on laptop is at 84% of its 5M-token budget for 7 days",
			}},
		{"no budgets", settings.AIBudget{Window: "30d"}, spend(spent{1e6, 1e12}, spent{1e6, 1e12}), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, c := range budgetConditions(tt.budget, tt.spend, keys) {
				if c.Source != string(AlertSourceBudget) || c.Detail == "" {
					t.Errorf("%s has source %q and detail %q", c.Key, c.Source, c.Detail)
				}
				if c.DeviceID != "" && c.DeviceID != "laptop" {
					t.Errorf("%s is on %q, a machine that isn't on the tailnet", c.Key, c.DeviceID)
				}
				got = append(got, c.Key+" "+string(c.Severity)+": "+c.Title)
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("conditions =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestTokenText(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		n    int64
		want string
	}{
		{0, "0"}, {950, "950"}, {1000, "1K"}, {12_345, "12.3K"}, {999_999, "999K"}, {3_450_000, "3.4M"},
		{120_000_000, "120M"}, {1_200_000_000, "1.2B"}, {2e12, "2T"},
	} {
		if got := tokenText(tt.n); got != tt.want {
			t.Errorf("tokenText(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// What the AI tools spent is worked out in the background at most once a
// minute, or at once for another window, and never while there's no budget.
func TestBudgetWatch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "bield.db"), store.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		now := time.Now()
		rep := store.CodingReport{Usage: []store.CodingUsage{{
			DeviceID: "laptop", Tool: "claude", Session: "s1", Model: "claude-x", Speed: "standard",
			Hour: now.UTC().Truncate(time.Hour), Input: 1_000_000, Output: 1_000_000,
		}}}
		if err := st.SaveCoding(t.Context(), "laptop", rep, now.Add(-365*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		prices, err := pricing.Parse([]byte(`{"asOf":"2026-11-06","models":{"claude-x":{"in":1,"out":5}}}`))
		if err != nil {
			t.Fatal(err)
		}
		var pokes atomic.Int32
		w := &budgetWatch{st: st, prices: prices, log: slog.New(slog.DiscardHandler), done: func() { pokes.Add(1) }}
		ctx := t.Context()

		if s := w.latest(ctx, settings.AIBudget{Window: "24h"}, time.Now()); s != nil || !w.tried.IsZero() {
			t.Fatalf("with no budget, latest = %+v and it tried at %v, want nothing", s, w.tried)
		}
		b := settings.AIBudget{Window: "24h", Timezone: "UTC", Cost: 10}
		if s := w.latest(ctx, b, time.Now()); s != nil {
			t.Fatalf("before it's worked out, latest = %+v", s)
		}
		synctest.Wait()
		first := w.latest(ctx, b, time.Now())
		if first == nil || first.fleet.cost != 6 || first.fleet.tokens != 2_000_000 || first.byKey["laptop"].cost != 6 || pokes.Load() != 1 {
			t.Fatalf("latest = %+v after %d pokes, want $6 and 2M tokens, and a poke", first, pokes.Load())
		}

		time.Sleep(30 * time.Second)
		synctest.Wait()
		if s := w.latest(ctx, b, time.Now()); s != first || pokes.Load() != 1 {
			t.Errorf("30 seconds later, it worked it out again")
		}
		time.Sleep(31 * time.Second)
		if s := w.latest(ctx, b, time.Now()); s != first {
			t.Errorf("a minute later, latest didn't return the last one while it works it out")
		}
		synctest.Wait()
		if s := w.latest(ctx, b, time.Now()); s == first || s == nil || pokes.Load() != 2 {
			t.Errorf("a minute later, it didn't work it out again")
		}

		// Another window is worked out at once, and the old one isn't
		// passed off as it.
		b.Window = "7d"
		if s := w.latest(ctx, b, time.Now()); s != nil {
			t.Errorf("for another window, latest = %+v, want nothing until it's worked out", s)
		}
		synctest.Wait()
		if s := w.latest(ctx, b, time.Now()); s == nil || s.window != "7d" || s.fleet.cost != 6 {
			t.Errorf("for 7 days, latest = %+v", s)
		}
	})
}
