package cli

import (
	"net/http"
	"testing"
)

func TestNewTSNode(t *testing.T) {
	t.Parallel()
	env := func(key string, hc *http.Client) Env {
		return Env{HTTP: hc, Getenv: func(k string) string { return map[string]string{authKeyEnv: key}[k] }}
	}
	for _, tt := range []struct {
		name string
		env  Env
		want bool
	}{
		{"no key", env("", nil), false},
		{"a key", env("tskey-auth-x", nil), true},
		// Tests, and anything else that sends requests its own way, win.
		{"a key and a client", env("tskey-auth-x", http.DefaultClient), false},
	} {
		if got := newTSNode(tt.env) != nil; got != tt.want {
			t.Errorf("%s: newTSNode() != nil = %v, want %v", tt.name, got, tt.want)
		}
	}
	var none *tsNode
	none.close() // closing no node is fine
}
