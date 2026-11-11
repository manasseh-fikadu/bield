package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// CSRFHeader must be set, to anything, on every request that isn't a GET,
// HEAD or OPTIONS. A web page can't send a custom header to another origin
// without Bield's permission, which it never gives, so a page elsewhere can't
// make a signed-in browser change anything.
const CSRFHeader = "X-Bield"

// viaOf returns how r reached Bield, for the activity log: ViaCLI or ViaMCP if
// it came from one of those, as its User-Agent says, and "" otherwise.
func viaOf(r *http.Request) string {
	name, _, _ := strings.Cut(r.UserAgent(), "/")
	switch name {
	case CLIUserAgent:
		return ViaCLI
	case MCPUserAgent:
		return ViaMCP
	}
	return ""
}

// caller is who sent a request, as Tailscale says, and what Bield lets them do.
type caller struct {
	// who is nil for an anonymous caller.
	who *tailnet.Identity
	// node and device are the ID and MagicDNS name of the node the request
	// came from, if Tailscale said, even if the caller is anonymous.
	node, device string
	// token is the API token the request came with, if it came with one.
	// who is nil then, since the token speaks for the caller, not Tailscale.
	token *store.APIToken
	// level is anyone, viewers or admins. guard always sets it, so a request
	// that skipped guard has noLevel and can't use any route.
	level level
	// reason says why an anonymous caller is anonymous, or why Bield doesn't
	// know a signed-in caller's role: a Reason constant.
	reason string
}

// role returns the caller's role, or "" if they can't view Bield.
func (c caller) role() Role {
	switch c.level {
	case admins:
		return RoleAdmin
	case viewers:
		return RoleViewer
	default:
		return ""
	}
}

// login returns the login that answers for what the caller does: the person
// Tailscale named, or the admin who made the caller's API token. It's "" for
// an anonymous caller.
func (c caller) login() string {
	switch {
	case c.token != nil:
		return c.token.CreatedBy
	case c.who != nil:
		return c.who.LoginName
	}
	return ""
}

// actorKey tells callers apart for the activity log: two callers with the
// same login are one unless one of them came with an API token.
func (c caller) actorKey() string { return c.login() + "\x00" + c.tokenName() }

// tokenName returns the name of the caller's API token, or "".
func (c caller) tokenName() string {
	if c.token == nil {
		return ""
	}
	return c.token.Name
}

type callerKey struct{}

// callerOf returns who sent r. guard put it there.
func callerOf(r *http.Request) caller {
	c, _ := r.Context().Value(callerKey{}).(caller)
	return c
}

