package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// tokenName is what an API token's name may be: short, and safe to show in a
// shell, a table or the activity log as it is.
var tokenName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// listTokens answers GET /api/tokens.
func (h *handler) listTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.opts.Store.Tokens(r.Context())
	if err != nil {
		h.fail(w, r, "list API tokens", err)
		return
	}
	out := make([]APIToken, len(tokens))
	for i, t := range tokens {
		out[i] = h.apiToken(r, t)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// newToken answers POST /api/tokens, which makes an API token.
func (h *handler) newToken(w http.ResponseWriter, r *http.Request) {
	var body NewAPIToken
	if !h.readJSON(w, r, &body) {
		return
	}
	if !tokenName.MatchString(body.Name) {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
			Error: "a token's name is 1 to 40 letters, digits, dots, dashes and underscores, starting with a letter or digit",
		})
		return
	}
	var role store.Role
	switch body.Role {
	case RoleViewer, "":
		role = store.Viewer
	case RoleAdmin:
		role = store.Admin
	default:
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
			Error: `role is "` + string(RoleAdmin) + `" or "` + string(RoleViewer) + `"`,
		})
		return
	}
	days := body.Days
	if days == 0 {
		days = DefaultTokenDays
	}
	if days < 1 || days > MaxTokenDays {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
			Error: fmt.Sprintf("a token works for 1 to %d days", MaxTokenDays),
		})
		return
	}
	now := time.Now()
	secret, t, err := h.opts.Store.NewToken(r.Context(), body.Name, role, callerOf(r).login(), now, now.AddDate(0, 0, days))
	switch {
	case errors.Is(err, store.ErrTokenName):
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{
			Error: "an API token was already called " + body.Name + ", and names aren't used twice, even after a token is " +
				"revoked, so the activity log always means one token; pick another name",
		})
		return
	case err != nil:
		h.fail(w, r, "make an API token", err)
		return
	}
	h.log.Info("made an API token", "token", t.Name, "role", string(t.Role), "by", t.CreatedBy, "expires", t.ExpiresAt)
	h.logAction(r, EventKindAccess, "", fmt.Sprintf("Made the API token %s, %s %s, which works until %s",
		t.Name, article(t.Role), t.Role, t.ExpiresAt.UTC().Format("2 January 2006")))
	h.writeJSON(w, r, http.StatusCreated, NewAPITokenResult{Token: h.apiToken(r, t), Secret: secret})
}

// revokeToken answers DELETE /api/tokens/{id}, which revokes an API token at
// once: the next request with it is refused, and a stream it opened ends
// within a role check. The token stays in the list, revoked, and keeps its
// name.
func (h *handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no such API token"})
		return
	}
	t, err := h.opts.Store.RevokeToken(r.Context(), id, callerOf(r).login(), time.Now())
	switch {
	case errors.Is(err, store.ErrUnknownToken):
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no such API token"})
		return
	case errors.Is(err, store.ErrTokenRevoked):
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: "the API token " + t.Name + " was revoked already"})
		return
	case err != nil:
		h.fail(w, r, "revoke an API token", err)
		return
	}
	h.log.Info("revoked an API token", "token", t.Name, "by", callerOf(r).login())
	h.logAction(r, EventKindAccess, "", "Revoked the API token "+t.Name)
	w.WriteHeader(http.StatusNoContent)
}

// apiToken returns t as the API shows it.
func (h *handler) apiToken(r *http.Request, t store.APIToken) APIToken {
	out := APIToken{
		ID: t.ID, Name: t.Name, Hint: t.Hint, Role: RoleViewer, CreatedBy: t.CreatedBy,
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, Expired: t.Expired(time.Now()),
	}
	if t.Role == store.Admin {
		out.Role = RoleAdmin
	}
	if t.Revoked() {
		at := t.RevokedAt
		out.RevokedAt, out.RevokedBy = &at, t.RevokedBy
	}
	if !out.Expired && !t.Revoked() {
		if l, _ := h.tokenLevel(r.Context(), t); l >= viewers {
			out.ActsAs = (caller{level: l}).role()
		}
	}
	if !t.LastUsedAt.IsZero() {
		at := t.LastUsedAt
		out.LastUsedAt = &at
	}
	if t.LastUsedNode != "" {
		out.LastUsedDeviceID, out.LastUsedFrom = t.LastUsedNode, h.deviceName(r, t.LastUsedNode)
	}
	return out
}

// article returns "an" for the admin role and "a" for the viewer role.
func article(role store.Role) string {
	if role == store.Admin {
		return "an"
	}
	return "a"
}
