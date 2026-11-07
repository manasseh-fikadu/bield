package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// CSRFHeader must be set, to anything, on every request that isn't a GET,
// HEAD or OPTIONS. A web page can't send a custom header to another origin
// without Bield's permission, which it never gives, so a page elsewhere can't
// make a signed-in browser change anything.
const CSRFHeader = "X-Bield"

// viaOf returns how r reached Bield, for the activity log: ViaCLI if it came
// from the command line, as its User-Agent says, and "" otherwise.
func viaOf(r *http.Request) string {
	if name, _, _ := strings.Cut(r.UserAgent(), "/"); name == CLIUserAgent {
		return ViaCLI
	}
	return ""
}

// caller is who sent a request, as Tailscale says, and what Bield lets them do.
type caller struct {
	// who is nil for an anonymous caller.
	who *tailnet.Identity
	// node is the ID of the node the request came from, if Tailscale said,
	// even if the caller is anonymous.
	node string
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
// store what role they have. No header can change the answer.
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
		ctx := store.WithVia(context.WithValue(r.Context(), callerKey{}, c), viaOf(r))
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
		return caller{node: who.NodeID, level: anyone, reason: ReasonBieldMachine}
	case len(who.Tags) > 0:
		return caller{node: who.NodeID, level: anyone, reason: ReasonTaggedDevice}
	case who.LoginName == "":
		return caller{node: who.NodeID, level: anyone, reason: ReasonNoUser}
	}
	l, err := h.levelOf(ctx, who.LoginName)
	if err != nil {
		// If Bield can't tell what someone's role is, they have none.
		return caller{who: who, node: who.NodeID, level: anyone, reason: ReasonRoleLookupFailed}
	}
	return caller{who: who, node: who.NodeID, level: l}
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
