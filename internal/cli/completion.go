package cli

import (
	"fmt"
	"slices"
	"strings"
)

// otherCommands are bield's commands that don't talk to a running Bield, which
// completion offers too.
var otherCommands = []string{"serve", "admin", "backup", "healthcheck", "version", "help"}

func completionCommands() []*Command {
	return []*Command{{
		Name: "completion", Args: "<bash|zsh|fish>", Min: 1, Max: 1, Run: runCompletion,
		Summary: "print a script that completes bield's commands and flags in a shell",
		What: "Bash: source <(bield completion bash). Zsh: source <(bield completion zsh). " +
			"Fish: bield completion fish | source. Put the line in the shell's startup file.",
	}}
}

func runCompletion(c *Call) error {
	var script string
	switch c.Args[0] {
	case "bash":
		script = bashCompletion()
	case "zsh":
		script = "autoload -U +X bashcompinit && bashcompinit\n" + bashCompletion()
	case "fish":
		script = fishCompletion()
	default:
		return usagef(c.cmd, "the shells are bash, zsh and fish, not %q", c.Args[0])
	}
	_, err := fmt.Fprint(c.Env.Stdout, script)
	return err
}

// flagWords lists a command's flags as a shell offers them, with the ones
// every command takes.
func flagWords(c *Command) []string {
	out := []string{"--url", "--json", "--yes", "--timeout", "--no-color", "--help"}
	if c != nil {
		for _, f := range c.Flags {
			out = append(out, "--"+f.Name)
		}
	}
	return out
}

// topWords returns the first words of the commands, then the other commands.
func topWords() []string {
	return slices.Concat(Names(), otherCommands)
}

// subWords returns the second words of the commands that start with first.
func subWords(first string) []string {
	var out []string
	for _, c := range commands {
		if w := c.words(); len(w) == 2 && w[0] == first {
			out = append(out, w[1])
		}
	}
	return out
}

func bashCompletion() string {
	var b strings.Builder
	b.WriteString("# bash completion for bield\n_bield() {\n")
	b.WriteString("\tlocal cur=\"${COMP_WORDS[COMP_CWORD]}\" key flags subs\n")
	fmt.Fprintf(&b, "\tif [ \"$COMP_CWORD\" -eq 1 ]; then\n\t\tCOMPREPLY=( $(compgen -W %q -- \"$cur\") )\n\t\treturn\n\tfi\n", strings.Join(topWords(), " "))
	b.WriteString("\tcase \"${COMP_WORDS[1]} ${COMP_WORDS[2]}\" in\n")
	for _, c := range commands {
		if len(c.words()) == 2 {
			fmt.Fprintf(&b, "\t\t%q) flags=%q ;;\n", c.Name, strings.Join(flagWords(c), " "))
		}
	}
	b.WriteString("\t\t*) key=\"${COMP_WORDS[1]}\"\n\t\t\tcase \"$key\" in\n")
	seen := map[string]bool{}
	for _, c := range commands {
		first := c.words()[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		var bare *Command
		for _, d := range commands {
			if d.Name == first {
				bare = d
			}
		}
		fmt.Fprintf(&b, "\t\t\t\t%s) flags=%q; subs=%q ;;\n", first, strings.Join(flagWords(bare), " "), strings.Join(subWords(first), " "))
	}
	b.WriteString("\t\t\t\t*) flags=\"--help\" ;;\n\t\t\tesac ;;\n\tesac\n")
	b.WriteString("\tif [ \"$COMP_CWORD\" -eq 2 ] && [ -n \"$subs\" ]; then\n\t\tCOMPREPLY=( $(compgen -W \"$subs $flags\" -- \"$cur\") )\n")
	b.WriteString("\telse\n\t\tCOMPREPLY=( $(compgen -W \"$flags\" -- \"$cur\") )\n\tfi\n}\ncomplete -F _bield bield\n")
	return b.String()
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString("# fish completion for bield\ncomplete -c bield -f\n")
	fmt.Fprintf(&b, "complete -c bield -n __fish_use_subcommand -a %q\n", strings.Join(topWords(), " "))
	seen := map[string]bool{}
	for _, c := range commands {
		first := c.words()[0]
		if seen[first] {
			continue
		}
		seen[first] = true
		if subs := subWords(first); len(subs) > 0 {
			fmt.Fprintf(&b, "complete -c bield -n \"__fish_seen_subcommand_from %s; and not __fish_seen_subcommand_from %s\" -a %q\n",
				first, strings.Join(subs, " "), strings.Join(subs, " "))
		}
	}
	for _, c := range commands {
		w := c.words()
		cond := "__fish_seen_subcommand_from " + w[len(w)-1]
		if len(w) == 2 {
			cond = fmt.Sprintf("__fish_seen_subcommand_from %s; and __fish_seen_subcommand_from %s", w[0], w[1])
		}
		for _, f := range c.Flags {
			short := ""
			if f.Short != 0 {
				short = " -s " + string(f.Short)
			}
			fmt.Fprintf(&b, "complete -c bield -n %q -l %s%s -d %q\n", cond, f.Name, short, f.Usage)
		}
	}
	for _, f := range []string{"url", "json", "yes", "timeout", "no-color", "help"} {
		fmt.Fprintf(&b, "complete -c bield -l %s\n", f)
	}
	return b.String()
}
