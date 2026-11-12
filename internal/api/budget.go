package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/settings"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

const (
	// budgetEvery is how often Bield works out what the AI tools spent, for
	// their budgets.
	budgetEvery = time.Minute
	// budgetStale is how old what Bield worked out can be for an alert of a
	// budget to resolve.
	budgetStale = 3 * time.Minute
	// budgetTimeout is the longest working it out can take.
	budgetTimeout = 30 * time.Second
	// budgetWarn is the share of a budget that raises a warning. All of it
	// raises a critical alert.
	budgetWarn = 0.8
)

// spent is what AI tools used: the API estimate of its cost, in dollars, and
// the tokens they processed.
type spent struct {
	cost   float64
	tokens int64
}

// budgetSpend is what the AI tools spent over a budget's window, as Bield
// last worked it out.
type budgetSpend struct {
	// window and zone are the window it covers, and the name of the time
	// zone its days are in.
	window, zone string
	from         time.Time
	// at is when it was worked out.
	at    time.Time
	fleet spent
	// byKey is what each machine spent, by the key its history is kept under.
	byKey map[string]spent
}

// budgetWatch works out what the AI tools spent over the window of the AI
// usage budget, at most once every budgetEvery, in the background, so that
// building a snapshot never waits for it. It works nothing out while there's
// no budget. It's safe for concurrent use.
type budgetWatch struct {
	st     *store.Store
	prices *pricing.Table
	log    *slog.Logger
	// done is called after working it out in the background, so a new
	// snapshot has it.
	done func()

	mu  sync.Mutex
	cur *budgetSpend
	// running says it's being worked out.
	running bool
	// tried is when it was last started, and triedFor which window and zone
	// it was for.
	tried    time.Time
	triedFor string
	// failing says the latest try failed, so its error is logged once.
	failing bool
}

// latest returns what the AI tools spent over b's window, as last worked out
// for that window, or nil if it hasn't been yet or b has no budget. It starts
// working it out again in the background if that's a minute old, or was for
// another window.
func (w *budgetWatch) latest(ctx context.Context, b settings.AIBudget, now time.Time) *budgetSpend {
	if !b.Any() {
		return nil
	}
	loc := b.Location()
	key := b.Window + " " + loc.String()
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running && (now.Sub(w.tried) >= budgetEvery || w.triedFor != key) {
		w.running, w.tried, w.triedFor = true, now, key
		// The request or snapshot that asked can end before it's done.
		ctx := context.WithoutCancel(ctx)
		go w.refresh(ctx, b.Window, loc, now)
	}
	if w.cur == nil || w.cur.window != b.Window || w.cur.zone != loc.String() {
		return nil
	}
	return w.cur
}

// wait returns what latest does, but if that's nil, works it out first,
// for a page that shows it. It returns nil if b has no budget, or it can't be
// worked out.
func (w *budgetWatch) wait(ctx context.Context, b settings.AIBudget, now time.Time) *budgetSpend {
	if s := w.latest(ctx, b, now); s != nil || !b.Any() {
		return s
	}
	s, err := w.spend(ctx, b.Window, b.Location(), now)
	if err != nil {
		w.log.Warn("work out what the AI tools spent", "error", err)
		return nil
	}
	return s
}

func (w *budgetWatch) refresh(ctx context.Context, window string, loc *time.Location, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, budgetTimeout)
	defer cancel()
	s, err := w.spend(ctx, window, loc, now)
	w.mu.Lock()
	w.running = false
	switch {
	case err != nil && !w.failing:
		w.log.Warn("work out what the AI tools spent, for their budgets", "error", err)
	case err == nil && w.failing:
		w.log.Info("worked out what the AI tools spent again")
	}
	w.failing = err != nil
	if err == nil && (w.cur == nil || !s.at.Before(w.cur.at)) {
		w.cur = s
	}
	w.mu.Unlock()
	if err == nil && w.done != nil {
		w.done()
	}
}

