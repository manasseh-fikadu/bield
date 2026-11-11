package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// newToken returns a new API token called name with role, made by the admin
// with login by, that works for a day.
func newToken(t *testing.T, st *store.Store, name string, role store.Role, by string) string {
	t.Helper()
	now := time.Now()
	secret, _, err := st.NewToken(t.Context(), name, role, by, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

// withToken sends a request from the device at from with an API token, and a
// JSON body if body isn't "".
func withToken(t *testing.T, h http.Handler, method, target string, from netip.Addr, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, target, from)
	if body != "" {
		req = newRequestBody(t, method, target, from, strings.NewReader(body))
	}
	req.Header.Set(api.CSRFHeader, "1")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", api.CLIUserAgent+"/test")
	return serve(h, req)
}

func TestTokens_make(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))

	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"name": ""}`, http.StatusBadRequest},
		{`{"name": "has space"}`, http.StatusBadRequest},
		{`{"name": "-dash"}`, http.StatusBadRequest},
		{`{"name": "` + strings.Repeat("x", 41) + `"}`, http.StatusBadRequest},
		{`{"name": "ci", "role": "owner"}`, http.StatusBadRequest},
		{`{"name": "ci", "days": 366}`, http.StatusBadRequest},
		{`{"name": "ci", "days": -1}`, http.StatusBadRequest},
		{`{"name": "ci", "secret": "mine"}`, http.StatusBadRequest},
	} {
		if rec := send(t, h, http.MethodPost, "/api/tokens", laptopIP, tc.body); rec.Code != tc.want {
			t.Errorf("POST /api/tokens %s = %d %s, want %d", tc.body, rec.Code, rec.Body, tc.want)
		}
	}

	before := time.Now()
	rec := send(t, h, http.MethodPost, "/api/tokens", laptopIP, `{"name": "ci.deploy_1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/tokens = %d %s, want 201", rec.Code, rec.Body)
	}
	var made api.NewAPITokenResult
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	tok := made.Token
	if !strings.HasPrefix(made.Secret, store.TokenPrefix) || !strings.HasSuffix(made.Secret, tok.Hint) {
		t.Errorf("secret = %q, hint %q; want a bield_ token that ends in the hint", made.Secret, tok.Hint)
	}
	if tok.Role != api.RoleViewer || tok.ActsAs != api.RoleViewer || tok.CreatedBy != alice || tok.Expired {
		t.Errorf("token = %+v, want a viewer token of alice's", tok)
	}
	if days := tok.ExpiresAt.Sub(before).Hours() / 24; days < 29.9 || days > 30.1 {
		t.Errorf("the token works for %.1f days, want %d", days, api.DefaultTokenDays)
	}
	if rec := send(t, h, http.MethodPost, "/api/tokens", laptopIP, `{"name": "CI.DEPLOY_1", "role": "admin"}`); rec.Code != http.StatusConflict {
		t.Errorf("POST /api/tokens with a name in use = %d, want 409", rec.Code)
	}
	rec = send(t, h, http.MethodPost, "/api/tokens", laptopIP, `{"name": "backup", "role": "admin", "days": 365}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/tokens for an admin = %d %s", rec.Code, rec.Body)
	}

	// The list never has the token itself.
	rec = serve(h, newRequest(t, http.MethodGet, "/api/tokens", laptopIP))
	if strings.Contains(rec.Body.String(), made.Secret) || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("GET /api/tokens = %s, which has the token in it", rec.Body)
	}
	var list []api.APIToken
	get(t, h, "/api/tokens", laptopIP, &list)
	if len(list) != 2 || list[0].Name != "backup" || list[0].Role != api.RoleAdmin || list[1].Name != "ci.deploy_1" {
		t.Errorf("GET /api/tokens = %+v, want backup and ci.deploy_1", list)
	}
	if list[1].LastUsedAt != nil {
		t.Errorf("a token no one used has LastUsedAt %v", list[1].LastUsedAt)
	}

	// Using it is noted, with the machine.
	if rec := withToken(t, h, http.MethodGet, "/api/me", serverIP, made.Secret, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me with the token = %d", rec.Code)
	}
	get(t, h, "/api/tokens", laptopIP, &list)
	if list[1].LastUsedAt == nil || list[1].LastUsedDeviceID != "server" || list[1].LastUsedFrom == "" {
		t.Errorf("after a use, token = %+v, want it used from the server", list[1])
	}

	events := activity(t, h)
	if len(events) < 2 || events[0].Message != "Made the API token backup, an admin, which works until "+
		list[0].ExpiresAt.UTC().Format("2 January 2006") || events[0].Actor != alice || events[0].Kind != api.EventKindAccess {
		t.Errorf("activity = %+v, want a line for each token", events)
	}

	// Revoking one ends it at once.
	target := "/api/tokens/" + jsonInt(tok.ID)
	if rec := serve(h, newRequestBody(t, http.MethodDelete, target, laptopIP, nil)); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE without the CSRF header = %d, want 403", rec.Code)
	}
	if rec := send(t, h, http.MethodDelete, target, laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE %s = %d %s, want 204", target, rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodDelete, target, laptopIP, ""); rec.Code != http.StatusConflict {
		t.Errorf("DELETE %s twice = %d, want 409", target, rec.Code)
	}
	if rec := send(t, h, http.MethodDelete, "/api/tokens/nope", laptopIP, ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE /api/tokens/nope = %d, want 404", rec.Code)
	}
	rec = withToken(t, h, http.MethodGet, "/api/me", serverIP, made.Secret, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "the API token ci.deploy_1 was revoked on") ||
		!strings.Contains(rec.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Errorf("GET /api/me with a revoked token = %d %s %v, want 401 that says it was revoked", rec.Code, rec.Body, rec.Header())
	}
	if events := activity(t, h); events[0].Message != "Revoked the API token ci.deploy_1" {
		t.Errorf("activity after revoking = %+v", events[0])
	}
	// It stays listed, revoked, and its name isn't used again.
	list = nil
	get(t, h, "/api/tokens", laptopIP, &list)
	if len(list) != 2 || list[1].RevokedAt == nil || list[1].RevokedBy != alice || list[1].ActsAs != "" {
		t.Errorf("GET /api/tokens after revoking = %+v, want ci.deploy_1 revoked by alice", list)
	}
	rec = send(t, h, http.MethodPost, "/api/tokens", laptopIP, `{"name": "ci.deploy_1"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "names aren't used twice") {
		t.Errorf("POST /api/tokens with a revoked token's name = %d %s, want 409", rec.Code, rec.Body)
	}
}

