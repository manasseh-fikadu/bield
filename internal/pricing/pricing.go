// Package pricing turns the tokens a coding agent used into dollars. It holds
// a list of what model makers charge per million tokens, taken from the
// LiteLLM project's price list (MIT license), which `make prices` refreshes,
// and prices a count of tokens with it.
//
// The result is an estimate of what the same tokens would cost on the maker's
// API, which is what the dashboard calls an API estimate. A subscription
// doesn't bill per token, and a tool that knows its own bill says so (see
// Price).
package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

//go:embed prices.json
var embedded []byte

// Rates are what one model costs in dollars per million tokens.
type Rates struct {
	Input      float64 `json:"in"`
	Output     float64 `json:"out"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	// Fast, if the model has a faster, dearer tier, is what that costs.
	Fast *Rates `json:"fast,omitempty"`
}

// A Table is a price list.
type Table struct {
	// AsOf is the day the list was taken, such as 2026-11-06.
	AsOf   string           `json:"asOf"`
	Models map[string]Rates `json:"models"`
}

var (
	defaultOnce  sync.Once
	defaultTable *Table
)

// Default returns the price list that is built into Bield.
func Default() *Table {
	defaultOnce.Do(func() {
		t, err := Parse(embedded)
		if err != nil {
			// The list is embedded and checked by a test, so this is a
			// build that was never meant to ship.
			panic("pricing: the built-in price list is broken: " + err.Error())
		}
		defaultTable = t
	})
	return defaultTable
}

// Parse reads a price list as `make prices` writes it.
func Parse(data []byte) (*Table, error) {
	var t Table
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("read the price list: %w", err)
	}
	if len(t.Models) == 0 {
		return nil, fmt.Errorf("read the price list: it has no models")
	}
	// Names are looked up in lower case.
	lower := make(map[string]Rates, len(t.Models))
	for name, r := range t.Models {
		lower[strings.ToLower(name)] = r
	}
	t.Models = lower
	return &t, nil
}

// Speed is the tier a request was billed at.
const (
	Standard = "standard"
	Fast     = "fast"
)

// unpriceable are the names that say nothing about the model. Claude Code
// writes <synthetic> for a message that never reached the API, and the bare
// family names fit more than one generation, so none of them has a price.
var unpriceable = map[string]bool{
	"<synthetic>": true, "synthetic": true, "opus": true, "sonnet": true, "haiku": true, "fable": true,
}

// A dated name such as claude-sonnet-4-5-20250929 or gpt-5-2025-08-07, for
// when the list only has the undated one.
var dateSuffix = regexp.MustCompile(`-(?:\d{8}|\d{4}-\d{2}-\d{2})$`)

// Lookup returns the rates of a model, by the name a tool wrote down. It
// tries the name as it is, without a provider in front ("anthropic/claude-x"
// and "claude-x@vertex" are claude-x) or a bracketed variant ("claude-x[1m]",
// which is priced as the base model), and without a date at the end.
func (t *Table) Lookup(model string) (Rates, bool) {
	name := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return Rates{}, false
	}
	bare := name
	if i := strings.LastIndexByte(bare, '/'); i >= 0 {
		bare = bare[i+1:]
	}
	if i := strings.IndexByte(bare, '@'); i >= 0 {
		bare = bare[:i]
	}
	if bare == "" || unpriceable[bare] {
		return Rates{}, false
	}
	for _, key := range []string{name, bare, dateSuffix.ReplaceAllString(bare, "")} {
		if r, ok := t.Models[key]; ok {
			return r, true
		}
	}
	return Rates{}, false
}

// Tokens is a count of tokens of one kind of request. Input is only what the
// cache didn't serve, and Output includes the reasoning.
type Tokens struct {
	Input, CacheRead, CacheWrite, Output int64
}

// Source says where a price came from.
type Source string

// The sources of a price.
const (
	// ModelPriced is the tokens times the list's rates.
	ModelPriced Source = "priced"
	// Reported is what the tool itself said it cost.
	Reported Source = "reported"
	// Unpriced means the model isn't on the list and the tool gave no cost,
	// so the tokens count for nothing.
	Unpriced Source = "unpriced"
)

// Price is what some tokens cost, in dollars, in total and by kind of token.
type Price struct {
	Total, Input, CacheRead, CacheWrite, Output float64
	// SpeedPremium is how much of Total the fast tier added.
	SpeedPremium float64
	// CacheSavings is what the cache saved: the cached tokens at the input
	// rate, less what they cost at the cache rate. It's not part of Total.
	CacheSavings float64
	Source       Source
}

// Add adds q to p. The source of a sum is Unpriced only if both are, and
// Reported only if both are, since the rest is a mix that the sum can't name.
func (p *Price) Add(q Price) {
	p.Total += q.Total
	p.Input += q.Input
	p.CacheRead += q.CacheRead
	p.CacheWrite += q.CacheWrite
	p.Output += q.Output
	p.SpeedPremium += q.SpeedPremium
	p.CacheSavings += q.CacheSavings
}

// Price prices tokens of a model at a speed. reported is the cost in dollars
// that the tool wrote down, or 0. A tool that knows what it billed beats the
// list, and the list still splits that bill by kind of token. A tool that
// writes 0 for a cost it doesn't know, such as OpenCode for a model on a
// subscription, is the same as none.
func (t *Table) Price(model, speed string, tk Tokens, reported float64) Price {
	rates, ok := t.Lookup(model)
	if !ok {
		if reported > 0 {
			return Price{Total: reported, Source: Reported}
		}
		return Price{Source: Unpriced}
	}
	at := rates
	if speed != Standard && speed != "" && rates.Fast != nil {
		at = *rates.Fast
	}
	listed := byKind(tk, at)
	base := byKind(tk, rates)
	listedTotal := listed.Input + listed.CacheRead + listed.CacheWrite + listed.Output
	baseTotal := base.Input + base.CacheRead + base.CacheWrite + base.Output
	p := Price{
		Total: listedTotal, Input: listed.Input, CacheRead: listed.CacheRead,
		CacheWrite: listed.CacheWrite, Output: listed.Output,
		SpeedPremium: listedTotal - baseTotal,
		CacheSavings: float64(tk.CacheRead) * (at.Input - at.CacheRead) / 1e6,
		Source:       ModelPriced,
	}
	if reported <= 0 {
		return p
	}
	if listedTotal <= 0 {
		return Price{Total: reported, Source: Reported}
	}
	scale := reported / listedTotal
	p.Total = reported
	p.Input *= scale
	p.CacheRead *= scale
	p.CacheWrite *= scale
	p.Output *= scale
	p.SpeedPremium *= scale
	p.Source = Reported
	return p
}

func byKind(tk Tokens, r Rates) Price {
	return Price{
		Input:      float64(tk.Input) * r.Input / 1e6,
		CacheRead:  float64(tk.CacheRead) * r.CacheRead / 1e6,
		CacheWrite: float64(tk.CacheWrite) * r.CacheWrite / 1e6,
		Output:     float64(tk.Output) * r.Output / 1e6,
	}
}