// guard checks every request before it reaches a handler, and works out who
// sent it:
//
//   - The Host header must be one of the names of Bield's node, or the host
//     of the Public URL an admin set, so a page that rebinds its own domain
//     to Bield's address can't talk to Bield as the browser's device.
//   - Requests that change things need CSRFHeader.
//   - WebSockets must come from a page on Bield, since browsers let any page
//     open one.
//
// Then it asks Tailscale who is on the other end of the connection, and the
// store what role they have. No header can change the answer, except an API
// token (see identifyToken), which speaks for the caller instead.
func (h *handler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := h.opts.Snapshot()
		if snap.Status == nil {
			w.Header().Set("Retry-After", "1")
			h.deny(w, r, http.StatusServiceUnavailable, "Bield is starting")
			return
		}
		self := snap.Status.Self
		public := h.publicURL(r.Context())
		if !h.opts.Demo && !ownHost(self, r.Host) && !publicHost(public, r.Host) {
			h.deny(w, r, http.StatusMisdirectedRequest, "Bield only answers to its own name on the tailnet")
			return
		}
		switch {
		case isWebSocket(r):
			// Browsers can't add headers to a WebSocket, but they do send Origin.
			if !sameOrigin(r) && !publicOrigin(public, r.Header.Get("Origin")) {
				h.deny(w, r, http.StatusForbidden, "WebSockets must come from Bield's own pages")
				return
			}
		case !safeMethod(r.Method) && r.Header.Get(CSRFHeader) == "":
			h.deny(w, r, http.StatusForbidden, "requests that change things need the "+CSRFHeader+" header")
			return
		}
		c := h.identify(r.Context(), r.RemoteAddr, self.ID)
		if secret := bearer(r); strings.HasPrefix(secret, store.TokenPrefix) {
			// Any request that names a token is the token's, or refused.
			var (
				status int
				msg    string
			)
			if c, status, msg = h.identifyToken(r.Context(), c, secret); status != 0 {
				if status == http.StatusUnauthorized {
					w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				}
				h.deny(w, r, status, msg)
				return
			}
		}
		ctx := store.WithVia(context.WithValue(r.Context(), callerKey{}, c), viaOf(r))
		if c.token != nil {
			ctx = store.WithToken(ctx, c.token.Name)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// identify asks Tailscale who is at remoteAddr. Anyone it can't name is
// anonymous, and so are tagged devices, which belong to no one, and
// connections from Bield's own node. In host mode that node is the machine
// Bield runs on, and any process there would otherwise act as its owner.
// Anonymous callers can only use routes for anyone.
func (h *handler) identify(ctx context.Context, remoteAddr, selfID string) caller {
	who, err := h.opts.WhoIs(ctx, remoteAddr)
	switch {
	case errors.Is(err, tailnet.ErrUnknownPeer):
		return caller{level: anyone, reason: ReasonUnknownDevice}
	case err != nil:
		h.log.Warn("ask Tailscale who sent a request", "remote_addr", remoteAddr, "error", err)
		return caller{level: anyone, reason: ReasonWhoIsFailed}
	case who.NodeID == selfID:
		return caller{node: who.NodeID, device: who.NodeName, level: anyone, reason: ReasonBieldMachine}
	case len(who.Tags) > 0:
		return caller{node: who.NodeID, device: who.NodeName, level: anyone, reason: ReasonTaggedDevice}
	case who.LoginName == "":
		return caller{node: who.NodeID, device: who.NodeName, level: anyone, reason: ReasonNoUser}
	}
	l, err := h.levelOf(ctx, who.LoginName)
	if err != nil {
		// If Bield can't tell what someone's role is, they have none.
		return caller{who: who, node: who.NodeID, device: who.NodeName, level: anyone, reason: ReasonRoleLookupFailed}
	}
	return caller{who: who, node: who.NodeID, device: who.NodeName, level: l}
}

// identifyToken makes peer, the caller Tailscale named, the caller that
// secret, an API token, speaks for: from any machine, even a tagged one or
// Bield's own, since whoever holds the token is who an admin gave it to. Its
// level is the token's role, or its maker's role now if that's lower, so an
// admin who loses their role takes their tokens' power with them. It returns
// the status and message to deny the request with if the token doesn't work,
// or 0, since a request that names a token mustn't quietly go on without it.
func (h *handler) identifyToken(ctx context.Context, peer caller, secret string) (caller, int, string) {
	const next = "ask one of Bield's admins for a new one"
	now := time.Now()
	tok, err := h.opts.Store.TokenBySecret(ctx, secret, now)
	switch {
	case errors.Is(err, store.ErrUnknownToken):
		return caller{}, http.StatusUnauthorized,
			"Bield doesn't know this API token; check that it was copied whole, or " + next
	case errors.Is(err, store.ErrTokenRevoked):
		return caller{}, http.StatusUnauthorized,
			"the API token " + tok.Name + " was revoked on " + tok.RevokedAt.UTC().Format("2006-01-02 15:04 MST") + "; " + next
	case errors.Is(err, store.ErrTokenExpired):
		return caller{}, http.StatusUnauthorized,
			"the API token " + tok.Name + " expired on " + tok.ExpiresAt.UTC().Format("2006-01-02 15:04 MST") + "; " + next
	case err != nil:
		if ctx.Err() == nil {
			h.log.Error("look up an API token", "error", err)
		}
		return caller{}, http.StatusServiceUnavailable, "Bield couldn't check this API token"
	}
	c := caller{node: peer.node, device: peer.device, token: &tok}
	c.level, c.reason = h.tokenLevel(ctx, tok)
	if !tok.LastUsedAt.After(now.Add(-time.Minute)) {
		if err := h.opts.Store.TouchToken(ctx, tok.ID, c.node, now); err != nil && ctx.Err() == nil {
			h.log.Warn("note an API token's use", "token", tok.Name, "error", err)
		}
	}
	return c, 0, ""
}

// tokenLevel returns the level of a caller with tok, and the reason for it if
// it isn't the token's own: the lower of the token's role and its maker's.
// The maker's role is the one they were given, not the viewer's role that
// anyone on the tailnet may have, so a person who is removed takes their
// tokens with them whoever can view Bield.
func (h *handler) tokenLevel(ctx context.Context, tok store.APIToken) (level, string) {
	own := viewers
	if tok.Role == store.Admin {
		own = admins
	}
	role, err := h.opts.Store.OwnRole(ctx, tok.CreatedBy)
	var maker level
	switch {
	case err != nil:
		if ctx.Err() == nil {
			h.log.Error("look up an API token's maker's role", "login", tok.CreatedBy, "error", err)
		}
		return anyone, ReasonRoleLookupFailed
	case role == store.Admin:
		maker = admins
	case role == store.Viewer:
		maker = viewers
	default:
		return anyone, ReasonTokenMakerHasNoRole
	}
	return min(own, maker), ""
}

// levelNow returns the level c has now, for a response that outlives the
// request guard checked, such as a stream or a terminal. A token that was
// revoked or expired since has none. It returns an error if the store can't
// say.
func (h *handler) levelNow(ctx context.Context, c caller) (level, error) {
	if c.token == nil {
		if c.who == nil {
			return c.level, nil
		}
		return h.levelOf(ctx, c.who.LoginName)
	}
	tok, err := h.opts.Store.Token(ctx, c.token.ID, time.Now())
	switch {
	case errors.Is(err, store.ErrUnknownToken), errors.Is(err, store.ErrTokenRevoked), errors.Is(err, store.ErrTokenExpired):
		return anyone, nil
	case err != nil:
		return anyone, err
	}
	l, reason := h.tokenLevel(ctx, tok)
	if reason == ReasonRoleLookupFailed {
		return anyone, errors.New("look up an API token's maker's role")
	}
	return l, nil
}

// levelOf returns the level of a signed-in caller, from the role the store
// gives login. It returns an error if the store can't say.
func (h *handler) levelOf(ctx context.Context, login string) (level, error) {
	role, err := h.opts.Store.Role(ctx, login)
	if err != nil {
		if ctx.Err() == nil {
			h.log.Error("look up a caller's role", "login", login, "error", err)
		}
		return anyone, err
	}
	switch role {
	case store.Admin:
		return admins, nil
	case store.Viewer:
		return viewers, nil
	default:
		return anyone, nil
	}
}

// deny answers with an error: JSON for the API, and text for anything else.
func (h *handler) deny(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.writeJSON(w, r, status, ErrorBody{Error: msg})
		return
	}
	http.Error(w, msg, status)
}

// ownHost reports whether hostport names Bield's node: its MagicDNS name,
// the short name that MagicDNS also resolves, or one of its tailnet IPs. A
// page can't make the browser send any of these unless it's on Bield.
func ownHost(self tailnet.Device, hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.TrimSuffix(host, ".")
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		for _, own := range self.IPs {
			if ip.Unmap() == own {
				return true
			}
		}
		return false
	}
	if self.DNSName == "" {
		return false
	}
	short, _, _ := strings.Cut(self.DNSName, ".")
	return strings.EqualFold(host, self.DNSName) || strings.EqualFold(host, short)
}

