package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A Command is something bield does with a running Bield.
type Command struct {
	// Name is the words that run it after "bield", such as "alerts ack".
	Name string
	// Summary is one line that says what it does, without a full stop.
	Summary string
	// Args is how its arguments read in the usage, such as "<device> [path]".
	Args string
	// Flags are the flags it takes besides the ones every command takes.
	Flags []Flag
	// Admin says it needs an admin. The commands that change anything do.
	Admin bool
	// ReadOnly says a command that needs an admin changes nothing, as with
	// reading a container's logs. MCP clients are told so.
	ReadOnly bool
	// Destructive says it deletes, stops or overwrites something, so it
	// does nothing without --yes.
	Destructive bool
	// Stream says it has a --follow flag, which keeps it open to print as
	// things happen. A run that follows has no timeout.
	Stream bool
	// Serves says it runs until it is stopped, so it has no timeout.
	Serves bool
	// Wait is the longest it runs, if that's longer than --timeout, for a
	// command that waits for something slow, such as an install.
	Wait time.Duration
	// NoTool says why this command isn't a tool of the MCP server. Every other
	// command is one.
	NoTool string
	// Stdin says what the command reads from standard input, for the tool's
	// content argument, if it reads it for its data.
	Stdin string
	// Min and Max are how many arguments it takes. Max below 0 is any number.
	Min, Max int
	// Run does it.
	Run func(*Call) error
	// What is the long description, if the summary isn't enough.
	What string
}

func (c *Command) words() []string { return strings.Fields(c.Name) }

func (c *Command) usageLine() string {
	var b strings.Builder
	b.WriteString(c.Name)
	if c.Args != "" {
		b.WriteString(" " + c.Args)
	}
	for _, f := range c.Flags {
		b.WriteString(" [" + f.usage() + "]")
	}
	return b.String()
}

func (c *Command) destructiveNote() string {
	if c.Name == "docker stop" || c.Name == "docker restart" {
		return "interrupts a running service"
	}
	return "changes or removes something that can't be put back"
}

// FlagKind is what a flag takes.
type FlagKind int

// The kinds of flag.
const (
	String FlagKind = iota
	Bool
	Int
	Duration
)

// A Flag is an option of a command: --tail 100.
type Flag struct {
	Name string
	// Short is a one-letter form such as 'f' for -f, or 0.
	Short byte
	Kind  FlagKind
	// Default is its value when it isn't given, as written.
	Default string
	Usage   string
	// Arg names the value in the usage, such as "n".
	Arg string
	// CLIOnly leaves the flag out of the MCP tool, because a tool answers once
	// and always in JSON.
	CLIOnly bool
}

// check reports whether val is a value this flag takes. name is how the
// message should call the flag.
func (f Flag) check(name, val string) error {
	switch f.Kind {
	case Int:
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Errorf("%s takes a number, not %q", name, val)
		}
	case Duration:
		if _, err := parseDuration(val); err != nil {
			return fmt.Errorf("%s takes a time such as 30m, 4h or 2d, not %q", name, val)
		}
	default:
	}
	return nil
}

func (f Flag) usage() string {
	s := "--" + f.Name
	if f.Short != 0 {
		s = "-" + string(f.Short) + "|" + s
	}
	if f.Kind != Bool {
		arg := f.Arg
		if arg == "" {
			arg = "value"
		}
		s += " <" + arg + ">"
	}
	return s
}

// commands is every command in this package, in the order of the help.
var commands []*Command

// find returns the command that args start with, and the arguments after its
// name. It takes the longest name: "alerts ack" before "alerts".
func find(args []string) (*Command, []string) {
	var best *Command
	bestLen := 0
	for _, c := range commands {
		w := c.words()
		if len(w) > len(args) || len(w) <= bestLen {
			continue
		}
		ok := true
		for i := range w {
			if args[i] != w[i] {
				ok = false
				break
			}
		}
		if ok {
			best, bestLen = c, len(w)
		}
	}
	if best == nil {
		return nil, args
	}
	return best, args[bestLen:]
}

// parseFlags reads the flags of a command in args, wherever they stand among
// the arguments, and returns their values by name and the arguments left.
// Nothing after "--" is a flag.
func parseFlags(flags []Flag, args []string) (map[string]string, []string, error) {
	vals := map[string]string{}
	var pos []string
	lookup := func(a string) (Flag, bool) {
		for _, f := range flags {
			if a == "--"+f.Name || len(a) == 2 && f.Short != 0 && a[0] == '-' && a[1] == f.Short {
				return f, true
			}
		}
		return Flag{}, false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return vals, append(pos, args[i+1:]...), nil
		case len(a) < 2 || a[0] != '-' || isNumber(a):
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(a, "=")
		f, ok := lookup(name)
		if !ok {
			return nil, nil, fmt.Errorf("unknown flag %s", name)
		}
		switch {
		case f.Kind == Bool:
			vals[f.Name] = "true"
			if hasVal {
				b, err := strconv.ParseBool(val)
				if err != nil {
					return nil, nil, fmt.Errorf("%s takes true or false, not %q", name, val)
				}
				vals[f.Name] = strconv.FormatBool(b)
			}
			continue
		case !hasVal:
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s needs a value", name)
			}
			i++
			val = args[i]
		}
		if err := f.check(name, val); err != nil {
			return nil, nil, err
		}
		vals[f.Name] = val
	}
	return vals, pos, nil
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// parseDuration reads a time such as 90s, 30m, 4h or 2d. Go's own format has
// no days.
func parseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("bad duration %q", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("bad duration %q", s)
	}
	return d, nil
}
