// Package cli is the part of the bield command line that talks to a running
// Bield, for people and for AI agents: bield devices, bield alerts ack and so
// on. It is a Go client of the API (internal/client), so it has the roles the
// web app has. Bield signs the CLI in by asking Tailscale who is on the other
// end, so there is nothing to log in to.
//
// Every command is a row in a table (see commands), and the help, the shell
// completion and the docs are made from it, so they can't drift from what the
// commands do.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/client"
)

// The exit codes of a command.
const (
	ExitOK       = 0
	ExitFailed   = 1
	ExitUsage    = 2
	ExitNotFound = 3
	ExitDenied   = 4
)

// Env is what the command line takes from outside the process, so tests can
// give it something else.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Getenv reads an environment variable. Nil reads none.
	Getenv func(string) string
	// ConfigDir is where bield connect saves Bield's address. Empty means
	// nowhere, and the address comes from --url or BIELD_URL.
	ConfigDir string
	// Version is the version of bield, which Bield sees in the User-Agent.
	Version string
	// HTTP sends the requests. Nil is a client with sensible timeouts.
	HTTP *http.Client
	// Terminal says stdout is a terminal, so output may be in color.
	Terminal bool
	// Now returns the time that ages in the output are counted from. Nil is
	// time.Now.
	Now func() time.Time
}

func (e Env) getenv(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

// DefaultEnv returns the Env of the running process.
func DefaultEnv(version string) Env {
	dir, _ := os.UserConfigDir()
	if dir != "" {
		dir += "/bield"
	}
	fi, err := os.Stdout.Stat()
	return Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, ConfigDir: dir, Version: version,
		Terminal: err == nil && fi.Mode()&os.ModeCharDevice != 0,
	}
}

// A usageError is a mistake in how a command was run. The command prints how
// to run it.
type usageError struct {
	msg string
	cmd *Command
}

func (e *usageError) Error() string { return e.msg }

func usagef(cmd *Command, format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...), cmd: cmd}
}

// Has reports whether name is a command of this package, as opposed to one of
// bield's other commands, such as serve.
func Has(name string) bool {
	return slices.ContainsFunc(commands, func(c *Command) bool { return c.words()[0] == name })
}

// Handles reports whether args run a command of this package. Unlike Has, it
// looks past the flags every command takes, so "--json devices" is one.
func Handles(args []string) bool {
	_, rest, err := extractGlobals(args)
	return err == nil && len(rest) > 0 && Has(rest[0])
}

// Names returns the first word of each command, once, in the order of the help.
func Names() []string {
	var out []string
	for _, c := range commands {
		if w := c.words()[0]; !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// Run runs a command with the arguments after "bield", such as {"alerts",
// "ack", "5"}, and returns the process's exit code.
func Run(ctx context.Context, env Env, args []string) int {
	g, rest, err := extractGlobals(args)
	if err != nil {
		return report(env, g, err)
	}
	if g.noColor || env.getenv("NO_COLOR") != "" || env.getenv("TERM") == "dumb" {
		env.Terminal = false
	}
	cmd, rest := find(rest)
	if cmd == nil {
		if len(rest) == 0 || rest[0] == "help" || g.help {
			printHelp(env.Stdout, rest)
			return ExitOK
		}
		return report(env, g, usagef(nil, "unknown command %q; run bield help to see them", strings.Join(rest, " ")))
	}
	if g.help {
		printCommandHelp(env.Stdout, cmd)
		return ExitOK
	}
	vals, pos, err := parseFlags(cmd.Flags, rest)
	if err != nil {
		return report(env, g, usagef(cmd, "%v", err))
	}
	if cmd.Destructive && !g.yes {
		return report(env, g, usagef(cmd, "%s %s; add --yes if you mean it (bield never asks)", cmd.Name, cmd.destructiveNote()))
	}
	// A command that follows has no timeout. It runs until Ctrl-C.
	following := cmd.Stream && vals["follow"] == "true"
	if !following && g.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	call := &Call{Ctx: ctx, Env: env, cmd: cmd, global: g, flags: vals, Args: pos}
	if err := call.checkArgs(); err != nil {
		return report(env, g, err)
	}
	err = cmd.Run(call)
	if err != nil && call.client != nil {
		err = call.client.Explain(ctx, err)
	}
	return report(env, g, err)
}

// report prints an error, in JSON on stderr if the command was asked for
// JSON, and returns the exit code for it.
func report(env Env, g globals, err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	var (
		code = ExitFailed
		msg  = err.Error()
		hint string
		stat int
		ue   *usageError
		ce   *client.Error
	)
	switch {
	case errors.As(err, &ue):
		code = ExitUsage
	case errors.As(err, &ce):
		msg, hint, stat = ce.Message, ce.Hint, ce.Status
		switch {
		case ce.NotFound():
			code = ExitNotFound
		case ce.Denied():
			code = ExitDenied
		}
	case errors.Is(err, context.DeadlineExceeded):
		msg = "timed out; Bield didn't answer in time (change this with --timeout)"
	}
	if g.json {
		_ = json.NewEncoder(env.Stderr).Encode(struct { //nolint:errchkjson // A write to stderr that fails has nowhere to be reported.
			Error  string `json:"error"`
			Hint   string `json:"hint,omitempty"`
			Status int    `json:"status,omitempty"`
			Code   int    `json:"code"`
		}{msg, hint, stat, code})
		return code
	}
	fmt.Fprintf(env.Stderr, "bield: %s\n", msg)
	if hint != "" {
		fmt.Fprintf(env.Stderr, "  %s\n", capitalize(hint))
	}
	if ue != nil && ue.cmd != nil {
		fmt.Fprintf(env.Stderr, "\nUsage: bield %s\n", ue.cmd.usageLine())
	}
	return code
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// globals are the flags every command takes, wherever they stand.
type globals struct {
	url     string
	json    bool
	yes     bool
	help    bool
	noColor bool
	timeout time.Duration
}

// extractGlobals takes the global flags out of args and returns the rest, in
// order. Nothing after "--" is a flag.
func extractGlobals(args []string) (g globals, rest []string, err error) {
	g.timeout = 30 * time.Second
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		take := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", usagef(nil, "%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--url":
			if g.url, err = take(); err != nil {
				return g, nil, err
			}
		case "--timeout":
			s, err := take()
			if err != nil {
				return g, nil, err
			}
			if g.timeout, err = time.ParseDuration(s); err != nil || g.timeout < 0 {
				return g, nil, usagef(nil, "--timeout takes a time such as 30s or 2m, not %q", s)
			}
		case "--json":
			g.json = !hasVal || val != "false"
		case "--yes", "-y":
			g.yes = !hasVal || val != "false"
		case "--no-color":
			g.noColor = true
		case "--help", "-h":
			g.help = true
		default:
			rest = append(rest, a)
		}
	}
	return g, rest, nil
}
