package ssh

import (
	"errors"
	"strings"
	"testing"
)

func TestNoLoginShell(t *testing.T) {
	for _, tt := range []struct {
		shell string
		want  bool
	}{
		{"/usr/sbin/nologin", true},
		{"/sbin/nologin", true},
		{"/bin/false", true},
		{"/usr/bin/false", true},
		{"/bin/bash", false},
		{"/usr/bin/fish", false},
		{"/bin/sh", false},
		// Unknown, as on a Mac, where /etc/passwd lists no one.
		{"", false},
	} {
		if got := noLoginShell(tt.shell); got != tt.want {
			t.Errorf("noLoginShell(%q) = %v, want %v", tt.shell, got, tt.want)
		}
	}
}

func TestLoginShell(t *testing.T) {
	const accounts = `root:x:0:0:root:/root:/bin/bash
# a comment
bield:x:998:998::/var/lib/bield:/usr/sbin/nologin
plain:x:1001:1001::/home/plain:
broken:x:1002
alice:x:1000:1000:Alice,,,:/home/alice:/usr/bin/fish
`
	for _, tt := range []struct {
		name  string
		want  string
		found bool
	}{
		{"bield", "/usr/sbin/nologin", true},
		{"alice", "/usr/bin/fish", true},
		{"root", "/bin/bash", true},
		// An empty shell is /bin/sh.
		{"plain", "/bin/sh", true},
		{"broken", "", false},
		{"bob", "", false},
		{"bie", "", false},
	} {
		got, found := loginShell(strings.NewReader(accounts), tt.name)
		if got != tt.want || found != tt.found {
			t.Errorf("loginShell(%q) = %q, %v, want %q, %v", tt.name, got, found, tt.want, tt.found)
		}
	}
}

func TestPrintable(t *testing.T) {
	long := strings.Repeat("é", maxSaid)
	for _, tt := range []struct {
		name string
		out  string
		want string
	}{
		{"nologin", "This account is currently not available.\n", "This account is currently not available."},
		{"empty", "", ""},
		{"spaces", " \n\t", ""},
		{"an SFTP packet", "\x00\x00\x00\x05\x02\x00\x00\x00\x03", ""},
		{"not UTF-8", "caf\xe9", ""},
		{"long", long, strings.Repeat("é", maxSaid/2) + "…"},
	} {
		if got := printable([]byte(tt.out)); got != tt.want {
			t.Errorf("%s: printable = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestShellErrors(t *testing.T) {
	err := shellError(1, []byte("This account is currently not available.\n"))
	if !errors.Is(err, ErrShell) || !strings.Contains(err.Error(), `status 1 and said "This account is currently not available."`) {
		t.Errorf("shellError = %v, want ErrShell with nologin's words", err)
	}
	if err := shellError(1, nil); !errors.Is(err, ErrShell) || !strings.Contains(err.Error(), "said nothing") {
		t.Errorf("shellError with no output = %v, want ErrShell that says so", err)
	}

	cause := errors.New("error receiving version packet from server: packet too long")
	err = sftpShellError([]byte("This account is currently not available.\n"), cause)
	if !errors.Is(err, ErrShell) || !strings.Contains(err.Error(), `printed "This account is currently not available."`) ||
		!strings.Contains(err.Error(), "packet too long") {
		t.Errorf("sftpShellError = %v, want ErrShell with what the shell printed and SFTP's error", err)
	}
	// A real SFTP server's answer isn't the shell talking.
	if err := sftpShellError([]byte("\x00\x00\x00\x05\x02\x00\x00\x00\x03"), cause); err != cause { //nolint:errorlint // It must be cause itself.
		t.Errorf("sftpShellError of a packet = %v, want %v as it was", err, cause)
	}
}