// publicURL returns the Public URL that admins set in the settings, or nil if
// they haven't. It's the address that a proxy, or a name of their own for
// Bield's address, serves Bield at, so it names Bield too.
func (h *handler) publicURL(ctx context.Context) *url.URL {
	s := h.opts.Settings.Get(ctx).General.PublicURL
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil
	}
	return u
}

// canonicalHost writes hostport so that two spellings of one address are
// equal: in lower case, without a trailing dot or brackets, and without the
// port that scheme uses anyway.
func canonicalHost(scheme, hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	host = strings.ToLower(strings.Trim(strings.TrimSuffix(host, "."), "[]"))
	if port == "" || port == map[string]string{"http": "80", "https": "443"}[scheme] {
		return host
	}
	return net.JoinHostPort(host, port)
}

// publicHost reports whether hostport is the host, and the port, of the Public
// URL. It takes no other name, so a page that rebinds its own domain to
// Bield's address still gets a 421.
func publicHost(public *url.URL, hostport string) bool {
	return public != nil && hostport != "" &&
		canonicalHost(public.Scheme, hostport) == canonicalHost(public.Scheme, public.Host)
}

// publicOrigin reports whether origin is the Public URL's origin: the page a
// proxy serves, which browsers say it is even if the proxy hands Bield its
// own name as the Host.
func publicOrigin(public *url.URL, origin string) bool {
	if public == nil {
		return false
	}
	o, err := url.Parse(origin)
	return err == nil && o.User == nil && o.Path == "" && o.Scheme == public.Scheme &&
		canonicalHost(o.Scheme, o.Host) == canonicalHost(public.Scheme, public.Host)
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// isWebSocket reports whether r opens a WebSocket: an HTTP/1.1 upgrade, or
// an HTTP/2 extended CONNECT (RFC 8441), which net/http shows as a CONNECT
// with a :protocol header. Go turns extended CONNECT off unless GODEBUG
// turns it on, so browsers use HTTP/1.1 for now.
func isWebSocket(r *http.Request) bool {
	if r.Method == http.MethodConnect {
		return strings.EqualFold(r.Header.Get(":protocol"), "websocket")
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// sameOrigin reports whether r's Origin is the page's own origin: the scheme
// it arrived over and its Host, which guard has already checked.
func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host) && origin.Path == ""
}
