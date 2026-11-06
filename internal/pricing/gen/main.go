// Command gen writes internal/pricing/prices.json from LiteLLM's price list
// (https://github.com/BerriAI/litellm, MIT license). Run it with `make prices`.
//
// LiteLLM lists every model once per cloud and reseller that sells it, under
// names such as "bedrock/eu.anthropic.claude-x" and "openrouter/anthropic/x".
// The tools Bield reads write the model's own name, so the list that comes
// out has one entry per bare name, priced by the maker's own API where the
// maker is known, and left out where resellers disagree.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/pricing"
)

const source = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// firstParty are the providers that sell their own models. Their price beats a
// cloud's or a reseller's for the same bare name.
var firstParty = map[string]bool{
	"anthropic": true, "openai": true, "text-completion-openai": true, "gemini": true,
	"xai": true, "deepseek": true, "mistral": true, "moonshot": true, "zai": true, "minimax": true,
}

// cloudKey matches the names that clouds give a model, which no tool writes.
var cloudKey = regexp.MustCompile(`^(?:(?:us|eu|apac|au|jp|ca|global|us-gov)\.|` +
	`(?:anthropic|amazon|meta|mistral|cohere|ai21|deepseek|openai|qwen|writer|moonshot|minimax|zai|google|nvidia|twelvelabs|stability)\.[a-z])`)

func main() {
	in := flag.String("in", "", "read LiteLLM's list from this file instead of the web")
	out := flag.String("out", "internal/pricing/prices.json", "write the list here")
	flag.Parse()
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	raw, err := read(in)
	if err != nil {
		return err
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("read LiteLLM's list: %w", err)
	}
	table := pricing.Table{AsOf: time.Now().UTC().Format("2006-01-02"), Models: resolve(doc)}
	body, err := json.MarshalIndent(table, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil { //nolint:gosec // a source file in the repo.
		return err
	}
	fmt.Printf("wrote %d models to %s\n", len(table.Models), out)
	return nil
}

func read(path string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path) //nolint:gosec // G304: a path someone gave to a command they run.
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch LiteLLM's list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch LiteLLM's list: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

type candidate struct {
	rates      pricing.Rates
	firstParty bool
	// ambiguous is set when two sources of the same rank disagree.
	ambiguous bool
}

// resolve gives each bare model name its rates.
func resolve(doc map[string]map[string]any) map[string]pricing.Rates {
	cands := map[string]*candidate{}
	for key, entry := range doc {
		key = strings.ToLower(key)
		if key == "sample_spec" || cloudKey.MatchString(key) || !chatMode(entry) {
			continue
		}
		rates, ok := ratesOf(entry)
		if !ok {
			continue
		}
		bare := key
		if i := strings.LastIndexByte(bare, '/'); i >= 0 {
			bare = bare[i+1:]
		}
		if bare == "" {
			continue
		}
		provider, _ := entry["litellm_provider"].(string)
		fp := firstParty[provider] || firstParty[strings.SplitN(key, "/", 2)[0]]
		held := cands[bare]
		switch {
		case held == nil || (fp && !held.firstParty):
			cands[bare] = &candidate{rates: rates, firstParty: fp}
		case fp == held.firstParty && !sameRates(held.rates, rates):
			// Resellers that omit a tier fill from the other entry, and a
			// real disagreement is left out rather than guessed.
			merged, ok := merge(held.rates, rates)
			if ok {
				held.rates = merged
			} else {
				held.ambiguous = true
			}
		}
	}
	out := make(map[string]pricing.Rates, len(cands))
	for name, c := range cands {
		if !c.ambiguous {
			out[name] = c.rates
		}
	}
	return out
}

func chatMode(entry map[string]any) bool {
	mode, _ := entry["mode"].(string)
	return mode == "" || mode == "chat" || mode == "completion" || mode == "responses"
}

func number(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && f >= 0
}

// perMillion is a cost per token as dollars per million tokens, without the
// noise of binary fractions.
func perMillion(perToken float64) float64 {
	return float64(int64(perToken*1e12+0.5)) / 1e6
}

func ratesOf(entry map[string]any) (pricing.Rates, bool) {
	std, ok := tier(entry, "", nil)
	if !ok {
		return pricing.Rates{}, false
	}
	if fast := fastMultiplier(entry); fast > 0 {
		std.Fast = &pricing.Rates{
			Input: round(std.Input * fast), Output: round(std.Output * fast),
			CacheRead: round(std.CacheRead * fast), CacheWrite: round(std.CacheWrite * fast),
		}
	} else if p, ok := tier(entry, "_priority", &std); ok {
		std.Fast = &p
	}
	return std, true
}

func round(v float64) float64 { return float64(int64(v*1e6+0.5)) / 1e6 }

// tier reads the rates with a suffix on their names. A cache rate that is
// left out is the input rate's share of the standard tier's, or the input
// rate itself.
func tier(entry map[string]any, suffix string, std *pricing.Rates) (pricing.Rates, bool) {
	in, ok1 := number(entry["input_cost_per_token"+suffix])
	out, ok2 := number(entry["output_cost_per_token"+suffix])
	if !ok1 || !ok2 {
		return pricing.Rates{}, false
	}
	cache := func(name string, standard func(pricing.Rates) float64) float64 {
		if v, ok := number(entry[name+suffix]); ok {
			return perMillion(v)
		}
		if std != nil && std.Input > 0 {
			return round(standard(*std) / std.Input * perMillion(in))
		}
		return perMillion(in)
	}
	return pricing.Rates{
		Input: perMillion(in), Output: perMillion(out),
		CacheRead:  cache("cache_read_input_token_cost", func(r pricing.Rates) float64 { return r.CacheRead }),
		CacheWrite: cache("cache_creation_input_token_cost", func(r pricing.Rates) float64 { return r.CacheWrite }),
	}, true
}

func fastMultiplier(entry map[string]any) float64 {
	specific, ok := entry["provider_specific_entry"].(map[string]any)
	if !ok {
		return 0
	}
	f, _ := number(specific["fast"])
	return f
}

func sameRates(a, b pricing.Rates) bool {
	if a.Input != b.Input || a.Output != b.Output || a.CacheRead != b.CacheRead || a.CacheWrite != b.CacheWrite {
		return false
	}
	if (a.Fast == nil) != (b.Fast == nil) {
		return false
	}
	return a.Fast == nil || *a.Fast == *b.Fast
}

// merge combines two entries for one model that differ only in a missing
// fast tier.
func merge(a, b pricing.Rates) (pricing.Rates, bool) {
	base := func(r pricing.Rates) pricing.Rates { r.Fast = nil; return r }
	if base(a) != base(b) {
		return a, false
	}
	switch {
	case a.Fast == nil:
		a.Fast = b.Fast
	case b.Fast != nil && *a.Fast != *b.Fast:
		return a, false
	}
	return a, true
}
