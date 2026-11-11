package editor

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// prelude starts every script. Everything Bield puts on a machine is under
// root, in the home of the user it logs in as: Tailscale SSH only forwards
// sockets in the home, /tmp and /run/user. Each version is a folder, current
// links to the one in use, and run holds the socket, the PID and the log.
// ours reports whether the PID file names Bield's code-server, so a PID that
// another program got since is left alone.
//
//nolint:dupword // sh ends nested ifs with fi and fi.
const prelude = `if [ -z "${HOME:-}" ]; then echo "HOME isn't set for the user Bield logs in as."; exit 1; fi
root="$HOME/.local/share/bield/code-server"
run="$root/run"
sock="$run/code.sock"
ours() {
	pid=$(cat "$run/pid" 2>/dev/null) && [ -n "$pid" ] && ps -p "$pid" -o args= 2>/dev/null | grep -q "bield/code-server"
}
stop() {
	if ours; then
		kill "$pid" 2>/dev/null
		i=0
		while [ "$i" -lt 20 ] && kill -0 "$pid" 2>/dev/null; do sleep 0.5 2>/dev/null || sleep 1; i=$((i + 1)); done
		if kill -0 "$pid" 2>/dev/null; then
			pkill -KILL -P "$pid" 2>/dev/null
			kill -KILL "$pid" 2>/dev/null
		fi
	fi
	rm -f "$run/pid" "$sock"
}
config() {
	[ -f "$root/config.yaml" ] || printf 'auth: none\ncert: false\n' >"$root/config.yaml"
}
`

// statusScript says what the machine is and what of code-server is on it,
// as key=value lines (see parseStatus).
const statusScript = prelude + `echo "system=$(uname -s)"
echo "machine=$(uname -m)"
echo "home=$HOME"
echo "socket=$sock"
if [ -x "$root/current/bin/code-server" ]; then
	v=$(readlink "$root/current" 2>/dev/null)
	echo "version=${v##*/}"
fi
if ours && [ -S "$sock" ]; then echo "running=1"; fi
if ls /lib/ld-musl-* >/dev/null 2>&1; then echo "libc=musl"; fi
echo "free=$(df -Pk "$HOME" 2>/dev/null | awk 'NR == 2 { print $4 }')"
`

// installScript downloads a build of code-server, checks its SHA-256,
// unpacks it, checks that it runs, and makes it the one in use, in place of
// any other version. A running editor stops first, since its files are
// replaced. $1 is the version, $2 the tarball's URL, $3 its SHA-256 and $4
// the folder it unpacks to.
const installScript = prelude + `version=$1 url=$2 sum=$3 dir=$4
umask 077
mkdir -p "$root" || exit 1
chmod 700 "$root"
stop
tmp=$(mktemp -d "$root/.install.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $url"
if command -v curl >/dev/null 2>&1; then
	curl -fsSL --retry 2 -o "$tmp/code-server.tar.gz" "$url" || { echo "This machine couldn't download code-server from GitHub."; exit 1; }
elif command -v wget >/dev/null 2>&1; then
	wget -q -O "$tmp/code-server.tar.gz" "$url" || { echo "This machine couldn't download code-server from GitHub."; exit 1; }
else
	echo "Neither curl nor wget is installed on this machine, so it can't download code-server."
	exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/code-server.tar.gz" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/code-server.tar.gz" | awk '{ print $1 }')
else
	echo "Neither sha256sum nor shasum is installed on this machine, so Bield can't check the download."
	exit 1
fi
if [ "$got" != "$sum" ]; then
	echo "The download's SHA-256 is $got, not $sum as Bield expects, so Bield didn't install it."
	exit 1
fi
echo "Checked the SHA-256, unpacking"
tar -xzf "$tmp/code-server.tar.gz" -C "$tmp" || { echo "Couldn't unpack the download."; exit 1; }
rm -f "$tmp/code-server.tar.gz"
config
if ! out=$("$tmp/$dir/bin/code-server" --config "$root/config.yaml" --version 2>&1); then
	echo "code-server doesn't run on this machine:"
	echo "$out" | tail -n 20
	exit 1
fi
rm -rf "${root:?}/$version"
mv "$tmp/$dir" "$root/$version" || exit 1
ln -sfn "$version" "$root/current" || exit 1
for old in "$root"/[0-9]*; do
	[ "${old##*/}" = "$version" ] || rm -rf "$old"
done
echo "Installed code-server $version"
`

