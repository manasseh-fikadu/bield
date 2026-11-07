// Package client is a Go client for Bield's API. The bield command line uses
// it, and so will the MCP server, so that neither can drift from the types the
// server answers with (see internal/api).
//
// Bield signs a caller in by asking Tailscale who is on the other end of the
// connection, so a client holds no credentials: it must run on a machine on
// the tailnet, and Bield sees the person who is logged in there.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// Options configures New.
type Options struct {
	// URL is where Bield is, such as https://bield.example.ts.net. Required.
	// It must name Bield's node, since Bield only answers to its own names.
	URL string
	// Name and Version make the User-Agent that Bield marks the changes in
	// its activity log with. Name is api.CLIUserAgent if it's empty.
	Name, Version string
	// HTTP sends the requests. Nil is a client that gives a server 30
	// seconds to start answering, and any time at all to finish.
	HTTP *http.Client
}

// A Client talks to one Bield.
type Client struct {
	base      *url.URL
	hc        *http.Client
	userAgent string
}

// New returns a Client for opts.URL. See ParseURL for the forms it takes.
func New(opts Options) (*Client, error) {
	base, err := ParseURL(opts.URL)
	if err != nil {
		return nil, err
	}
	hc := opts.HTTP
	if hc == nil {
		tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
		if def, ok := http.DefaultTransport.(*http.Transport); ok {
			tr = def.Clone()
		}
		tr.ResponseHeaderTimeout = 30 * time.Second
		hc = &http.Client{Transport: tr}
	}
	name := opts.Name
	if name == "" {
		name = api.CLIUserAgent
	}
	ua := name
	if opts.Version != "" {
		ua += "/" + opts.Version
	}
	return &Client{base: base, hc: hc, userAgent: ua}, nil
}

// ParseURL turns what someone typed for Bield's address into its base URL. A
// bare name such as bield.example.ts.net is https, and a path is dropped.
func ParseURL(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("no Bield address: pass --url, set BIELD_URL, or run bield connect <url>")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%q isn't a Bield address: %w", s, err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("%q isn't a Bield address: use something like https://bield.example.ts.net", s)
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}

// URL returns the address of the Bield the client talks to.
func (c *Client) URL() string { return c.base.String() }

// An Error is a response from Bield that says no.
type Error struct {
	// Status is the HTTP status.
	Status int
	// Message is what Bield said.
	Message string
	// Hint says what to do about it, if the client could work that out. See
	// Client.Explain.
	Hint string
}

func (e *Error) Error() string {
	if e.Hint == "" {
		return e.Message
	}
	return e.Message + "; " + e.Hint
}

// NotFound reports whether Bield said there's no such thing.
func (e *Error) NotFound() bool { return e.Status == http.StatusNotFound }

// Denied reports whether Bield refused the caller, because of who they are
// and not because of what they asked.
func (e *Error) Denied() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// An UnreachableError means the request never got to Bield.
type UnreachableError struct {
	URL string
	Err error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("can't reach Bield at %s (%v); is this machine on the tailnet, with Tailscale connected?", e.URL, e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// request builds a request for path on Bield.
func (c *Client) request(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Request, error) {
	u := *c.base
	u.Path = path
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set(api.CSRFHeader, "1")
	}
	return req, nil
}

// send sends req, and turns an answer that isn't a success into an Error.
// The caller closes the body of a response it gets.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			_, isOp := errors.AsType[*net.OpError](err)
			_, isDNS := errors.AsType[*net.DNSError](err)
			if isOp || isDNS {
				return nil, &UnreachableError{URL: c.URL(), Err: urlErr.Err}
			}
		}
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	msg := http.StatusText(resp.StatusCode)
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var eb api.ErrorBody
	if json.Unmarshal(raw, &eb) == nil && eb.Error != "" {
		msg = eb.Error
	} else if s := strings.TrimSpace(string(raw)); s != "" && len(s) < 300 {
		msg = s
	}
	return nil, &Error{Status: resp.StatusCode, Message: msg}
}

// do sends a request with a JSON body, if in isn't nil, and decodes the JSON
// answer into out, if it isn't nil.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	contentType := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, contentType = bytes.NewReader(b), "application/json"
	}
	req, err := c.request(ctx, method, path, q, body, contentType)
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("read Bield's answer to %s %s: %w", method, path, err)
	}
	return nil
}

