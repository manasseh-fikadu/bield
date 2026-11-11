package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/client"
)

// mcpInstructions is what the server tells a client about itself.
const mcpInstructions = "Bield watches the machines on a tailnet. These tools do what the bield command line does, " +
	"as the Tailscale user running this server, with that user's role. Results are the JSON of bield's --json output. " +
	"A failed call says why and, if it can, what to do about it. If a call is refused, don't look for a way around it; tell the user."

// maxToolResult is the most a tool answers with. It is more than a model
// should read at once, and a file or a log can be any size.
const maxToolResult = 2 << 20

func mcpCommands() []*Command {
	return []*Command{{
		Name: "mcp", Max: 0, Run: runMCP, Serves: true,
		NoTool:  "it is the server the tools are in",
		Summary: "run an MCP server over stdio, for AI agents that take typed tools",
		What: "An MCP client starts this and talks to it on stdin and stdout. It reaches Bield as the other commands do. " +
			"Without --allow-changes it only has the tools a viewer could use. See docs/mcp.md.",
		Flags: []Flag{{Name: "allow-changes", Kind: Bool, Usage: "add the tools that need an admin, including the ones that change or delete things"}},
	}}
}

func runMCP(c *Call) error {
	s := &mcpServer{env: c.Env, global: c.global}
	server := mcp.NewServer(&mcp.Implementation{Name: "bield", Version: c.Env.Version}, &mcp.ServerOptions{Instructions: mcpInstructions})
	for _, t := range mcpTools(c.Bool("allow-changes")) {
		server.AddTool(t.def, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// An error from here would be a protocol error, which a client
			// doesn't show the model. So every failure is a result.
			return s.call(ctx, t, req.Params.Arguments), nil
		})
	}
	in, ok := c.Env.Stdin.(io.ReadCloser)
	if !ok {
		in = io.NopCloser(c.Env.Stdin)
	}
	err := server.Run(c.Ctx, &mcp.IOTransport{Reader: in, Writer: nopWriteCloser{c.Env.Stdout}})
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// readOnly reports whether a command changes nothing.
func (c *Command) readOnly() bool { return !c.Admin || c.ReadOnly }

// A toolParam is an argument of a tool: one of the command's arguments or
// flags, or its standard input.
type toolParam struct {
	name     string
	schema   map[string]any
	required bool
	flag     *Flag
	stdin    bool
	list     bool
}

// A mcpTool is a command as a tool.
type mcpTool struct {
	cmd    *Command
	params []toolParam
	def    *mcp.Tool
}

