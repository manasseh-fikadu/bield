package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/api"
)

func tokenCommands() []*Command {
	return []*Command{
		{
			Name: "tokens", Admin: true, ReadOnly: true, Run: runTokens,
			Summary: "list the API tokens, without the tokens themselves",
			What: "API tokens let a machine no person is logged in to, such as a tagged server or a CI runner, use Bield. " +
				"Only a person can list, make or revoke them, not a token.",
		},
		{
			Name: "tokens create", Args: "<name>", Min: 1, Max: 1, Admin: true, Run: runTokensCreate,
			Flags: []Flag{
				{Name: "role", Default: string(api.RoleViewer), Arg: "admin|viewer", Usage: "what the token may do"},
				{Name: "days", Kind: Int, Default: strconv.Itoa(api.DefaultTokenDays), Arg: "n",
					Usage: fmt.Sprintf("how many days it works, up to %d", api.MaxTokenDays)},
			},
			NoTool:  "it prints a new credential, which belongs with the person who will store it, not in an agent's context",
			Summary: "make an API token, and print it once",
			What: "The token alone goes to standard output, so it can go straight into a secret store, and Bield keeps only " +
				"its hash. It never has more than your role, now or later: if you become a viewer, so does an admin token " +
				"you made. Tokens can't open terminals, make tokens or change who has access. " +
				"Use it with BIELD_TOKEN=<token>, or save it with bield connect <url> --with-token.",
		},
		{
			Name: "tokens revoke", Args: "<token>", Min: 1, Max: 1, Admin: true, Destructive: true, Run: runTokensRevoke,
			Summary: "revoke an API token, by its name or ID",
			What:    "Whatever uses it is refused from the next request on.",
		},
	}
}

func runTokens(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	tokens, err := cl.Tokens(c.Ctx)
	if err != nil {
		return err
	}
	return c.Print(tokens, func(v *View) {
		if len(tokens) == 0 {
			v.Line("No API tokens. Make one with: bield tokens create <name>")
			return
		}
		t := v.Table("ID", "NAME", "ROLE", "MADE BY", "EXPIRES", "LAST USED")
		for _, tok := range tokens {
			role := string(tok.Role)
			switch {
			case tok.Expired:
			case tok.ActsAs == "":
				role += " (does nothing: its maker has no role)"
			case tok.ActsAs != tok.Role:
				role += " (acts as " + string(tok.ActsAs) + ")"
			}
			expires := v.until(tok.ExpiresAt)
			if tok.Expired {
				expires = v.Paint(bad, "expired "+v.ago(tok.ExpiresAt))
			}
			used := "never"
			if tok.LastUsedAt != nil {
				used = v.ago(*tok.LastUsedAt)
				if tok.LastUsedFrom != "" {
					used += " from " + tok.LastUsedFrom
				}
			}
			t.Row(strconv.FormatInt(tok.ID, 10), tok.Name, role, tok.CreatedBy, expires, used)
		}
		t.Write()
	})
}

func runTokensCreate(c *Call) error {
	role := api.Role(c.Str("role"))
	if role != api.RoleAdmin && role != api.RoleViewer {
		return usagef(c.cmd, "--role is admin or viewer, not %q", c.Str("role"))
	}
	if days := c.Int("days"); days < 1 || days > api.MaxTokenDays {
		return usagef(c.cmd, "--days is 1 to %d, not %s", api.MaxTokenDays, c.Str("days"))
	}
	cl, err := c.Client()
	if err != nil {
		return err
	}
	made, err := cl.NewToken(c.Ctx, api.NewAPIToken{Name: c.Args[0], Role: role, Days: c.Int("days")})
	if err != nil {
		return err
	}
	return c.Print(made, func(v *View) {
		// The token alone on stdout, so a script can store it.
		v.Line(made.Secret)
		c.Notef("Made the API token %s, %s %s token that works until %s. This is the only time Bield shows it.",
			made.Token.Name, article(made.Token.Role), made.Token.Role, clock(made.Token.ExpiresAt))
		c.Notef("Use it with BIELD_TOKEN=<token> bield ..., or save it: bield connect %s --with-token", cl.URL())
	})
}

func article(role api.Role) string {
	if role == api.RoleAdmin {
		return "an"
	}
	return "a"
}

func runTokensRevoke(c *Call) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	tokens, err := cl.Tokens(c.Ctx)
	if err != nil {
		return err
	}
	want := c.Args[0]
	var found *api.APIToken
	for i, t := range tokens {
		if strings.EqualFold(t.Name, want) || strconv.FormatInt(t.ID, 10) == want {
			found = &tokens[i]
			break
		}
	}
	if found == nil {
		return notFound("no API token is called %s; bield tokens lists them", want)
	}
	if err := cl.RevokeToken(c.Ctx, found.ID); err != nil {
		return err
	}
	return c.Print(map[string]any{"revoked": found.Name, "id": found.ID}, func(v *View) {
		v.Printf("Revoked the API token %s\n", found.Name)
	})
}
