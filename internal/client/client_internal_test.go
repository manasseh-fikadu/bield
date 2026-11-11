package client

import "testing"

func TestNewPart(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ hint, msg, want string }{
		{"", "no", ""},
		{"run it elsewhere", "only admins", "run it elsewhere"},
		{"The token is a viewer token", "the token is a viewer token", ""},
		{"The token does nothing now; ask an admin for a new token", "the token does nothing now", "ask an admin for a new token"},
	} {
		if got := newPart(tt.hint, tt.msg); got != tt.want {
			t.Errorf("newPart(%q, %q) = %q, want %q", tt.hint, tt.msg, got, tt.want)
		}
	}
}
