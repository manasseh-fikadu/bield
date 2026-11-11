package editor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeServer is a code-server for the scripts to install and start: it
// says its version, or serves the socket it's given until it's killed.
const fakeServer = `#!/bin/sh
if [ "$1" = --config ] && [ "$3" = --version ]; then echo "9.9.9 fake"; exit 0; fi
while [ $# -gt 0 ]; do
	case $1 in --socket) sock=$2; shift ;; esac
	shift
done
exec python3 -c '
import os, socket, sys, time
s = socket.socket(socket.AF_UNIX)
s.bind(sys.argv[1])
s.listen()
while True:
    c, _ = s.accept()
    c.sendall(b"hello\n")
    c.close()
' "$sock"
`

// tarball returns a gzipped tar of a build of fakeServer in dir, and its
// SHA-256.
func tarball(t *testing.T, dir string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
		body string
	}{
		{dir + "/", 0o755, ""},
		{dir + "/bin/", 0o755, ""},
		{dir + "/bin/code-server", 0o755, fakeServer},
	} {
		typ := byte(tar.TypeReg)
		if strings.HasSuffix(f.name, "/") {
			typ = tar.TypeDir
		}
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: typ}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// runScript runs a script as Bield runs it over SSH, with HOME set to home.
func runScript(t *testing.T, home, script string, args ...string) (string, int) {
	t.Helper()
	// Not t.Context(), which a cleanup can't use.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", append([]string{"-c", script, "sh"}, args...)...) //nolint:gosec // G204: the scripts under test.
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	}
	t.Fatalf("run the script: %v", err)
	return "", 0
}

func TestScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the scripts are for sh")
	}
	for _, tool := range []string{"python3", "curl", "tar", "ps"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed", tool)
		}
	}
	t.Parallel()
	// A socket's path has to fit in about 100 bytes, which a test's folder
	// in /tmp does.
	home := t.TempDir()
	root := filepath.Join(home, ".local/share/bield/code-server")
	const dir = "code-server-9.9.9-linux-amd64"
	tgz, sum := tarball(t, dir)
	file := filepath.Join(t.TempDir(), dir+".tar.gz")
	if err := os.WriteFile(file, tgz, 0o600); err != nil {
		t.Fatal(err)
	}
	url := "file://" + file
	status := func() Status {
		t.Helper()
		out, code := runScript(t, home, statusScript)
		if code != 0 {
			t.Fatalf("status failed: %s", out)
		}
		return parseStatus([]byte(out))
	}

	st := status()
	if st.System == "" || st.Machine == "" || st.Home != home || st.Version != "" || st.Running {
		t.Fatalf("before the install, status = %+v", st)
	}
	if want := filepath.Join(root, "run/code.sock"); st.Socket != want {
		t.Errorf("socket = %s, want %s", st.Socket, want)
	}
	if st.Free <= 0 {
		t.Errorf("free = %d, want what df says", st.Free)
	}

	// A download that doesn't match is refused.
	out, code := runScript(t, home, installScript, "9.9.9", url, strings.Repeat("0", 64), dir)
	if code == 0 || !strings.Contains(out, "so Bield didn't install it") {
		t.Errorf("install with the wrong SHA-256 = %d, %q", code, out)
	}
	if st := status(); st.Version != "" {
		t.Errorf("after the refused install, version = %q", st.Version)
	}

	out, code = runScript(t, home, installScript, "9.9.9", url, sum, dir)
	if code != 0 || !strings.Contains(out, "Installed code-server 9.9.9") {
		t.Fatalf("install = %d, %q", code, out)
	}
	if st := status(); st.Version != "9.9.9" || st.Running {
		t.Errorf("after the install, status = %+v", st)
	}
	if entries, _ := filepath.Glob(filepath.Join(root, ".install.*")); len(entries) > 0 {
		t.Errorf("the install left %v", entries)
	}
	for path, want := range map[string]os.FileMode{root: 0o700, filepath.Join(root, "config.yaml"): 0o600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Errorf("Stat(%s) = %v, %v, want mode %v", path, info, err, want)
		}
	}

	out, code = runScript(t, home, startScript, "120")
	if code != 0 || socketOf([]byte(out)) != st.Socket {
		t.Fatalf("start = %d, %q", code, out)
	}
	t.Cleanup(func() { runScript(t, home, stopScript) })
	if st := status(); !st.Running {
		t.Errorf("after the start, status = %+v", st)
	}
	info, err := os.Stat(st.Socket)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("Stat(socket) = %v, %v: others mustn't reach it", info, err)
	}
	if run, err := os.Stat(filepath.Join(root, "run")); err != nil || run.Mode().Perm() != 0o700 {
		t.Errorf("Stat(run) = %v, %v, want mode 0700", run, err)
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(t.Context(), "unix", st.Socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	// Starting it again finds it running.
	pidFile := filepath.Join(root, "run/pid")
	before, _ := os.ReadFile(pidFile) //nolint:gosec // G304: a file the test made.
	if out, code := runScript(t, home, startScript, "120"); code != 0 || socketOf([]byte(out)) == "" {
		t.Errorf("the second start = %d, %q", code, out)
	}
	if after, _ := os.ReadFile(pidFile); !bytes.Equal(before, after) { //nolint:gosec // G304: a file the test made.
		t.Errorf("the second start started another one: PID %s, then %s", before, after)
	}

	// An update stops it, since its files are replaced.
	if out, code := runScript(t, home, installScript, "9.9.9", url, sum, dir); code != 0 {
		t.Fatalf("update = %d, %q", code, out)
	}
	if st := status(); st.Running || st.Version != "9.9.9" {
		t.Errorf("after the update, status = %+v", st)
	}

	if out, code := runScript(t, home, startScript, "120"); code != 0 {
		t.Fatalf("start = %d, %q", code, out)
	}
	if out, code := runScript(t, home, stopScript); code != 0 {
		t.Fatalf("stop = %d, %q", code, out)
	}
	if st := status(); st.Running {
		t.Errorf("after the stop, status = %+v", st)
	}
	if _, err := os.Stat(st.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stop left the socket: %v", err)
	}

	// A PID file that names another program doesn't stop it.
	sleeper := exec.CommandContext(t.Context(), "sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	if err := os.WriteFile(filepath.Join(root, "run/pid"), []byte(strconv.Itoa(sleeper.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := status(); st.Running {
		t.Errorf("another program's PID counts as running")
	}
	runScript(t, home, stopScript)
	if err := sleeper.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("the stop killed another program: %v", err)
	}

	// Removing it can keep the settings and the extensions.
	if err := os.WriteFile(filepath.Join(root, "data", "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := runScript(t, home, removeScript, "keep"); code != 0 {
		t.Fatalf("remove, keeping the data = %d, %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "settings.json")); err != nil {
		t.Errorf("the settings went: %v", err)
	}
	if st := status(); st.Version != "" {
		t.Errorf("after the removal, version = %q", st.Version)
	}
	// code-server's heartbeat and logs, which it writes outside its folder.
	theirs := filepath.Join(home, ".local/share/code-server")
	if err := os.MkdirAll(filepath.Join(theirs, "coder-logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"heartbeat", "coder-logs/code-server-stdout.log", "coder-logs/code-server-stderr.log"} {
		if err := os.WriteFile(filepath.Join(theirs, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := runScript(t, home, removeScript, "all"); code != 0 {
		t.Fatalf("remove = %d, %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/share/bield")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the removal left Bield's folder: %v", err)
	}
	// Nothing went anywhere else in the home.
	if entries, _ := os.ReadDir(filepath.Join(home, ".local/share")); len(entries) != 0 {
		t.Errorf("left in ~/.local/share: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scripts wrote to ~/.config: %v", err)
	}
}

// Removing Bield's code-server leaves a code-server of the user's own alone,
// which shares the folder of the heartbeat and the logs.
func TestRemoveScript_keepsTheUsersCodeServer(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	theirs := filepath.Join(home, ".local/share/code-server")
	for _, dir := range []string{"coder-logs", "User", filepath.Join("..", "bield", "code-server", "data")} {
		if err := os.MkdirAll(filepath.Join(theirs, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(theirs, "heartbeat"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := runScript(t, home, removeScript, "all"); code != 0 {
		t.Fatalf("remove = %d, %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(theirs, "User")); err != nil {
		t.Errorf("the user's own code-server went: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/share/bield")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Bield's folder is still there: %v", err)
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()
	st := parseStatus([]byte("system=Darwin\nmachine=arm64\nhome=/Users/a\nsocket=/Users/a/s\nversion=4.1.0\nrunning=1\nfree=1024\nnoise\n"))
	want := Status{System: "Darwin", Machine: "arm64", Home: "/Users/a", Socket: "/Users/a/s", Version: "4.1.0", Running: true, Free: 1 << 20}
	if st != want {
		t.Errorf("parseStatus = %+v, want %+v", st, want)
	}
	if st := parseStatus([]byte("libc=musl\nfree=\n")); !st.Musl || st.Free != -1 {
		t.Errorf("parseStatus(musl, no df) = %+v", st)
	}
}
