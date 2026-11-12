package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// POST /api/coding/watch turns watching on for every machine that has an
// agent, and off for every machine, with one line in the activity log.
func TestCoding_watchAll(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	ctx := t.Context()
	enrolled(t, st, "laptop")
	enrolled(t, st, "server")
	// The server is watched already, and the phone, which has no agent, is
	// watched from before.
	for _, id := range []string{"server", "phone"} {
		if err := st.SetCoding(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}

	var sum api.CodingSummary
	get(t, h, "/api/coding?range=24h", phoneIP, &sum)
	if sum.Unwatched != 1 {
		t.Errorf("unwatched = %d, want the laptop", sum.Unwatched)
	}

	if rec := send(t, h, http.MethodPost, "/api/coding/watch", phoneIP, `{"enabled":true}`); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer watching every machine = %d, want 403", rec.Code)
	}
	for _, tt := range []struct {
		name    string
		body    string
		changed int
		watched []string
	}{
		{"on", `{"enabled":true}`, 1, []string{"laptop", "phone", "server"}},
		{"on again", `{"enabled":true}`, 0, []string{"laptop", "phone", "server"}},
		{"off", `{"enabled":false}`, 3, nil},
		{"off again", `{"enabled":false}`, 0, nil},
	} {
		rec := send(t, h, http.MethodPost, "/api/coding/watch", laptopIP, tt.body)
		var got api.WatchedCoding
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Changed != tt.changed {
			t.Fatalf("%s: %d %s, want %d changed", tt.name, rec.Code, rec.Body, tt.changed)
		}
		on, err := st.CodingDevices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var watched []string
		for id, yes := range on {
			if yes {
				watched = append(watched, id)
			}
		}
		slices.Sort(watched)
		if !slices.Equal(watched, tt.watched) {
			t.Errorf("%s: watched = %q, want %q", tt.name, watched, tt.watched)
		}
		if tt.name == "on" {
			get(t, h, "/api/coding?range=24h", phoneIP, &sum)
			if sum.Unwatched != 0 {
				t.Errorf("after watching every machine, unwatched = %d", sum.Unwatched)
			}
		}
	}
	want := []string{"device: Turned on watching the AI tools on 1 machine", "device: Turned off watching the AI tools on 3 machines"}
	if got := eventMessages(t, st); !slices.Equal(got, want) {
		t.Errorf("the activity log = %q, want %q", got, want)
	}
}