// mcpTools makes the tools from the command table: all of them that a viewer
// could use, and the ones that need an admin if allowChanges is true.
func mcpTools(allowChanges bool) []mcpTool {
	var out []mcpTool
	for _, c := range commands {
		if c.NoTool != "" || (c.Admin && !allowChanges) {
			continue
		}
		params := toolParams(c)
		props := make(map[string]any, len(params))
		required := []string{}
		for _, p := range params {
			props[p.name] = p.schema
			if p.required {
				required = append(required, p.name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		destructive, closed := c.Destructive, false
		out = append(out, mcpTool{cmd: c, params: params, def: &mcp.Tool{
			Name:        toolName(c),
			Description: toolDescription(c),
			InputSchema: schema,
			Annotations: &mcp.ToolAnnotations{Title: "bield " + c.Name, ReadOnlyHint: c.readOnly(), DestructiveHint: &destructive, OpenWorldHint: &closed},
		}})
	}
	return out
}

func toolName(c *Command) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(c.Name)
}

// toolDescription says what a tool does. The sentences about --yes, --json and
// stderr are left out, since a tool has none of them.
func toolDescription(c *Command) string {
	var b strings.Builder
	b.WriteString(capitalize(c.Summary) + ".")
	for s := range strings.SplitAfterSeq(c.What, ". ") {
		if s != "" && !strings.Contains(s, "--yes") && !strings.Contains(s, "--json") && !strings.Contains(s, "stderr") {
			b.WriteString(" " + strings.TrimSpace(s))
		}
	}
	if c.Stdin != "" {
		b.WriteString(" Standard input is the content argument here.")
	}
	if c.Admin {
		b.WriteString(" Needs an admin.")
	}
	return b.String()
}

// argDescriptions says what each argument of a tool is, by its name.
//
//nolint:gosec // G101: "token" is the name of an argument, not a credential.
var argDescriptions = map[string]string{
	"device":    "the device: its ID or a name, as the devices tool lists them",
	"check":     "the check: its ID or name, as the checks tool lists them",
	"container": "the container: its name or the start of its ID",
	"id":        "the alert's ID, as the alerts tool lists them",
	"path":      "a path on the device",
	"text":      "the text of the note",
	"login":     "a Tailscale login, such as alice@example.com",
	"role":      "the role to give",
	"who":       "who can view Bield; leave it out to see the current value",
	"token":     "the API token: its name, as the tokens tool lists them, or its ID",
	"settings":  "the settings to change, each as name=value, such as thresholds.diskWarn=85",
}

// argNames names the arguments whose usage isn't a name.
var argNames = map[string]string{"name=value": "settings", "admin|viewer": "role", "tailnet|listed": "who"}

// toolParams works out a tool's arguments from the command's usage and flags.
func toolParams(c *Command) []toolParam {
	var out []toolParam
	for tok := range strings.FieldsSeq(c.Args) {
		out = append(out, positionalParam(tok))
	}
	for i := range c.Flags {
		f := &c.Flags[i]
		if !f.CLIOnly {
			out = append(out, flagParam(f))
		}
	}
	if c.Stdin != "" {
		out = append(out, toolParam{
			name: "content", required: true, stdin: true,
			schema: map[string]any{"type": "string", "description": c.Stdin},
		})
	}
	return out
}

// positionalParam reads an argument in a usage line: <device>, [path],
// <text...>, which is one piece of text, or <name=value>..., which is a list.
func positionalParam(tok string) toolParam {
	p := toolParam{required: tok[0] == '<'}
	p.list = strings.HasSuffix(tok, "...") && !strings.HasSuffix(tok, "...>")
	raw := strings.TrimSuffix(strings.Trim(strings.TrimSuffix(tok, "..."), "<>[]"), "...")
	p.name = raw
	if n, ok := argNames[raw]; ok {
		p.name = n
	}
	p.schema = map[string]any{"type": "string", "description": argDescriptions[p.name]}
	if strings.Contains(raw, "|") {
		p.schema["enum"] = strings.Split(raw, "|")
	}
	switch {
	case p.list:
		p.schema["type"] = "array"
		p.schema["minItems"] = 1
		p.schema["items"] = map[string]any{"type": "string"}
	case p.name == "id":
		p.schema["type"] = "integer"
	}
	return p
}

func flagParam(f *Flag) toolParam {
	schema := map[string]any{"description": f.Usage}
	switch f.Kind {
	case Bool:
		schema["type"] = "boolean"
	case Int:
		schema["type"] = "integer"
	case String, Duration:
		schema["type"] = "string"
	}
	if f.Default != "" {
		schema["default"] = f.Default
		if n, err := strconv.Atoi(f.Default); err == nil && f.Kind == Int {
			schema["default"] = n
		}
	}
	if strings.Contains(f.Arg, "|") {
		schema["enum"] = strings.Split(f.Arg, "|")
	}
	return toolParam{name: camel(f.Name), schema: schema, flag: f}
}

// camel writes a flag's name as the API writes its fields: alert-offline as
// alertOffline.
func camel(name string) string {
	head, rest, _ := strings.Cut(name, "-")
	var b strings.Builder
	b.WriteString(head)
	for w := range strings.SplitSeq(rest, "-") {
		b.WriteString(capitalize(w))
	}
	return b.String()
}

// mcpServer holds what the tools share.
type mcpServer struct {
	env    Env
	global globals

	mu sync.Mutex
	cl *client.Client
}

// client returns the client of the MCP server, which says so in its
// User-Agent. It looks for the address until it finds one, so that running
// bield connect doesn't need the agent restarted.
func (s *mcpServer) client() (*client.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cl != nil {
		return s.cl, nil
	}
	opts := (&Call{Env: s.env, global: s.global}).clientOptions()
	opts.Name = api.MCPUserAgent
	cl, err := client.New(opts)
	if err != nil {
		return nil, err
	}
	s.cl = cl
	return cl, nil
}

// call runs a tool's command, and returns what it printed or why it failed.
func (s *mcpServer) call(ctx context.Context, t mcpTool, raw json.RawMessage) *mcp.CallToolResult {
	vals, pos, stdin, err := t.bind(raw)
	if err != nil {
		return toolFailure(err)
	}
	cl, err := s.client()
	if err != nil {
		return toolFailure(err)
	}
	if s.global.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.global.timeout)
		defer cancel()
	}
	var out cappedBuffer
	env := s.env
	env.Stdin, env.Stdout, env.Stderr, env.Terminal = strings.NewReader(stdin), &out, io.Discard, false
	// A tool is always JSON, and asking the client to approve a destructive
	// tool takes the place of --yes.
	global := s.global
	global.json, global.yes = true, true
	call := &Call{Ctx: ctx, Env: env, cmd: t.cmd, global: global, flags: vals, Args: pos, client: cl}
	if err := call.checkArgs(); err != nil {
		return toolFailure(err)
	}
	if err := t.cmd.Run(call); err != nil {
		return toolFailure(cl.Explain(ctx, err))
	}
	if !utf8.Valid(out.b.Bytes()) {
		return toolFailure(errors.New("the answer isn't text, so a tool can't return it"))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.b.String()}}}
}

