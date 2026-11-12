package api_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// A budget raises a warning at 80% and a critical alert once it's spent,
// for every machine together and for each one, with the numbers the AI usage
// page shows. Its alerts resolve when the budget goes up.
func TestCoding_budgets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		prices, err := pricing.Parse([]byte(codingPrices))
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		hour := now.UTC().Truncate(time.Hour)
		for id, u := range map[string]store.CodingUsage{
			// A million tokens of each kind of claude-x: $7.35.
			"laptop": {Input: 1e6, CacheRead: 1e6, CacheWrite: 1e6, Output: 1e6},
			// A million tokens of input: $1.
			"server": {Input: 1e6},
		} {
			u.DeviceID, u.Tool, u.Session, u.Model, u.Speed, u.Hour = id, "claude", "s1", "claude-x", "standard", hour
			if err := st.SaveCoding(t.Context(), id, store.CodingReport{Usage: []store.CodingUsage{u}}, now.Add(-24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		prefs := settings.NewCache(st, nil)
		budget := func(b settings.AIBudget) {
			t.Helper()
			if _, _, err := prefs.Update(context.Background(), func(s *settings.Settings) error { s.AIBudget = b; return nil }); err != nil {
				t.Fatal(err)
			}
		}
		budget(settings.AIBudget{Window: "24h", Cost: 10, MachineCost: 5, MachineTokens: 3e6})
		status := &tailnet.Status{
			State: tailnet.StateRunning,
			Self:  tailnet.Device{ID: "bield", HostName: "bield", Online: true},
			Peers: []tailnet.Device{
				{ID: "laptop", HostName: "laptop", Online: true},
				{ID: "server", HostName: "server", Online: true},
			},
		}
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, StatusAt: time.Now()} },
			Alerts:  alerts.NewEngine(st, nil), Store: st, Settings: prefs, Prices: prices,
		})
		go feed.Run(t.Context())
		synctest.Wait()
		time.Sleep(2 * time.Second)
		want := []string{
			"critical: AI usage on laptop passed its $5 budget for 24 hours ($7.35)",
			"critical: AI usage on laptop passed its 3M-token budget for 24 hours (4M tokens)",
			"warning: AI usage is at 83% of its $10 budget for 24 hours",
		}
		snap := feed.Latest()
		if got := alertSummary(snap.Alerts); !slices.Equal(got, want) {
			t.Fatalf("alerts =\n%q\nwant\n%q", got, want)
		}
		for _, a := range snap.Alerts {
			if a.Source != api.AlertSourceBudget || (a.DeviceID != "laptop" && a.DeviceID != "") {
				t.Errorf("%q has source %q and device %q", a.Title, a.Source, a.DeviceID)
			}
		}

		// The AI usage page shows how much of the budget is used.
		h := api.New(withStore(t, testTailnet(), st, api.Options{Feed: feed, Settings: prefs}))
		var sum api.CodingSummary
		get(t, h, "/api/coding?range=24h", phoneIP, &sum)
		if b := sum.Budget; b == nil || b.Cost != 10 || b.Window != "24h" || !near(b.UsedCost, sum.Totals.Cost.Total) || b.UsedTokens != sum.Totals.Tokens.Total {
			t.Errorf("budget = %+v, want $10 over 24h, with the page's $%v and %d tokens", b, sum.Totals.Cost.Total, sum.Totals.Tokens.Total)
		}

		// Raising the budgets resolves their alerts a minute later.
		budget(settings.AIBudget{Window: "24h", Cost: 100})
		feed.Poke()
		time.Sleep(90 * time.Second)
		if got := feed.Latest().Alerts; len(got) != 0 {
			t.Errorf("after the budgets went up, alerts = %q, want none", alertSummary(got))
		}

		// And turning the rule off resolves them too.
		budget(settings.AIBudget{Window: "24h", Cost: 1})
		feed.Poke()
		time.Sleep(2 * time.Second)
		if got := alertSummary(feed.Latest().Alerts); !slices.Equal(got, []string{"critical: AI usage passed its $1 budget for 24 hours ($8.35)"}) {
			t.Fatalf("with a budget of $1, alerts = %q", got)
		}
		if _, _, err := prefs.Update(context.Background(), func(s *settings.Settings) error { s.Rules.AIBudget = false; return nil }); err != nil {
			t.Fatal(err)
		}
		feed.Poke()
		time.Sleep(90 * time.Second)
		if got := feed.Latest().Alerts; len(got) != 0 {
			t.Errorf("with the rule off, alerts = %q, want none", alertSummary(got))
		}
	})
}
