package api_test

import (
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// TestRoutes walks every route New registers. Each one must say who can use
// it, a route that changes things must be for admins only, unless it's for
// agents, and each must let exactly those callers through.
func TestRoutes(t *testing.T) {
	t.Parallel()
	const (
		anyone = iota + 1
		viewers
		admins
		agentSecret
		enrollCode
	)
	levels := map[string]int{
		"anyone": anyone, "viewers": viewers, "admins": admins,
		"agent secret": agentSecret, "enrollment code": enrollCode,
	}
	type callerSpec struct {
		name  string
		from  netip.Addr
		level int
		// token is the API token the caller sends: "viewer" or "admin" for
		// one of alice's with that role, "bob's admin" for one bob made as
		// an admin before he became a viewer, and "carol's" for one carol
		// made before she lost her role.
		token string
	}
	callers := []callerSpec{
		{"an unknown device", strangerIP, anyone, ""},
		{"a tagged device", serverIP, anyone, ""},
		{"Bield's own node", bieldIP, anyone, ""},
		{"carol, who has no role", tabletIP, anyone, ""},
		{"bob, a viewer", phoneIP, viewers, ""},
		{"alice, an admin", laptopIP, admins, ""},
		{"a viewer token on a tagged device", serverIP, viewers, "viewer"},
		{"an admin token on Bield's own node", bieldIP, admins, "admin"},
		{"an admin token on bob's phone", phoneIP, admins, "admin"},
		{"bob's admin token, now a viewer's", serverIP, viewers, "bob's admin"},
		{"carol's token, who has no role", serverIP, anyone, "carol's"},
	}
	wildcard := regexp.MustCompile(`\{[^}]*\}`)

	for _, devUI := range []bool{false, true} {
		st := testStore(t)
		apiTokens := map[string]string{
			"viewer":      newToken(t, st, "v", store.Viewer, alice),
			"admin":       newToken(t, st, "a", store.Admin, alice),
			"bob's admin": newToken(t, st, "b", store.Admin, bob),
			"carol's":     newToken(t, st, "c", store.Viewer, carol),
		}
		opts := withStore(t, testTailnet(), st, api.Options{})
		if devUI {
			opts.DevUI = &url.URL{Scheme: "http", Host: "127.0.0.1:5173"}
		}
		h, routes := api.StubRoutes(opts)
		if !slices.ContainsFunc(routes, func(r api.Route) bool { return r.Pattern == "GET /api/me" }) {
			t.Fatalf("StubRoutes() = %v, want GET /api/me among them", routes)
		}
		// The laptop's agent has a secret, and a code no machine has used
		// yet is good for any.
		tokens := map[int]string{agentSecret: enrolled(t, st, "laptop"), enrollCode: newCode(t, st, time.Now())}

		for _, r := range routes {
			need, ok := levels[r.Level]
			if !ok {
				t.Errorf("%s has no level, so no one can use it; register it with a level", r.Pattern)
				continue
			}
			method, path, ok := strings.Cut(r.Pattern, " ")
			methods := []string{method}
			if !ok {
				// A pattern without a method matches all of them.
				path = r.Pattern
				methods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete}
			} else if method != http.MethodGet && need != admins && need != agentSecret && need != enrollCode {
				t.Errorf("%s changes things, so it must be for admins, not %s", r.Pattern, r.Level)
			}
			target := wildcard.ReplaceAllString(path, "x")

			if token, ok := tokens[need]; ok {
				// Only the token counts, whatever the caller's role, and only
				// from a machine Tailscale knows. An agent's secret counts
				// only from the machine it enrolled on.
				for _, c := range callers {
					if c.token != "" {
						continue
					}
					for _, auth := range []string{"", "Bearer wrong", "Bearer " + token} {
						req := newRequest(t, method, target, c.from)
						req.Header.Set(api.CSRFHeader, "1")
						if auth != "" {
							req.Header.Set("Authorization", auth)
						}
						want := http.StatusUnauthorized
						switch {
						case auth != "Bearer "+token:
						case c.from == strangerIP, need == agentSecret && c.from != laptopIP:
							want = http.StatusForbidden
						default:
							want = http.StatusNoContent
						}
						if rec := serve(h, req); rec.Code != want {
							t.Errorf("%s from %s with %q = %d, want %d", r.Pattern, c.name, auth, rec.Code, want)
						}
					}
				}
				continue
			}

			for _, m := range methods {
				want := need
				if m != http.MethodGet && m != http.MethodHead {
					want = admins
				}
				for _, c := range callers {
					req := newRequest(t, m, target, c.from)
					req.Header.Set(api.CSRFHeader, "1")
					if c.token != "" {
						req.Header.Set("Authorization", "Bearer "+apiTokens[c.token])
					}
					rec := serve(h, req)
					allowed := c.level >= want && (c.token == "" || !r.People)
					switch {
					case allowed && rec.Code != http.StatusNoContent:
						t.Errorf("dev UI %v: %s %s from %s = %d %q, want it let through",
							devUI, m, target, c.name, rec.Code, rec.Body)
					case !allowed && rec.Code != http.StatusForbidden:
						t.Errorf("dev UI %v: %s %s from %s = %d, want 403", devUI, m, target, c.name, rec.Code)
					}
				}
			}
		}
	}
}