// toolFailure is the result for a call that failed, in the form of bield's
// --json errors.
func toolFailure(err error) *mcp.CallToolResult {
	text := err.Error()
	if b, jerr := json.Marshal(describe(err)); jerr == nil {
		text = string(b)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

var errToolResultTooBig = fmt.Errorf("the answer is over %d MiB, which is too long for a tool; ask for less, such as with a smaller limit or range", maxToolResult>>20)

// cappedBuffer holds a tool's output, and fails once there is too much.
type cappedBuffer struct{ b bytes.Buffer }

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.b.Len()+len(p) > maxToolResult {
		return 0, errToolResultTooBig
	}
	return c.b.Write(p)
}

// bind checks a call's arguments against the tool's and turns them into what
// a run of the command takes. Flags go straight into their values and never
// through the command line's parser, so a value that starts with a dash stays
// a value.
func (t mcpTool) bind(raw json.RawMessage) (vals map[string]string, pos []string, stdin string, err error) {
	args := map[string]any{}
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return nil, nil, "", usagef(t.cmd, "the arguments aren't a JSON object: %v", err)
		}
	}
	for name := range args {
		known := false
		for _, p := range t.params {
			known = known || p.name == name
		}
		if !known {
			return nil, nil, "", usagef(t.cmd, "%s has no argument called %q", toolName(t.cmd), name)
		}
	}
	vals = map[string]string{}
	for _, p := range t.params {
		v, given := args[p.name]
		given = given && v != nil
		if s, ok := v.(string); ok && s == "" && p.flag == nil && !p.stdin {
			given = false
		}
		if !given {
			if p.required {
				return nil, nil, "", usagef(t.cmd, "%s needs the argument %q", toolName(t.cmd), p.name)
			}
			continue
		}
		if p.list {
			items, ok := v.([]any)
			if !ok {
				return nil, nil, "", usagef(t.cmd, "%q is a list", p.name)
			}
			for _, it := range items {
				s, err := scalarText(p, it)
				if err != nil {
					return nil, nil, "", usagef(t.cmd, "%v", err)
				}
				pos = append(pos, s)
			}
			continue
		}
		s, err := scalarText(p, v)
		if err != nil {
			return nil, nil, "", usagef(t.cmd, "%v", err)
		}
		switch {
		case p.stdin:
			stdin = s
		case p.flag != nil:
			if err := p.flag.check("--"+p.flag.Name, s); err != nil {
				return nil, nil, "", usagef(t.cmd, "%v", err)
			}
			vals[p.flag.Name] = s
		default:
			pos = append(pos, s)
		}
	}
	return vals, pos, stdin, nil
}

// scalarText writes a JSON value as the text the command takes. A model that
// sends 85 for a string or "5" for an integer means what it says, so those are
// accepted. What can't be read as the argument's type is refused.
func scalarText(p toolParam, v any) (string, error) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case json.Number:
		s = x.String()
	case bool:
		s = strconv.FormatBool(x)
	default:
		return "", fmt.Errorf("%q can't be a list or an object", p.name)
	}
	switch p.schema["type"] {
	case "integer":
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return "", fmt.Errorf("%q takes a whole number, not %q", p.name, s)
		}
	case "boolean":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return "", fmt.Errorf("%q takes true or false, not %q", p.name, s)
		}
		s = strconv.FormatBool(b)
	}
	return s, nil
}
