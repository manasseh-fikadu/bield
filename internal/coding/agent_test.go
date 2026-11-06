package coding_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/coding"
)

// The reports that the agent's tests check against (agent/testdata) are what a
// real agent sends, so the hub has to take every one of them whole: nothing
// malformed, nothing left out, and the same totals.
func TestClean_takesWhatTheAgentSends(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "..", "agent", "testdata", "sessions", "*.golden.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden reports from the agent: %v", err)
	}
	// The made-up sessions are all in November 2026, and Bield looks at them as
	// that month's end, whatever day the test runs.
	at := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(file) //nolint:gosec // G304: a file in the agent's testdata.
			if err != nil {
				t.Fatal(err)
			}
			var r coding.Report
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			got, err := r.Clean(at, at.Add(-365*24*time.Hour))
			if err != nil {
				t.Fatalf("Clean error = %v", err)
			}
			if len(got.Usage) != len(r.Usage) || len(got.Sessions) != len(r.Sessions) ||
				len(got.Limits) != len(r.Limits) || len(got.Tools) != len(r.Tools) {
				t.Fatalf("Clean kept %d/%d/%d/%d of %d/%d/%d/%d usage rows, sessions, limits and tools",
					len(got.Usage), len(got.Sessions), len(got.Limits), len(got.Tools),
					len(r.Usage), len(r.Sessions), len(r.Limits), len(r.Tools))
			}
			var wantTokens, gotTokens int64
			for _, u := range r.Usage {
				wantTokens += u.In + u.CR + u.CW + u.Out
			}
			for _, u := range got.Usage {
				gotTokens += u.Input + u.CacheRead + u.CacheWrite + u.Output
			}
			if wantTokens != gotTokens || wantTokens == 0 {
				t.Errorf("tokens = %d, want %d", gotTokens, wantTokens)
			}
		})
	}
}
