package ssh

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sshd runs every command, SFTP included, with the login shell of the
// account it logs in as. An account made with no login shell, such as the
// one install.sh makes for Bield, has nologin or false there, which prints
// "This account is currently not available." or nothing, and exits 1. So
// Bield can log in as such an account, but not run anything.

var (
	// ErrNoLoginShell is why AuthorizeHere refuses the user Bield runs as.
	ErrNoLoginShell = errors.New("the account has no login shell, so sshd runs no commands for it")
	// ErrShell is why Bield can't use a machine whose login shell, for the
	// account it logs in as, didn't run what Bield sent.
	ErrShell = errors.New("the account's login shell didn't run Bield's command")
)

// defaultPasswd is where AuthorizeHere looks up the login shell of the user
// Bield runs as.
const defaultPasswd = "/etc/passwd"

// noLoginShell reports whether shell is one that lets no one log in.
func noLoginShell(shell string) bool {
	switch filepath.Base(shell) {
	case "nologin", "false":
		return true
	}
	return false
}

// loginShell returns the login shell of the user name in passwd, a file in
// the format of /etc/passwd, and whether the user is there. An empty shell
// is /bin/sh.
func loginShell(passwd io.Reader, name string) (string, bool) {
	s := bufio.NewScanner(passwd)
	for s.Scan() {
		fields := strings.Split(s.Text(), ":")
		if len(fields) != 7 || fields[0] != name {
			continue
		}
		return cmp.Or(fields[6], "/bin/sh"), true
	}
	return "", false
}

// hubShell returns the login shell of the user Bield runs as, called name,
// from the passwd file, or "" if it isn't there, as on a Mac or with users
// from a directory.
func (m *Manager) hubShell(name string) string {
	f, err := os.Open(cmp.Or(m.opts.Passwd, defaultPasswd))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	shell, _ := loginShell(f, name)
	return shell
}

// shellError is ErrShell for a command that exited with code and printed
// out, which is what the login shell said, such as nologin's notice.
func shellError(code int, out []byte) error {
	said := printable(out)
	if said == "" {
		return fmt.Errorf("%w: it exited with status %d and said nothing. An account with no login shell, such as /usr/sbin/nologin or /bin/false, can't run commands; give it one, or log in as another user", ErrShell, code)
	}
	return fmt.Errorf("%w: it exited with status %d and said %q. An account with no login shell, such as /usr/sbin/nologin, can't run commands; give it one, or log in as another user", ErrShell, code, said)
}

// maxSaid is how much of what a login shell printed an error quotes.
const maxSaid = 200

// printable returns out trimmed, cut to maxSaid bytes, if it's text, or ""
// if it's empty or binary.
func printable(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > maxSaid {
		text = text[:maxSaid]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += "…"
	}
	for _, r := range text {
		if r == utf8.RuneError || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return ""
		}
	}
	return text
}

// sftpShellError explains an SFTP client that couldn't start because the
// login shell printed head before the SFTP server did, or ran none: what err
// says of that, such as "packet too long", is SFTP reading the shell's words
// as a packet. It returns err as it is if head is empty.
func sftpShellError(head []byte, err error) error {
	said := printable(head)
	if said == "" {
		return err
	}
	return fmt.Errorf("%w: SFTP didn't start, because the login shell printed %q first (%w). An account with no login shell, such as /usr/sbin/nologin, can't use SFTP; give it one, or log in as another user. Startup files that print for non-interactive shells break SFTP too", ErrShell, said, err)
}

// headReader keeps the first max bytes read through it.
type headReader struct {
	r    io.Reader
	head []byte
	max  int
}

func (h *headReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if room := h.max - len(h.head); room > 0 {
		h.head = append(h.head, p[:min(n, room)]...)
	}
	return n, err
}
