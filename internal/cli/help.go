package cli

import (
	"fmt"
	"io"
	"strings"
)

// HelpText lists the commands of this package for bield's own help.
func HelpText() string {
	var b strings.Builder
	width := 0
	for _, c := range commands {
		width = max(width, len(c.Name)+1+len(c.Args))
	}
	for _, c := range commands {
		left := strings.TrimSpace(c.Name + " " + c.Args)
		note := ""
		if c.Admin {
			note = " (admins)"
		}
		fmt.Fprintf(&b, "  %-*s  %s%s\n", width, left, c.Summary, note)
	}
	return b.String()
}

const globalFlagsText = `Flags every command takes:
  --url <address>    Where Bield is (env BIELD_URL; "bield connect" saves it)
  --json             Print JSON, with the API's field names, and errors as JSON on stderr
  -y, --yes          Allow a command that stops, deletes or overwrites something
  --timeout <time>   How long to wait for Bield (default 30s)
  --no-color         No color (also off when piped, or with NO_COLOR set)
  -h, --help         Show help for a command
`

const exitCodesText = `Exit codes: 0 done, 1 failed, 2 wrong usage, 3 not found, 4 not allowed.
`

func printHelp(w io.Writer, rest []string) {
	if len(rest) >= 2 && rest[0] == "help" {
		if cmd, _ := find(rest[1:]); cmd != nil {
			printCommandHelp(w, cmd)
			return
		}
	}
	fmt.Fprintf(w, "Commands that talk to a running Bield:\n%s\n%s\n%s", HelpText(), globalFlagsText, exitCodesText)
}

func printCommandHelp(w io.Writer, c *Command) {
	fmt.Fprintf(w, "Usage: bield %s\n\n%s.", c.usageLine(), capitalize(c.Summary))
	switch {
	case c.Admin && c.Destructive:
		fmt.Fprint(w, " Needs an admin, and --yes.")
	case c.Admin:
		fmt.Fprint(w, " Needs an admin.")
	case c.Destructive:
		fmt.Fprint(w, " Needs --yes.")
	}
	fmt.Fprintln(w)
	if c.What != "" {
		fmt.Fprintf(w, "\n%s\n", c.What)
	}
	if len(c.Flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		width := 0
		for _, f := range c.Flags {
			width = max(width, len(f.usage()))
		}
		for _, f := range c.Flags {
			def := ""
			if f.Default != "" && f.Kind != Bool {
				def = " (default " + f.Default + ")"
			}
			fmt.Fprintf(w, "  %-*s  %s%s\n", width, f.usage(), f.Usage, def)
		}
	}
	fmt.Fprintf(w, "\n%s", globalFlagsText)
}