func jsonInt(n int64) string { return strconv.FormatInt(n, 10) }

// activity returns the latest events in the activity log, as alice sees them.
func activity(t *testing.T, h http.Handler) []api.Event {
	t.Helper()
	var list api.EventList
	get(t, h, "/api/events?limit=20", laptopIP, &list)
	return list.Events
}

func TestTokens_use(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	admin := newToken(t, st, "deploy", store.Admin, alice)
	viewer := newToken(t, st, "status", store.Viewer, alice)

	// A token speaks for the caller, even from a tagged server or Bield's own
	// machine, which are anonymous without one.
	for _, from := range []netip.Addr{serverIP, bieldIP, phoneIP} {
		var me api.Me
		rec := withToken(t, h, http.MethodGet, "/api/me", from, admin, "")
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
			t.Fatal(err)
		}
		if !me.SignedIn || me.Token != "deploy" || me.Login != alice || me.Role != api.RoleAdmin || me.TokenRole != api.RoleAdmin || me.Name != "" {
			t.Errorf("GET /api/me with the admin token from %v = %+v", from, me)
		}
	}

	// A change it makes is logged under its maker, with its name.
	rec := withToken(t, h, http.MethodPost, "/api/checks", serverIP, admin,
		`{"name":"web","kind":"tcp","target":"100.64.0.4:443","intervalSec":60}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/checks with the admin token = %d %s", rec.Code, rec.Body)
	}
	if e := activity(t, h)[0]; e.Actor != alice || e.Token != "deploy" || e.Via != api.ViaCLI {
		t.Errorf("the check's activity line = %+v, want alice's, with the token deploy, via the CLI", e)
	}
	// Viewers don't see who.
	var list api.EventList
	get(t, h, "/api/events", phoneIP, &list)
	if e := list.Events[0]; e.Token != "" || e.Actor != "" {
		t.Errorf("a viewer sees %+v, want no actor or token", e)
	}

	rec = withToken(t, h, http.MethodPost, "/api/checks", serverIP, viewer, `{"name": "x"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "status is a viewer token") {
		t.Errorf("POST /api/checks with the viewer token = %d %s, want 403 that says it's a viewer token", rec.Code, rec.Body)
	}
	if rec := withToken(t, h, http.MethodGet, "/api/snapshot", serverIP, viewer, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/snapshot with the viewer token = %d", rec.Code)
	}

	// Some things need a person, whatever the token.
	for _, r := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/tokens", `{"name": "more"}`},
		{http.MethodGet, "/api/tokens", ""},
		{http.MethodPut, "/api/access/users/eve@example.com", `{"role": "admin"}`},
		{http.MethodGet, "/api/devices/laptop/terminal", ""},
	} {
		rec := withToken(t, h, r.method, r.target, serverIP, admin, r.body)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "only a person") {
			t.Errorf("%s %s with the admin token = %d %s, want 403 for people only", r.method, r.target, rec.Code, rec.Body)
		}
	}

	// A bad token fails the request, even for a route anyone can use, and
	// however the header is written: it never falls back to alice.
	unknown := store.TokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAA"
	for _, auth := range []string{"Bearer " + unknown, "Bearer " + admin + "x", "bearer " + unknown, "BEARER  " + unknown + " "} {
		req := newRequest(t, http.MethodGet, "/api/settings", laptopIP)
		req.Header.Set("Authorization", auth)
		if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET /api/settings from alice with %q = %d, want 401", auth, rec.Code)
		}
	}
	req := newRequest(t, http.MethodGet, "/api/me", serverIP)
	req.Header.Set("Authorization", "bearer  "+viewer)
	if rec := serve(h, req); !strings.Contains(rec.Body.String(), `"token":"status"`) {
		t.Errorf("GET /api/me with a lower-case scheme = %d %s, want the token", rec.Code, rec.Body)
	}

	// Nor can a token change which pages Bield trusts.
	rec = withToken(t, h, http.MethodPatch, "/api/settings", serverIP, admin, `{"general": {"publicUrl": "https://evil.example"}}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "only a person") {
		t.Errorf("PATCH the Public URL with the admin token = %d %s, want 403", rec.Code, rec.Body)
	}
	rec = withToken(t, h, http.MethodPatch, "/api/settings", serverIP, admin, `{"general": {"name": "Lab"}}`)
	if rec.Code != http.StatusOK {
		t.Errorf("PATCH the name with the admin token = %d %s, want 200", rec.Code, rec.Body)
	}
}

func TestTokens_expired(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	now := time.Now()
	secret, _, err := st.NewToken(t.Context(), "old", store.Viewer, alice, now.Add(-48*time.Hour), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rec := withToken(t, h, http.MethodGet, "/api/snapshot", serverIP, secret, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "the API token old expired on") {
		t.Errorf("GET /api/snapshot with an expired token = %d %s, want 401 that says it expired", rec.Code, rec.Body)
	}
	var list []api.APIToken
	get(t, h, "/api/tokens", laptopIP, &list)
	if len(list) != 1 || !list[0].Expired || list[0].ActsAs != "" {
		t.Errorf("GET /api/tokens = %+v, want the token expired and acting as no one", list)
	}
}

// A token never does more than its maker could now.
func TestTokens_followMaker(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	secret := newToken(t, st, "bob", store.Admin, bob)

	me := func() api.Me {
		t.Helper()
		var me api.Me
		rec := withToken(t, h, http.MethodGet, "/api/me", serverIP, secret, "")
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
			t.Fatal(err)
		}
		return me
	}
	// Bob is a viewer, so his admin token is too.
	if m := me(); m.Role != api.RoleViewer || m.TokenRole != api.RoleAdmin {
		t.Errorf("bob's admin token = %+v, want it to act as a viewer", m)
	}
	rec := withToken(t, h, http.MethodPut, "/api/alerts/1/ack", serverIP, secret, "")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "bob@example.com, who made it, is a viewer now") {
		t.Errorf("an admin's request with bob's token = %d %s", rec.Code, rec.Body)
	}

	setRole(t, st, bob, store.Admin)
	if m := me(); m.Role != api.RoleAdmin {
		t.Errorf("bob's admin token once bob is an admin = %+v", m)
	}

	// Even when anyone on the tailnet can view Bield, a maker who's removed
	// takes their tokens with them.
	if _, err := st.SetWhoCanView(t.Context(), store.Tailnet); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RemoveUser(t.Context(), bob); err != nil {
		t.Fatal(err)
	}
	if m := me(); m.Role != "" || m.Reason != api.ReasonTokenMakerHasNoRole {
		t.Errorf("bob's token once bob has no role = %+v, want no role", m)
	}
	rec = withToken(t, h, http.MethodGet, "/api/snapshot", serverIP, secret, "")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "has no role in Bield now") {
		t.Errorf("GET /api/snapshot with bob's token once he has no role = %d %s", rec.Code, rec.Body)
	}
	var list []api.APIToken
	get(t, h, "/api/tokens", laptopIP, &list)
	if len(list) != 1 || list[0].ActsAs != "" {
		t.Errorf("GET /api/tokens = %+v, want bob's token acting as no one", list)
	}
}

func setRole(t *testing.T, st *store.Store, login string, role store.Role) {
	t.Helper()
	if _, err := st.SetRole(t.Context(), login, role); err != nil {
		t.Fatal(err)
	}
}

// A stream opened with a token ends when the token is revoked.
func TestStream_tokenRevoked(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		secret, tok, err := st.NewToken(context.Background(), "watch", store.Viewer, alice, now, now.Add(24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		tn := testTailnet()
		h := api.New(withStore(t, tn, st, api.Options{}))
		serveOn(t, tn, h)
		events := readEventsWith(t, tn, serverIP, "/api/stream", true, http.Header{"Authorization": {"Bearer " + secret}})
		synctest.Wait()
		nextSnapshot(t, events)

		if _, err := st.RevokeToken(context.Background(), tok.ID, alice, time.Now()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(api.RoleCheckInterval)
		synctest.Wait()
		if e, ok := <-events; ok {
			t.Errorf("got event %+v after the token was revoked, want the stream to end", e)
		}
	})
}