// spend works out what the AI tools spent over a window at now, the way the
// AI usage page does, so the numbers are the same.
func (w *budgetWatch) spend(ctx context.Context, window string, loc *time.Location, now time.Time) (*budgetSpend, error) {
	span, ok := newCodingSpanIn(window, loc, now)
	if !ok {
		return nil, fmt.Errorf("a budget can't have a window of %q", window)
	}
	rows, err := w.st.CodingUsageBetween(ctx, "", span.from, span.to)
	if err != nil {
		return nil, err
	}
	sum := summarizeCoding(codingInput{
		rows: rows, prices: w.prices, span: span, now: now, fleet: true,
		deviceOf: func(key string) string { return key },
	})
	out := &budgetSpend{
		window: window, zone: loc.String(), from: span.from, at: now,
		fleet: spent{sum.Totals.Cost.Total, sum.Totals.Tokens.Total},
		byKey: make(map[string]spent, len(sum.Devices)),
	}
	for _, d := range sum.Devices {
		out.byKey[d.DeviceID] = spent{d.Totals.Cost.Total, d.Totals.Tokens.Total}
	}
	return out, nil
}

// budgetConditions returns the alerts that the budgets of b need, given what
// s says the AI tools spent. keys maps the keys that history is kept under to
// the devices on the tailnet. A machine that isn't on it has no budget of its
// own, but counts towards every machine's.
func budgetConditions(b settings.AIBudget, s *budgetSpend, keys map[string]Device) []alerts.Condition {
	if s == nil {
		return nil
	}
	var out []alerts.Condition
	window := budgetWindowName(b.Window)
	check := func(key, deviceID, who string, used, budget float64, cost bool) {
		if budget <= 0 || used < budget*budgetWarn {
			return
		}
		c := alerts.Condition{
			Key: "budget:" + key, Source: string(AlertSourceBudget), DeviceID: deviceID, Severity: alerts.Warning,
		}
		what, usedText := tokenText(int64(budget))+"-token", tokenText(int64(used))+" tokens"
		detail := fmt.Sprintf("%s of %s tokens processed in the last %s, as the AI usage page counts them.",
			tokenText(int64(used)), tokenText(int64(budget)), window)
		if cost {
			what, usedText = budgetMoney(budget), spendMoney(used)
			detail = fmt.Sprintf("%s of %s in the last %s, by the API estimate the AI usage page shows.",
				spendMoney(used), budgetMoney(budget), window)
		}
		subject := "AI usage" + who
		if used >= budget {
			c.Severity = alerts.Critical
			c.Title = fmt.Sprintf("%s passed its %s budget for %s (%s)", subject, what, window, usedText)
		} else {
			// Down, so 99.6% isn't called 100% before it is.
			pct := math.Floor(used / budget * 100)
			c.Title = fmt.Sprintf("%s is at %.0f%% of its %s budget for %s", subject, pct, what, window)
		}
		c.Detail = detail
		out = append(out, c)
	}
	check("fleet:cost", "", "", s.fleet.cost, b.Cost, true)
	check("fleet:tokens", "", "", float64(s.fleet.tokens), float64(b.Tokens), false)
	for key, sp := range s.byKey {
		d, ok := keys[key]
		if !ok {
			continue
		}
		check(d.ID+":cost", d.ID, " on "+d.Name, sp.cost, b.MachineCost, true)
		check(d.ID+":tokens", d.ID, " on "+d.Name, float64(sp.tokens), float64(b.MachineTokens), false)
	}
	return out
}

// historyKeys maps the keys that history is kept under to the IDs of the
// devices on the tailnet. For the one machine that has two nodes, the
// first, which is Bield's own, wins.
func historyKeys(st *tailnet.Status) map[string]string {
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

// budgetWindowName names a budget's window, such as "30 days".
func budgetWindowName(w string) string {
	if w == "24h" {
		return "24 hours"
	}
	if days, ok := codingRanges[w]; ok {
		return strconv.Itoa(days) + " days"
	}
	return w
}

// budgetMoney writes a budget in dollars, without cents if it has none, such
// as $100 or $12.50.
func budgetMoney(v float64) string {
	if v == math.Trunc(v) {
		return "$" + strconv.FormatFloat(v, 'f', 0, 64)
	}
	return spendMoney(v)
}

// spendMoney writes dollars with their cents, such as $23.40.
func spendMoney(v float64) string { return fmt.Sprintf("$%.2f", v) }

// tokenText writes a number of tokens short, such as 950, 12K, 3.4M or 1.2B.
func tokenText(n int64) string {
	for _, u := range []struct {
		size float64
		unit string
	}{{1e12, "T"}, {1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if v := float64(n) / u.size; v >= 1 {
			if v >= 100 || v == math.Trunc(v) {
				return strconv.FormatFloat(math.Floor(v), 'f', 0, 64) + u.unit
			}
			return strconv.FormatFloat(math.Floor(v*10)/10, 'f', -1, 64) + u.unit
		}
	}
	return strconv.FormatInt(n, 10)
}