// Explain adds to an error from Bield what the client can work out about why
// Bield refused the caller. For a refusal it asks Bield who it thinks the
// caller is, so a tagged server hears that it is tagged and a viewer hears
// what they are, instead of a bare 403. Other errors come back as they are.
func (c *Client) Explain(ctx context.Context, err error) error {
	var e *Error
	if !errors.As(err, &e) || e.Hint != "" {
		return err
	}
	switch e.Status {
	case http.StatusMisdirectedRequest:
		e.Hint = "the address names this Bield by something other than its own name on the tailnet; use its MagicDNS name, such as https://bield.example.ts.net"
		return e
	case http.StatusForbidden, http.StatusServiceUnavailable:
	default:
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	me, meErr := c.Me(ctx)
	if meErr != nil {
		return err
	}
	if hint := Hint(me); hint != "" {
		e.Hint = hint
	}
	return e
}

// Hint says in a sentence why Bield treats the caller as it does and what to
// do about it, or "" if there is nothing to add: the caller is an admin.
func Hint(me api.Me) string {
	switch {
	case me.Reason == api.ReasonTaggedDevice:
		return "this machine is tagged in Tailscale, so it belongs to no one and Bield doesn't know who you are; " +
			"run bield from a machine you are logged in to"
	case me.Reason == api.ReasonBieldMachine:
		return "this is Bield's own machine, which Bield treats as anonymous because any process on it could send the request; " +
			"run bield from another machine on the tailnet"
	case me.Reason == api.ReasonUnknownDevice:
		return "Tailscale doesn't know the machine this came from; check that Tailscale is running and logged in here"
	case me.Reason == api.ReasonNoUser:
		return "Tailscale names this machine but no user; run bield from a machine a person is logged in to"
	case me.Reason == api.ReasonWhoIsFailed:
		return "Bield couldn't ask Tailscale who you are; try again in a moment"
	case me.Reason == api.ReasonRoleLookupFailed:
		return "Bield couldn't look up your role just now; try again in a moment"
	case me.SignedIn && me.Role == "":
		return fmt.Sprintf("you are signed in as %s, but have no role in Bield; ask one of its admins to make you a viewer", me.Login)
	case me.SignedIn && me.Role == api.RoleViewer:
		return fmt.Sprintf("you are signed in as %s, a viewer; this needs an admin", me.Login)
	}
	return ""
}

// Health returns whether Bield is up, and its version. Anyone can ask.
func (c *Client) Health(ctx context.Context) (h api.Health, err error) {
	return h, c.do(ctx, http.MethodGet, "/api/health", nil, nil, &h)
}

// Me returns who Bield thinks is asking. Anyone can ask.
func (c *Client) Me(ctx context.Context) (me api.Me, err error) {
	return me, c.do(ctx, http.MethodGet, "/api/me", nil, nil, &me)
}

// Snapshot returns what Bield knows now: its devices, checks, alerts and so
// on. It is the same as the first event of the stream.
func (c *Client) Snapshot(ctx context.Context) (s api.Snapshot, err error) {
	return s, c.do(ctx, http.MethodGet, "/api/snapshot", nil, nil, &s)
}

// Event is a server-sent event.
type Event struct {
	Name string
	Data []byte
}

// stream sends a GET request for path and calls fn with each server-sent
// event, until fn returns an error, the stream ends or ctx is done. It
// returns nil when Bield ends the stream.
func (c *Client) stream(ctx context.Context, path string, q url.Values, fn func(Event) error) error {
	req, err := c.request(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	// Events are flushed one at a time. Without compression they reach the
	// reader as they are sent.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var ev Event
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.Name != "" || len(ev.Data) > 0 {
				if err := fn(ev); err != nil {
					return err
				}
			}
			ev = Event{}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			ev.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if len(ev.Data) > 0 {
				ev.Data = append(ev.Data, '\n')
			}
			ev.Data = append(ev.Data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

// Watch calls fn with a new snapshot each time Bield publishes one, the first
// as soon as it opens, until fn returns an error or ctx is done. Bield ends a
// stream when the caller's role changes, and a restart ends it too, so Watch
// opens it again after a moment. It gives up on an error from Bield, such as a
// refusal, and on any error from the first try.
func (c *Client) Watch(ctx context.Context, fn func(api.Snapshot) error) error {
	errStop := errors.New("stop")
	for first := true; ; first = false {
		var fnErr error
		err := c.stream(ctx, "/api/stream", nil, func(ev Event) error {
			if ev.Name != "snapshot" {
				return nil
			}
			var s api.Snapshot
			if err := json.Unmarshal(ev.Data, &s); err != nil {
				return err
			}
			if fnErr = fn(s); fnErr != nil {
				return errStop
			}
			return nil
		})
		var apiErr *Error
		switch {
		case fnErr != nil:
			return fnErr
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil && (first || errors.As(err, &apiErr)):
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
