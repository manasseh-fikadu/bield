package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// The environment variables that make bield join the tailnet on its own, for
// a machine without Tailscale, such as a container or a CI runner.
const (
	// authKeyEnv holds a Tailscale (or Headscale) auth key. When it's set,
	// bield starts a node of its own, in this process, and sends its requests
	// through it, instead of through the machine's Tailscale.
	authKeyEnv = "BIELD_TS_AUTHKEY"
	// stateDirEnv keeps that node's state, so the next run is the same node
	// and needn't log in again. Without it, each run is a new node that's
	// ephemeral and logs out when the command ends.
	stateDirEnv = "BIELD_TS_STATE_DIR"
	// hostnameEnv names the node. It's "bield-cli" if it's empty.
	hostnameEnv = "BIELD_TS_HOSTNAME"
	// controlURLEnv is the control server, for Headscale. The server reads the
	// same variable.
	controlURLEnv = "BIELD_CONTROL_URL"
)

// nodeUpTimeout is how long bield waits for its own node to join the tailnet.
const nodeUpTimeout = time.Minute

// A tsNode is the Tailscale node bield starts for itself when it has an auth
// key. It starts when a command first needs to reach Bield, so help and
// usage errors never join the tailnet, and Run closes it.
type tsNode struct {
	env Env

	mu   sync.Mutex
	srv  *tsnet.Server
	hc   *http.Client
	dir  string
	temp bool

	// logs keeps tsnet's last few lines, which say why a login failed, such
	// as an expired key. tsnet writes them from its own goroutines, while
	// client holds mu, so they have a lock of their own.
	logMu sync.Mutex
	logs  []string
}

// newTSNode returns the node for env, or nil if env has no auth key, or
// sends its requests some other way already.
func newTSNode(env Env) *tsNode {
	if env.HTTP != nil || env.getenv(authKeyEnv) == "" {
		return nil
	}
	return &tsNode{env: env}
}

// client starts the node if it hasn't started, and returns an HTTP client
// that sends requests through it.
func (n *tsNode) client(ctx context.Context) (*http.Client, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.hc != nil {
		return n.hc, nil
	}
	dir, temp := n.env.getenv(stateDirEnv), false
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "bield-tsnet-*"); err != nil {
			return nil, fmt.Errorf("make a directory for this run's Tailscale node: %w", err)
		}
		temp = true
	}
	host := n.env.getenv(hostnameEnv)
	if host == "" {
		host = "bield-cli"
	}
	srv := &tsnet.Server{
		Dir:        dir,
		Hostname:   host,
		AuthKey:    n.env.getenv(authKeyEnv),
		ControlURL: n.env.getenv(controlURLEnv),
		// A node per run would fill the machine list; an ephemeral one goes
		// away by itself, and Close logs it out at once.
		Ephemeral: temp,
		Logf:      func(string, ...any) {},
		UserLogf:  n.keepLog,
	}
	upCtx, cancel := context.WithTimeout(ctx, nodeUpTimeout)
	defer cancel()
	err := n.up(upCtx, srv)
	if err != nil {
		_ = srv.Close()
		if temp {
			_ = os.RemoveAll(dir)
		}
		return nil, n.upError(err)
	}
	n.srv, n.dir, n.temp, n.hc = srv, dir, temp, srv.HTTPClient()
	return n.hc, nil
}

// up starts srv and waits until it knows the other machines on the tailnet.
// A node that starts from saved state is up at once, before its map of the
// tailnet arrives, and a request sent then would look Bield's name up in the
// machine's DNS instead. A node that never sees another machine isn't let
// reach any by the tailnet's access rules, Bield included.
func (n *tsNode) up(ctx context.Context, srv *tsnet.Server) error {
	if _, err := srv.Up(ctx); err != nil {
		return err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return err
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		st, err := lc.Status(ctx)
		if err == nil && len(st.Peer) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("it joined, but sees no other machine; check that the tailnet's access rules let it reach Bield: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func (n *tsNode) keepLog(format string, args ...any) {
	line := strings.TrimSpace(fmt.Sprintf(format, args...))
	n.logMu.Lock()
	defer n.logMu.Unlock()
	n.logs = append(n.logs, line)
	if len(n.logs) > 5 {
		n.logs = n.logs[1:]
	}
}

// upError says why the node didn't join the tailnet.
func (n *tsNode) upError(err error) error {
	n.logMu.Lock()
	last := strings.Join(n.logs, "; ")
	n.logMu.Unlock()
	msg := "bield couldn't join the tailnet with " + authKeyEnv
	if errors.Is(err, context.DeadlineExceeded) {
		// The error says nothing, so what Tailscale last said may.
		msg += fmt.Sprintf(" within %v", nodeUpTimeout)
		if last != "" {
			msg += " (Tailscale said: " + last + ")"
		}
	}
	return fmt.Errorf("%s: %w; check that the key is valid and not used up, and set %s for Headscale",
		msg, err, controlURLEnv)
}

// close stops the node. A node for this run alone logs out first, so the
// tailnet forgets it at once, and its state is removed.
func (n *tsNode) close() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.srv == nil {
		return
	}
	if n.temp {
		if lc, err := n.srv.LocalClient(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = lc.Logout(ctx)
			cancel()
		}
	}
	_ = n.srv.Close()
	if n.temp {
		_ = os.RemoveAll(n.dir)
	}
	n.srv, n.hc = nil, nil
}
