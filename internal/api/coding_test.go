package api_test

import (
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/hostmetrics"
	"github.com/manasseh-fikadu/bield/internal/pricing"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// codingPrices prices a million tokens of each kind of claude-x at 1, 0.1, 1.25
// and 5 dollars, and its fast tier at twice that, so sums are easy to check.
const codingPrices = `{"asOf":"2026-11-06","models":{
 "claude-x":{"in":1,"out":5,"cacheRead":0.1,"cacheWrite":1.25,"fast":{"in":2,"out":10,"cacheRead":0.2,"cacheWrite":2.5}},
 "gpt-x":{"in":2,"out":8,"cacheRead":0.2,"cacheWrite":2}}}`

// codingReport returns a report with a million tokens of each kind, in the
// hour before now, from a session of a tool.
func codingReport(now time.Time, tool, session, model, speed string) string {
	hour := now.UTC().Add(-time.Hour).Format("2006-01-02T15")
	at := now.UTC().Add(-50 * time.Minute).Format("2006-01-02T15:04:05Z")
	return `{"v":1,"tools":[{"tool":"` + tool + `","status":"ok"}],
	 "sessions":[{"tool":"` + tool + `","id":"` + session + `","project":"secret-client","first":"` + at + `","last":"` + at + `"}],
	 "usage":[{"tool":"` + tool + `","session":"` + session + `","model":"` + model + `","speed":"` + speed + `","hour":"` + hour + `",
	  "in":1000000,"cr":1000000,"cw":1000000,"out":1000000,"reason":250000,"n":7,"cost":0}],
	 "limits":[{"tool":"codex","id":"codex:primary","window":10080,"used":18.5,"resets":` + itoa(now.Add(24*time.Hour).Unix()) + `,"status":"ok","plan":"plus","seen":"` + at + `"}]}`
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func codingHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	prices, err := pricing.Parse([]byte(codingPrices))
	if err != nil {
		t.Fatal(err)
	}
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{Prices: prices})
	return api.New(opts), st
}

