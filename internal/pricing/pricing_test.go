package pricing_test

import (
	"math"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/pricing"
)

const testList = `{"asOf":"2026-11-06","models":{
 "Claude-Opus-5-5":{"in":4,"out":20,"cacheRead":0.2,"cacheWrite":5,"fast":{"in":8,"out":40,"cacheRead":0.4,"cacheWrite":10}},
 "claude-sonnet-4-5":{"in":3,"out":15,"cacheRead":0.3,"cacheWrite":3.75},
 "gpt-5.6-sol":{"in":4,"out":20,"cacheRead":0.4,"cacheWrite":5},
 "gemini-3.1-pro-preview":{"in":2,"out":12,"cacheRead":0.2,"cacheWrite":2},
 "gpt-6.1-sol":{"in":2,"out":10,"cacheRead":0.1,"cacheWrite":2.5,"fast":{"in":4,"out":20,"cacheRead":0.2,"cacheWrite":5},
  "ultrafast":{"in":12,"out":60,"cacheRead":0.6,"cacheWrite":15}}
}}`

func table(t *testing.T) *pricing.Table {
	t.Helper()
	tb, err := pricing.Parse([]byte(testList))
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func TestDefault(t *testing.T) {
	t.Parallel()
	tb := pricing.Default()
	if len(tb.Models) < 500 {
		t.Errorf("the built-in list has %d models, want at least 500", len(tb.Models))
	}
	for _, name := range []string{"claude-sonnet-4-5", "gpt-4o"} {
		if _, ok := tb.Lookup(name); !ok {
			t.Errorf("the built-in list has no %s", name)
		}
	}
	for name, r := range tb.Models {
		if r.Input < 0 || r.Output < 0 || r.CacheRead < 0 || r.CacheWrite < 0 || math.IsNaN(r.Input+r.Output) {
			t.Errorf("%s has a rate below zero: %+v", name, r)
		}
		if r.Fast != nil && r.Fast.Input < r.Input {
			t.Errorf("%s is cheaper in its fast tier: %+v", name, r)
		}
		if r.Ultrafast != nil && r.Ultrafast.Input < r.Input {
			t.Errorf("%s is cheaper in its ultrafast tier: %+v", name, r)
		}
	}
	if tb.AsOf == "" {
		t.Error("the built-in list doesn't say when it was taken")
	}
}

func TestParse_errors(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{"not JSON": "{", "no models": `{"asOf":"x","models":{}}`} {
		if _, err := pricing.Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%s) succeeded", name)
		}
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	tb := table(t)
	tests := []struct {
		model string
		ok    bool
		in    float64
	}{
		{"claude-opus-5-5", true, 4},
		{"CLAUDE-OPUS-5-5", true, 4},
		{"claude-opus-5-5[1m]", true, 4},
		{"anthropic/claude-opus-5-5", true, 4},
		{"claude-opus-5-5@20251101", true, 4},
		{"claude-sonnet-4-5-20250929", true, 3},
		{"claude-sonnet-4-5-2025-09-29", true, 3},
		{"openrouter/openai/gpt-5.6-sol", true, 4},
		{"gemini-3.1-pro", true, 2},
		{"gemini-3.1-pro-preview", true, 2},
		{"<synthetic>", false, 0},
		{"opus", false, 0},
		{"anthropic/sonnet", false, 0},
		{"", false, 0},
		{"unknown-model", false, 0},
	}
	for _, tt := range tests {
		got, ok := tb.Lookup(tt.model)
		if ok != tt.ok || (ok && got.Input != tt.in) {
			t.Errorf("Lookup(%q) = %+v, %v; want input %v, %v", tt.model, got, ok, tt.in, tt.ok)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPrice(t *testing.T) {
	t.Parallel()
	tb := table(t)
	// A million of each kind makes the cost the sum of the rates.
	million := pricing.Tokens{Input: 1e6, CacheRead: 1e6, CacheWrite: 1e6, Output: 1e6}

	t.Run("standard", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("claude-opus-5-5", pricing.Standard, million, 0)
		if !near(p.Total, 4+0.2+5+20) || !near(p.Input, 4) || !near(p.CacheRead, 0.2) || !near(p.CacheWrite, 5) || !near(p.Output, 20) {
			t.Errorf("Price = %+v", p)
		}
		if p.Source != pricing.ModelPriced || p.SpeedPremium != 0 {
			t.Errorf("source %q premium %v", p.Source, p.SpeedPremium)
		}
		// The cache saved the input rate less the cache rate on a million tokens.
		if !near(p.CacheSavings, 4-0.2) {
			t.Errorf("CacheSavings = %v", p.CacheSavings)
		}
	})
	t.Run("fast tier costs more and says by how much", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("claude-opus-5-5", pricing.Fast, million, 0)
		if !near(p.Total, 2*(4+0.2+5+20)) || !near(p.SpeedPremium, 4+0.2+5+20) {
			t.Errorf("Price = %+v", p)
		}
	})
	t.Run("a model without a fast tier is priced as standard", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("gpt-5.6-sol", pricing.Fast, million, 0)
		if !near(p.Total, 4+0.4+5+20) || p.SpeedPremium != 0 {
			t.Errorf("Price = %+v", p)
		}
	})
	t.Run("ultrafast has rates of its own", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("gpt-6.1-sol", pricing.Ultrafast, million, 0)
		if !near(p.Total, 12+0.6+15+60) || !near(p.SpeedPremium, (12+0.6+15+60)-(2+0.1+2.5+10)) {
			t.Errorf("Price = %+v", p)
		}
	})
	t.Run("a model without ultrafast rates is priced as fast", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("claude-opus-5-5", pricing.Ultrafast, million, 0)
		if !near(p.Total, 2*(4+0.2+5+20)) {
			t.Errorf("Price = %+v", p)
		}
		p = tb.Price("gpt-5.6-sol", pricing.Ultrafast, million, 0)
		if !near(p.Total, 4+0.4+5+20) {
			t.Errorf("without either tier: Price = %+v", p)
		}
	})
	t.Run("a reported cost wins, and the list splits it", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("claude-opus-5-5", pricing.Standard, million, 14.645)
		if p.Source != pricing.Reported || !near(p.Total, 14.645) {
			t.Fatalf("Price = %+v", p)
		}
		if !near(p.Input+p.CacheRead+p.CacheWrite+p.Output, 14.645) {
			t.Errorf("the kinds add up to %v", p.Input+p.CacheRead+p.CacheWrite+p.Output)
		}
		if !near(p.Output/p.Input, 5) {
			t.Errorf("the split doesn't follow the rates: %+v", p)
		}
	})
	t.Run("a reported cost for a model that isn't listed", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("some-new-model", pricing.Standard, million, 1.5)
		if p.Source != pricing.Reported || !near(p.Total, 1.5) || p.Input != 0 {
			t.Errorf("Price = %+v", p)
		}
	})
	t.Run("no price at all", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("some-new-model", pricing.Standard, million, 0)
		if p.Source != pricing.Unpriced || p.Total != 0 {
			t.Errorf("Price = %+v", p)
		}
	})
	t.Run("a model that is listed but a zero total with a report", func(t *testing.T) {
		t.Parallel()
		p := tb.Price("claude-opus-5-5", pricing.Standard, pricing.Tokens{}, 2)
		if p.Source != pricing.Reported || !near(p.Total, 2) {
			t.Errorf("Price = %+v", p)
		}
	})
}

func TestPrice_add(t *testing.T) {
	t.Parallel()
	a := pricing.Price{Total: 1, Input: 1, CacheSavings: 2}
	a.Add(pricing.Price{Total: 3, Output: 3, SpeedPremium: 1})
	if a.Total != 4 || a.Input != 1 || a.Output != 3 || a.SpeedPremium != 1 || a.CacheSavings != 2 {
		t.Errorf("Add = %+v", a)
	}
}