// startScript starts code-server on its socket, unless it runs already,
// and waits for the socket. It answers socket=path. code-server stops by
// itself once no one has been connected for $1 seconds. It runs apart from
// the SSH session, which ends as soon as the script does.
const startScript = prelude + `bin="$root/current/bin/code-server"
if [ ! -x "$bin" ]; then echo "code-server isn't installed on this machine."; exit 3; fi
if ours && [ -S "$sock" ]; then echo "socket=$sock"; exit 0; fi
stop
umask 077
mkdir -p "$run" "$root/data" "$root/extensions" || exit 1
chmod 700 "$root" "$run"
config
nohup "$bin" --config "$root/config.yaml" --socket "$sock" --socket-mode 600 --auth none \
	--disable-telemetry --disable-update-check --disable-proxy --disable-getting-started-override \
	--idle-timeout-seconds "$1" --user-data-dir "$root/data" --extensions-dir "$root/extensions" \
	>"$run/log" 2>&1 </dev/null &
started=$!
echo "$started" >"$run/pid"
i=0
while [ ! -S "$sock" ]; do
	if ! kill -0 "$started" 2>/dev/null; then
		echo "code-server stopped as it started:"
		tail -n 20 "$run/log"
		rm -f "$run/pid"
		exit 1
	fi
	if [ "$i" -ge 150 ]; then
		echo "code-server didn't start in 30 seconds:"
		tail -n 20 "$run/log"
		exit 1
	fi
	sleep 0.2 2>/dev/null || sleep 1
	i=$((i + 1))
done
echo "socket=$sock"
`

// stopScript stops code-server if it runs.
const stopScript = prelude + `stop
echo "Stopped"
`

// removeScript stops code-server and deletes it. With $1 = keep, the
// settings and the extensions stay, for the next install. code-server also
// writes a heartbeat and its logs in ~/.local/share/code-server, whatever
// folder it's given, and that folder goes too, unless it holds anything
// else, such as a code-server of the user's own.
const removeScript = prelude + `stop
if [ "$1" = keep ]; then
	for old in "$root"/[0-9]* "$root/current" "$run" "$root/config.yaml"; do rm -rf "$old"; done
	echo "Removed code-server, and kept its settings and extensions in $root"
	exit 0
fi
rm -rf "$root"
rmdir "$HOME/.local/share/bield" 2>/dev/null
cs="$HOME/.local/share/code-server"
if [ -d "$cs" ] && [ -z "$(ls -A "$cs" | grep -v -x -e heartbeat -e coder-logs)" ] &&
	[ -z "$(ls -A "$cs/coder-logs" 2>/dev/null | grep -v -x -e code-server-stdout.log -e code-server-stderr.log)" ]; then
	rm -rf "$cs"
fi
echo "Removed code-server, its settings and its extensions"
`

// Status is what a machine has of code-server, as statusScript says.
type Status struct {
	// System and Machine are what uname -s and uname -m say, such as Linux
	// and x86_64.
	System, Machine string
	// Home is the home of the user Bield logs in as.
	Home string
	// Socket is the path code-server listens on when it runs.
	Socket string
	// Version is the version installed, or "".
	Version string
	// Running says code-server runs.
	Running bool
	// Musl says the machine's C library is musl, as on Alpine, which
	// code-server's builds don't run on.
	Musl bool
	// Free is how many bytes are free in Home, or -1 if df didn't say.
	Free int64
}

// parseStatus reads statusScript's output.
func parseStatus(out []byte) Status {
	st := Status{Free: -1}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "system":
			st.System = val
		case "machine":
			st.Machine = val
		case "home":
			st.Home = val
		case "socket":
			st.Socket = val
		case "version":
			st.Version = val
		case "running":
			st.Running = val == "1"
		case "libc":
			st.Musl = val == "musl"
		case "free":
			if kib, err := strconv.ParseInt(val, 10, 64); err == nil {
				st.Free = kib << 10
			}
		}
	}
	return st
}

// socketOf returns the path in startScript's answer.
func socketOf(out []byte) string {
	for line := range strings.Lines(string(out)) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "socket="); ok {
			return path
		}
	}
	return ""
}