func TestCoding_flow(t *testing.T) {
	t.Parallel()
	h, st := codingHandler(t)
	secret := enrolled(t, st, "laptop")
	now := time.Now()

	// Until an admin turns it on, the agent is told nothing, and its report
	// is refused.
	if rec := sendBeat(t, h, laptopIP, secret, laptopBeat); rec.Code != http.StatusNoContent {
		t.Fatalf("a heartbeat with coding off = %d %s, want 204", rec.Code, rec.Body)
	}
	rec := sendAs(t, h, http.MethodPost, "/api/agent/sessions", laptopIP, secret, codingReport(now, "claude", "s1", "claude-x", "standard"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("a report with coding off = %d %s, want 409", rec.Code, rec.Body)
	}

	// Only admins turn it on.
	if rec := send(t, h, http.MethodPut, "/api/devices/laptop/coding", phoneIP, `{"enabled":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer turning it on = %d, want 403", rec.Code)
	}
	if rec := send(t, h, http.MethodPut, "/api/devices/laptop/coding", laptopIP, `{"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("an admin turning it on = %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPut, "/api/devices/nope/coding", laptopIP, `{"enabled":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("turning it on for a device that isn't there = %d, want 404", rec.Code)
	}

	// The next heartbeat says so.
	rec = sendBeat(t, h, laptopIP, secret, laptopBeat)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("a heartbeat with coding on = %d %s, want 200 with enabled", rec.Code, rec.Body)
	}

	// A report is taken, twice with the same effect, and a second session on
	// a fast tier adds to it.
	for range 2 {
		rec = sendAs(t, h, http.MethodPost, "/api/agent/sessions", laptopIP, secret, codingReport(now, "claude", "s1", "claude-x", "standard"))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("a report = %d %s, want 204", rec.Code, rec.Body)
		}
	}
	rec = sendAs(t, h, http.MethodPost, "/api/agent/sessions", laptopIP, secret, codingReport(now, "claude", "s2", "claude-x", "fast"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("a report on the fast tier = %d %s, want 204", rec.Code, rec.Body)
	}

	// A million tokens of each kind cost 1 + 0.1 + 1.25 + 5 = 7.35 dollars, and
	// twice that on the fast tier: 7.35 for the first session, which reported
	// twice and counts once, and 14.7 for the second.
	var sum api.CodingSummary
	get(t, h, "/api/devices/laptop/coding?range=24h", laptopIP, &sum)
	if !sum.Enabled || sum.Step != "hour" || sum.PricesAsOf != "2026-11-06" {
		t.Errorf("summary = enabled %v step %q prices %q", sum.Enabled, sum.Step, sum.PricesAsOf)
	}
	if got := sum.Totals; got.Sessions != 2 || got.Requests != 14 || got.Tokens.Total != 8_000_000 || got.Tokens.Reasoning != 500_000 {
		t.Errorf("Totals = %+v", got)
	}
	if got, want := sum.Totals.Cost.Total, 7.35*3; !near(got, want) {
		t.Errorf("cost = %v, want %v", got, want)
	}
	if got, want := sum.Totals.SpeedPremium, 7.35; !near(got, want) {
		t.Errorf("speed premium = %v, want %v", got, want)
	}
	if got, want := sum.Totals.FastCost, 7.35*2; !near(got, want) {
		t.Errorf("fast cost = %v, want %v", got, want)
	}
	// The cache saved 0.9 dollars on a million tokens at the standard tier, and 1.8
	// at the fast one.
	if got, want := sum.Totals.CacheSavings, 0.9+1.8; !near(got, want) {
		t.Errorf("cache savings = %v, want %v", got, want)
	}
	if len(sum.Tools) != 1 || sum.Tools[0].Tool != "claude" || len(sum.Models) != 1 || sum.Models[0].Model != "claude-x" || !sum.Models[0].Priced {
		t.Errorf("tools %+v models %+v", sum.Tools, sum.Models)
	}
	if len(sum.Series) != 1 || len(sum.Series[0].Points) != 24 {
		t.Fatalf("series = %+v", sum.Series)
	}
	var seriesCost float64
	for _, p := range sum.Series[0].Points {
		seriesCost += p.Cost
	}
	if !near(seriesCost, sum.Totals.Cost.Total) {
		t.Errorf("the series add up to %v, want %v", seriesCost, sum.Totals.Cost.Total)
	}
	// They were last active 50 minutes ago.
	if len(sum.Sessions) != 2 || sum.Sessions[0].Active || sum.Sessions[1].Active {
		t.Errorf("sessions = %+v", sum.Sessions)
	}
	if len(sum.Limits) != 1 || sum.Limits[0].Used == nil || *sum.Limits[0].Used != 18.5 || sum.Limits[0].DeviceID != "laptop" || sum.Limits[0].Expired {
		t.Errorf("limits = %+v", sum.Limits)
	}
	if len(sum.Sources) != 1 || sum.Sources[0].Status != "ok" {
		t.Errorf("sources = %+v", sum.Sources)
	}

	// Admins see the project's name and viewers don't.
	if sum.Sessions[0].Project != "secret-client" {
		t.Errorf("an admin sees project %q", sum.Sessions[0].Project)
	}
	var seen api.CodingSummary
	get(t, h, "/api/devices/laptop/coding?range=24h", phoneIP, &seen)
	if seen.Totals.Tokens.Total != sum.Totals.Tokens.Total || len(seen.Sessions) != 2 {
		t.Errorf("a viewer sees %+v", seen.Totals)
	}
	for _, s := range seen.Sessions {
		if s.Project != "" {
			t.Errorf("a viewer sees project %q", s.Project)
		}
	}
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/coding", tabletIP)); rec.Code != http.StatusForbidden {
		t.Errorf("a person with no role = %d, want 403", rec.Code)
	}

	// The fleet sums up every device.
	var fleet api.CodingSummary
	get(t, h, "/api/coding?range=7d&tz=Europe/London", phoneIP, &fleet)
	if fleet.Step != "day" || fleet.Timezone != "Europe/London" || fleet.Totals.Tokens.Total != 8_000_000 {
		t.Errorf("fleet = step %q tz %q totals %+v", fleet.Step, fleet.Timezone, fleet.Totals)
	}
	if len(fleet.Series) != 1 || len(fleet.Series[0].Points) != 7 {
		t.Errorf("fleet series = %+v", fleet.Series)
	}
	if len(fleet.Devices) != 1 || fleet.Devices[0].DeviceID != "laptop" || !fleet.Devices[0].Enabled || fleet.Devices[0].Totals.Sessions != 2 {
		t.Errorf("fleet devices = %+v", fleet.Devices)
	}

	// Turning it off stops reports and keeps what was counted.
	if rec := send(t, h, http.MethodPut, "/api/devices/laptop/coding", laptopIP, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("turning it off = %d", rec.Code)
	}
	rec = sendAs(t, h, http.MethodPost, "/api/agent/sessions", laptopIP, secret, codingReport(now, "claude", "s1", "claude-x", "standard"))
	if rec.Code != http.StatusConflict {
		t.Errorf("a report after turning it off = %d, want 409", rec.Code)
	}
	get(t, h, "/api/devices/laptop/coding?range=24h", laptopIP, &sum)
	if sum.Enabled || sum.Totals.Tokens.Total != 8_000_000 {
		t.Errorf("after turning it off: enabled %v tokens %d", sum.Enabled, sum.Totals.Tokens.Total)
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-6 && d > -1e-6 }

func TestCoding_refusals(t *testing.T) {
	t.Parallel()
	h, st := codingHandler(t)
	secret := enrolled(t, st, "laptop")
	if err := st.SetCoding(t.Context(), "laptop", true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	good := codingReport(now, "claude", "s1", "claude-x", "standard")
	tests := []struct {
		name   string
		from   netip.Addr
		secret string
		body   string
		want   int
	}{
		{"another machine's secret", phoneIP, secret, good, http.StatusForbidden},
		{"a stranger", strangerIP, secret, good, http.StatusForbidden},
		{"a secret Bield doesn't know", laptopIP, "unknown", good, http.StatusUnauthorized},
		{"another format", laptopIP, secret, `{"v":2}`, http.StatusBadRequest},
		{"not JSON", laptopIP, secret, `{"v":1`, http.StatusBadRequest},
		{"a malformed row", laptopIP, secret, `{"v":1,"usage":[{"tool":"Claude Code"}]}`, http.StatusBadRequest},
		{"a field Bield doesn't know", laptopIP, secret, `{"v":1,"color":"red"}`, http.StatusNoContent},
	}
	for _, tt := range tests {
		if rec := sendAs(t, h, http.MethodPost, "/api/agent/sessions", tt.from, tt.secret, tt.body); rec.Code != tt.want {
			t.Errorf("%s: POST = %d %s, want %d", tt.name, rec.Code, rec.Body, tt.want)
		}
	}
	for _, target := range []string{"/api/coding?range=2d", "/api/devices/laptop/coding?range=2d"} {
		if rec := serve(h, newRequest(t, http.MethodGet, target, laptopIP)); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, rec.Code)
		}
	}
	// An unknown time zone is UTC.
	var sum api.CodingSummary
	get(t, h, "/api/coding?range=7d&tz=Mars/Olympus", laptopIP, &sum)
	if sum.Timezone != "UTC" {
		t.Errorf("timezone = %q, want UTC", sum.Timezone)
	}
}

// The machine Bield runs on is refused heartbeats when Bield measures it, but
// it can still say how its coding tools are going, so the refusal tells its
// agent whether to.
func TestCoding_hub(t *testing.T) {
	t.Parallel()
	prices, _ := pricing.Parse([]byte(codingPrices))
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{Prices: prices})
	opts.Feed = api.NewFeed(api.FeedOptions{
		Tailnet: opts.Snapshot, Store: st, Agents: opts.Agents,
		Host: func() hostmetrics.Reading { return hostmetrics.Reading{} },
	})
	h := api.New(opts)
	secret := enrolled(t, st, "bield")

	rec := sendBeat(t, h, bieldIP, secret, laptopBeat)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("a heartbeat from the hub = %d %s, want 409 that says coding is off", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPut, "/api/devices/bield/coding", laptopIP, `{"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("turning it on for the hub = %d %s", rec.Code, rec.Body)
	}
	rec = sendBeat(t, h, bieldIP, secret, laptopBeat)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"enabled":true`) || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("a heartbeat from the hub = %d %s, want 409 that says coding is on", rec.Code, rec.Body)
	}
	rec = sendAs(t, h, http.MethodPost, "/api/agent/sessions", bieldIP, secret, codingReport(time.Now(), "codex", "s1", "gpt-x", "standard"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("a report from the hub = %d %s, want 204", rec.Code, rec.Body)
	}
	var sum api.CodingSummary
	get(t, h, "/api/devices/bield/coding?range=24h", laptopIP, &sum)
	// A million tokens of each kind at 2, 8, 0.2 and 2 dollars.
	if want := 2 + 8 + 0.2 + 2; !near(sum.Totals.Cost.Total, want) || sum.Tools[0].Tool != "codex" {
		t.Errorf("cost = %v, want %v; tools %+v", sum.Totals.Cost.Total, want, sum.Tools)
	}
}

// A report that is more than a day ahead, or older than what Bield keeps, is
// left out, and so is what a model has no price for, as a cost of nothing.
func TestCoding_unpricedAndOld(t *testing.T) {
	t.Parallel()
	h, st := codingHandler(t)
	secret := enrolled(t, st, "laptop")
	if err := st.SetCoding(t.Context(), "laptop", true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	hour := now.UTC().Add(-time.Hour).Format("2006-01-02T15")
	body := `{"v":1,"usage":[
	 {"tool":"opencode","session":"s","model":"mystery-model","speed":"standard","hour":"` + hour + `","in":10,"cr":0,"cw":0,"out":5,"reason":0,"n":1,"cost":0},
	 {"tool":"opencode","session":"s","model":"paid-model","speed":"standard","hour":"` + hour + `","in":10,"cr":0,"cw":0,"out":5,"reason":0,"n":1,"cost":0.25},
	 {"tool":"opencode","session":"s","model":"claude-x","speed":"standard","hour":"2020-01-01T00","in":10,"cr":0,"cw":0,"out":5,"reason":0,"n":1,"cost":0}]}`
	if rec := sendAs(t, h, http.MethodPost, "/api/agent/sessions", laptopIP, secret, body); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	var sum api.CodingSummary
	get(t, h, "/api/devices/laptop/coding?range=24h", laptopIP, &sum)
	if len(sum.Models) != 2 {
		t.Fatalf("models = %+v", sum.Models)
	}
	// The one with a reported cost sorts first, and the one that is
	// unpriced says so.
	if m := sum.Models[0]; m.Model != "paid-model" || !near(m.Cost, 0.25) || !m.Priced {
		t.Errorf("Models[0] = %+v", m)
	}
	if m := sum.Models[1]; m.Model != "mystery-model" || m.Priced || m.Cost != 0 {
		t.Errorf("Models[1] = %+v", m)
	}
	if sum.Totals.UnpricedTokens != 15 || !near(sum.Totals.Cost.Total, 0.25) {
		t.Errorf("Totals = %+v", sum.Totals)
	}
}
