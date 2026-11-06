#!/bin/bash
# SPDX-License-Identifier: MIT
# Bield agent: sends a metrics heartbeat to the hub every INTERVAL seconds.
# Works on macOS (bash 3.2) and Linux. Needs only curl and standard system tools. Reading
# OpenCode sessions also takes sqlite3 or python3, and is skipped without either.
set -u

AGENT_VERSION="1.4.0"
CONF="${BIELD_AGENT_CONF:-$HOME/.config/bield-agent/env}"
# shellcheck disable=SC1090
[ -f "$CONF" ] && . "$CONF"
: "${SERVER_URL:?SERVER_URL is not set (expected in $CONF)}"
: "${SECRET:?SECRET is not set (expected in $CONF); enroll this machine again from the Agents page in Bield}"
INTERVAL="${INTERVAL:-15}"
OS="$(uname -s)"
export PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/usr/sbin:/sbin"
if [ "$OS" = Linux ]; then PAGE_SIZE=$(getconf PAGESIZE 2>/dev/null); fi
PAGE_SIZE="${PAGE_SIZE:-4096}"

json_str() {
  local s="$1"
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/ }
  s=${s//$'\r'/}
  s=${s//$'\t'/ }
  # JSON has no room for any other control character in a string, and a
  # hostname with one would make every heartbeat a 400.
  s=${s//[[:cntrl:]]/ }
  printf '"%s"' "$s"
}

# Prints the value if it is a number, otherwise JSON null.
num() {
  if [[ "${1:-}" =~ ^-?[0-9]+([.][0-9]+)?$ ]]; then printf '%s' "$1"; else printf 'null'; fi
}

# Per-process usage, shared by the macOS and Linux collectors: add() one process at a time,
# then emit() groups them, keeps the heaviest by CPU and by memory, and prints the heartbeat's
# "usage" object. Kept to POSIX awk (BWK on macOS, mawk/gawk/busybox on Linux): no gensub,
# asort, length(array), interval expressions or backslashes in gsub replacements.
USAGE_AWK='
function jstr(s,    n, a, i, o) {
  gsub(/[\001-\037\177]/, " ", s)
  n = split(s, a, "\\"); o = a[1]; for (i = 2; i <= n; i++) o = o "\\\\" a[i]; s = o
  n = split(s, a, "\""); o = a[1]; for (i = 2; i <= n; i++) o = o "\\\"" a[i]
  return "\"" o "\""
}
function add(pid, name, user, cpu, mem, group, cmd) {
  N++; PID[N] = pid; PN[N] = name; PU[N] = user; PC[N] = cpu; PM[N] = mem; PG[N] = group; PX[N] = cmd
}
# Linux only (macOS gets the user from ps); UNAME is filled from /etc/passwd there.
function userof(p,    f, line, a, u) {
  f = "/proc/" p "/status"; u = ""
  while ((getline line < f) > 0) if (line ~ /^Uid:/) { split(line, a, " "); u = a[2]; break }
  close(f)
  return (u in UNAME) ? UNAME[u] : u
}
# Marks in CHOSEN the indexes of the cnt largest non-zero val[1..n].
function pick(n, val, cnt,    k, i, best) {
  split("", USED)
  for (k = 0; k < cnt; k++) {
    best = 0
    for (i = 1; i <= n; i++) if (!(i in USED) && val[i] > 0 && (!best || val[i] > val[best])) best = i
    if (!best) break
    USED[best] = 1; CHOSEN[best] = 1
  }
}
function topnames(key,    i, k, nm, best, found, out) {
  split("", SCORE)
  for (i = 1; i <= N; i++) if (PG[i] == key) SCORE[PN[i]] += int(PC[i] * 10) * 1e13 + PM[i] + 1
  out = ""
  for (k = 0; k < 3; k++) {
    found = 0
    for (nm in SCORE) if (!found || SCORE[nm] > SCORE[best]) { best = nm; found = 1 }
    if (!found) break
    out = out (k ? "," : "") jstr(best)
    delete SCORE[best]
  }
  return out
}
function emit(    i, g, key, sep, tc, tm, u) {
  for (i = 1; i <= N; i++) {
    tc += PC[i]; tm += PM[i]; key = PG[i]
    if (!(key in GI)) { GI[key] = ++G; GK[G] = key }
    g = GI[key]; GC[g] += PC[i]; GM[g] += PM[i]; GN[g]++
  }
  split("", CHOSEN); pick(G, GC, 12); pick(G, GM, 12)
  printf "{\"groups\":["
  sep = ""
  for (g = 1; g <= G; g++) if (g in CHOSEN) {
    printf "%s[%s,%.2f,%.0f,%d,[%s]]", sep, jstr(GK[g]), GC[g], GM[g], GN[g], topnames(GK[g])
    sep = ","
  }
  split("", CHOSEN); pick(N, PC, 12); pick(N, PM, 12)
  printf "],\"procs\":["
  sep = ""
  for (i = 1; i <= N; i++) if (i in CHOSEN) {
    u = PU[i]
    if (u == "" && LINUX) u = userof(PID[i])
    printf "%s[%d,%s,%s,%.2f,%.0f,%s,%s]", sep, PID[i], jstr(PN[i]), jstr(u), PC[i], PM[i], jstr(PG[i]), jstr(substr(PX[i], 1, 160))
    sep = ","
  }
  printf "],\"total\":[%.2f,%.0f,%d]}\n", tc, tm, N
}
'

collect_macos() {
  cpu=$(top -l 2 -n 0 -s 1 2>/dev/null | awk '/CPU usage/ { idle=$7 } END { gsub("%", "", idle); if (idle != "") printf "%.1f", 100 - idle }')
  cores=$(sysctl -n hw.ncpu 2>/dev/null)
  mem_total=$(sysctl -n hw.memsize 2>/dev/null)
  local page
  page=$(sysctl -n hw.pagesize 2>/dev/null)
  mem_used=$(vm_stat 2>/dev/null | awk -v page="${page:-4096}" -F: '
    /Pages active/ { gsub(/[^0-9]/, "", $2); a = $2 }
    /Pages wired down/ { gsub(/[^0-9]/, "", $2); w = $2 }
    /Pages occupied by compressor/ { gsub(/[^0-9]/, "", $2); c = $2 }
    END { printf "%.0f", (a + w + c) * page }')
  read -r swap_total swap_used <<EOF
$(sysctl -n vm.swapusage 2>/dev/null | awk '{ t=$3; u=$6; gsub("M", "", t); gsub("M", "", u); printf "%.0f %.0f", t * 1048576, u * 1048576 }')
EOF
  local df_line
  df_line=$(df -k /System/Volumes/Data 2>/dev/null | tail -n 1)
  [ -z "$df_line" ] && df_line=$(df -k / | tail -n 1)
  disk_total=$(echo "$df_line" | awk '{ printf "%.0f", $2 * 1024 }')
  # APFS volumes share one container, so "used" is everything that isn't free.
  disk_used=$(echo "$df_line" | awk '{ printf "%.0f", ($2 - $4) * 1024 }')
  read -r load1 load5 load15 <<EOF
$(sysctl -n vm.loadavg 2>/dev/null | awk '{ print $2, $3, $4 }')
EOF
  local boot
  boot=$(sysctl -n kern.boottime 2>/dev/null | sed -E 's/^[{] sec = ([0-9]+).*/\1/')
  uptime_s=$(( $(date +%s) - ${boot:-$(date +%s)} ))
  procs=$(ps -A 2>/dev/null | wc -l | tr -d ' ')
  temp=""

  battery="null"
  local batt pct status source
  batt=$(pmset -g batt 2>/dev/null)
  if echo "$batt" | grep -q 'InternalBattery'; then
    pct=$(echo "$batt" | grep -Eo '[0-9]+%' | head -n 1 | tr -d '%')
    status=$(echo "$batt" | grep -Eo '[0-9]+%; [a-zA-Z ]+' | head -n 1 | sed -E 's/^[0-9]+%; //')
    source=$(echo "$batt" | head -n 1)
    local charging=false on_ac=false
    [ "$status" = "charging" ] && charging=true
    echo "$source" | grep -q "AC Power" && on_ac=true
    [ -n "$pct" ] && battery="{\"pct\":$pct,\"charging\":$charging,\"onAc\":$on_ac}"
  fi

  local iface
  iface=$(route -n get default 2>/dev/null | awk '/interface:/ { print $2 }')
  read -r net_rx net_tx <<EOF
$( [ -n "$iface" ] && netstat -ibn -I "$iface" 2>/dev/null | awk 'NR > 1 && $3 ~ /^<Link/ { print $7, $10; exit }')
EOF

  os_name="macOS"
  os_version="macOS $(sw_vers -productVersion 2>/dev/null)"
  model=$(sysctl -n hw.model 2>/dev/null)
  host=$(scutil --get ComputerName 2>/dev/null || hostname)
  usage=$(macos_usage)
}

# ps reports %CPU of one core (a decaying average) and RSS in KB. Apps are grouped by their
# outermost .app bundle, so Chrome's helpers count as "Google Chrome"; anything else by executable.
macos_usage() {
  LC_ALL=C ps -Ao pid=,pcpu=,rss=,user=,comm= 2>/dev/null | LC_ALL=C awk -v cores="${cores:-1}" "$USAGE_AWK"'
{
  c = $0
  sub(/^ *[0-9]+ +[0-9.]+ +[0-9]+ +[^ ]+ +/, "", c)
  base = c; sub(/.*\//, "", base)
  i = index(c, ".app/")
  if (c == "kernel_task") { kind = "kernel"; g = "Kernel" }
  else if (i) { g = substr(c, 1, i - 1); sub(/.*\//, "", g); kind = "app" }
  else { kind = "other"; g = base }
  add($1, base, $4, $2 / (cores > 0 ? cores : 1), $3 * 1024, kind ":" g, c)
}
END { emit() }'
}

cpu_sample() {
  awk '/^cpu / { idle = $5 + $6; total = 0; for (i = 2; i <= 9; i++) total += $i; print total, idle }' /proc/stat
}

collect_linux() {
  local t1 i1 t2 i2 p1 p2
  read -r t1 i1 <<EOF
$(cpu_sample)
EOF
  p1=$(cat /proc/[0-9]*/stat 2>/dev/null)
  sleep 1
  read -r t2 i2 <<EOF
$(cpu_sample)
EOF
  p2=$(cat /proc/[0-9]*/stat 2>/dev/null)
  usage=$(linux_usage "$p1" "$p2" $((t2 - t1)))
  cpu=$(awk -v t=$((t2 - t1)) -v i=$((i2 - i1)) 'BEGIN { if (t > 0) printf "%.1f", (1 - i / t) * 100 }')
  cores=$(grep -c '^processor' /proc/cpuinfo)
  read -r mem_total mem_used swap_total swap_used <<EOF
$(awk '/^MemTotal:/ { t = $2 } /^MemAvailable:/ { a = $2 } /^SwapTotal:/ { st = $2 } /^SwapFree:/ { sf = $2 }
  END { printf "%.0f %.0f %.0f %.0f", t * 1024, (t - a) * 1024, st * 1024, (st - sf) * 1024 }' /proc/meminfo)
EOF
  read -r disk_total disk_used <<EOF
$(df -kP / | awk 'NR == 2 { printf "%.0f %.0f", $2 * 1024, $3 * 1024 }')
EOF
  read -r load1 load5 load15 _ <<EOF
$(cat /proc/loadavg)
EOF
  uptime_s=$(awk '{ printf "%d", $1 }' /proc/uptime)
  procs=$(awk '{ split($4, a, "/"); print a[2] }' /proc/loadavg)

  temp=""
  local z t type
  for z in /sys/class/thermal/thermal_zone*; do
    [ -r "$z/temp" ] || continue
    type=$(cat "$z/type" 2>/dev/null)
    t=$(cat "$z/temp" 2>/dev/null)
    case "$type" in x86_pkg_temp|cpu*|soc*|coretemp*|acpitz) ;; *) [ -n "$temp" ] && continue ;; esac
    if [ -n "$t" ] && [ "$t" -gt 0 ] 2>/dev/null; then
      t=$(awk -v t="$t" 'BEGIN { printf "%.1f", t / 1000 }')
      temp="$t"
      [ "$type" = "x86_pkg_temp" ] && break
    fi
  done

  battery="null"
  local b sum=0 count=0 charging=false on_ac=false s
  for b in /sys/class/power_supply/BAT*; do
    [ -r "$b/capacity" ] || continue
    sum=$((sum + $(cat "$b/capacity")))
    count=$((count + 1))
    s=$(cat "$b/status" 2>/dev/null)
    [ "$s" = "Charging" ] && charging=true
  done
  for b in /sys/class/power_supply/*; do
    [ -r "$b/online" ] && [ "$(cat "$b/type" 2>/dev/null)" = "Mains" ] && [ "$(cat "$b/online")" = "1" ] && on_ac=true
  done
  [ "$count" -gt 0 ] && battery="{\"pct\":$((sum / count)),\"charging\":$charging,\"onAc\":$on_ac}"

  local iface
  iface=$(ip route show default 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit } }')
  read -r net_rx net_tx <<EOF
$( [ -n "$iface" ] && sed 's/:/ /' /proc/net/dev | awk -v ifc="$iface" '$1 == ifc { print $2, $10 }')
EOF

  os_name="linux"
  # shellcheck source=/dev/null
  os_version=$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-Linux}")
  model=$(cat /sys/devices/virtual/dmi/id/product_name 2>/dev/null)
  host=$(hostname)
}

# Per-process CPU from two /proc/[pid]/stat snapshots taken around the 1 s CPU sleep ($1, $2), as a
# share of the jiffies all CPUs spent in between ($3). Grouped by cgroup the same way the hub does.
linux_usage() {
  {
    printf '%s\n@@\n%s\n@@\n' "$1" "$2"
    # head labels each file ("==> /proc/1/cmdline <=="); /dev/null first so it does even for one pid.
    head -c 300 /dev/null /proc/[0-9]*/cmdline 2>/dev/null | tr '\0' ' '
  } | LC_ALL=C awk -v ticks="$3" -v page="$PAGE_SIZE" "$USAGE_AWK"'
# comm can hold spaces and parentheses, so it runs from the first "(" to the last ")".
function parse(s,    o, f) {
  o = index(s, "(")
  if (!o || !match(s, /\)[^)]*$/)) return 0
  S_pid = substr(s, 1, o - 2) + 0
  S_comm = substr(s, o + 1, RSTART - o - 1)
  if (split(substr(s, RSTART + 2), f, " ") < 22) return 0
  S_flags = f[7]; S_ticks = f[12] + f[13]; S_start = f[20]; S_rss = f[22]
  return 1
}
function cgroup(p,    f, line, v2, sd, i) {
  f = "/proc/" p "/cgroup"; v2 = ""; sd = ""
  while ((getline line < f) > 0) {
    if (substr(line, 1, 3) == "0::") v2 = substr(line, 4)
    else if (i = index(line, ":name=systemd:")) sd = substr(line, i + 14)
  }
  close(f)
  return (v2 != "" && v2 != "/") ? v2 : (sd != "" ? sd : v2)
}
function unit(s) { sub(/\.(service|scope|slice)$/, "", s); gsub(/\\x2d/, "-", s); return s }
function app(s,    o) {
  o = s; s = unit(s); sub(/^app-/, "", s)
  if (match(s, /-[0-9a-f]+$/) && (RLENGTH > 8 || substr(s, RSTART + 1) ~ /^[0-9]+$/)) s = substr(s, 1, RSTART - 1)
  return s == "" ? o : s
}
# Sets KIND and GNAME. Must classify processes the same way the hub does.
function classify(path, kthread, name,    n, i, m, at, id, uid) {
  KIND = "other"; GNAME = name
  if (kthread) { KIND = "kernel"; GNAME = "Kernel"; return }
  n = split(path, SEG, "/"); m = 0
  for (i = 1; i <= n; i++) if (SEG[i] != "") SG[++m] = SEG[i]
  if (!m) return
  for (i = 1; i <= m; i++) {
    id = ""
    if (SG[i] ~ /^(docker|libpod|cri-containerd|crio)-[0-9a-f]+\.scope$/) { id = SG[i]; sub(/\.scope$/, "", id); sub(/.*-/, "", id) }
    else if (SG[i] == "docker" && i < m && SG[i + 1] ~ /^[0-9a-f]+$/) id = SG[i + 1]
    if (length(id) == 64) { KIND = "container"; GNAME = substr(id, 1, 12); return }
  }
  if (SG[1] == "init.scope") { KIND = "service"; GNAME = "systemd"; return }
  if (SG[1] == "user.slice") {
    at = 0
    for (i = 2; i <= m; i++) if (SG[i] ~ /^user@[0-9]+\.service$/) { at = i; break }
    if (at) {
      KIND = "user-service"
      if (at == m || SG[at + 1] == "init.scope") { GNAME = "systemd --user"; return }
      for (i = at + 1; i <= m; i++) if (SG[i] ~ /\.service$/) { GNAME = unit(SG[i]); return }
      if (SG[at + 1] == "app.slice" && at + 2 <= m && SG[at + 2] ~ /^app-.*\.slice$/) { GNAME = app(SG[at + 2]); return }
      GNAME = app(SG[m]); return
    }
    for (i = 2; i <= m; i++) if (SG[i] ~ /^session-[^.]*\.scope$/) {
      uid = "unknown"
      if (SG[2] ~ /^user-[0-9]+\.slice$/) { uid = SG[2]; sub(/^user-/, "", uid); sub(/\.slice$/, "", uid); if (uid in UNAME) uid = UNAME[uid] }
      KIND = "session"; GNAME = "Login sessions (" uid ")"; return
    }
  }
  for (i = 1; i <= m; i++) if (SG[i] ~ /\.service$/) { KIND = "service"; GNAME = unit(SG[i]); return }
  GNAME = app(SG[m])
}
BEGIN {
  LINUX = 1; sec = 1
  while ((getline line < "/etc/passwd") > 0) { split(line, pw, ":"); UNAME[pw[3]] = pw[1] }
  close("/etc/passwd")
}
$0 == "@@" { sec++; next }
sec == 1 { if (parse($0)) { T0[S_pid] = S_ticks; ST0[S_pid] = S_start }; next }
sec == 2 {
  if (parse($0)) {
    m++; ID[m] = S_pid; CO[m] = S_comm; RSS[m] = S_rss
    KT[m] = int(S_flags / 2097152) % 2
    # Not in the first snapshot: it started during the sleep, so all its time counts.
    D[m] = (S_pid in T0 && ST0[S_pid] == S_start) ? S_ticks - T0[S_pid] : S_ticks
  }
  next
}
/^==> \/proc\/[0-9]+\/cmdline <==$/ { cur = $2; sub(/^\/proc\//, "", cur); sub(/\/cmdline$/, "", cur); want = 1; next }
want { CMD[cur] = $0; want = 0 }
END {
  for (j = 1; j <= m; j++) {
    p = ID[j]; name = CO[j]; c = CMD[p]; sub(/ +$/, "", c)
    a0 = c; sub(/ .*/, "", a0); sub(/.*\//, "", a0)
    # comm is cut at 15 characters, and Node renames its main thread; argv[0] reads better then.
    if (!KT[j] && a0 != "" && ((length(name) == 15 && index(a0, name) == 1) || name ~ /^(node-)?MainThread$/)) name = a0
    if (KT[j] || c == "") c = "[" CO[j] "]"
    classify(KT[j] ? "" : cgroup(p), KT[j], name)
    add(p, name, "", ticks > 0 ? D[j] * 100 / ticks : 0, RSS[j] * page, KIND ":" GNAME, c)
  }
  emit()
}'
}

build_payload() {
  usage=null
  case "$OS" in
    Darwin) collect_macos ;;
    Linux) collect_linux ;;
    *) echo "unsupported OS: $OS" >&2; exit 1 ;;
  esac
  # A truncated or empty usage object must not take the rest of the heartbeat down with it.
  case "$usage" in '{"groups":['*']}') ;; *) usage=null ;; esac
  payload=$(printf '{"v":1,"agentVersion":%s,"hostname":%s,"os":%s,"osVersion":%s,"kernel":%s,"model":%s,"arch":%s,"metrics":{"cpu":%s,"cores":%s,"load":[%s,%s,%s],"memUsed":%s,"memTotal":%s,"swapUsed":%s,"swapTotal":%s,"diskUsed":%s,"diskTotal":%s,"netRxTotal":%s,"netTxTotal":%s,"temp":%s,"battery":%s,"uptime":%s,"procs":%s},"usage":%s}' \
    "$(json_str "$AGENT_VERSION")" "$(json_str "$host")" \
    "$(json_str "$os_name")" "$(json_str "$os_version")" "$(json_str "$(uname -r)")" \
    "$(json_str "$model")" "$(json_str "$(uname -m)")" \
    "$(num "$cpu")" "$(num "$cores")" "$(num "$load1")" "$(num "$load5")" "$(num "$load15")" \
    "$(num "$mem_used")" "$(num "$mem_total")" "$(num "$swap_used")" "$(num "$swap_total")" \
    "$(num "$disk_used")" "$(num "$disk_total")" "$(num "${net_rx:-}")" "$(num "${net_tx:-}")" \
    "$(num "$temp")" "$battery" "$(num "$uptime_s")" "$(num "$procs")" "$usage")
}

# Sends one heartbeat. If it fails, says why on stderr, with what the hub
# answered, and leaves the HTTP status in STATUS.
send_heartbeat() {
  local out body msg
  STATUS=""
  build_payload
  # The secret goes through stdin (-K -) so it never shows up in `ps`. Bield
  # refuses POSTs without an X-Bield header, which pages on other sites can't send.
  out=$(printf 'header = "Authorization: Bearer %s"\n' "$SECRET" |
    curl -sS -m 10 -K - -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -H 'X-Bield: agent' \
      --data-binary "$payload" "$SERVER_URL/api/agent/heartbeat") || {
    echo "$(date '+%Y-%m-%d %H:%M:%S') heartbeat failed: couldn't reach $SERVER_URL" >&2
    return 1
  }
  STATUS=${out##*$'\n'}
  body=${out%$'\n'*}
  # Every answer says whether the hub wants coding sessions read: a 2xx with {"coding":{"enabled":true}}
  # in it when it does, and 204 when it does not. The machine the hub runs on gets a 409 that says so too.
  case "$STATUS" in
    2??)
      case "$body" in *'"coding":{"enabled":true'*) CODING=1 ;; *) CODING=0 ;; esac
      return 0
      ;;
  esac
  case "$body" in
    *'"coding":{"enabled":true'*) CODING=1 ;;
    *'"coding":{"enabled":false'*) CODING=0 ;;
  esac
  # Bield's errors look like {"error":"..."}, and may have the coding key after the message.
  msg=$(printf '%s' "$body" | sed -n -e 's/^{"error":"\(.*\)","coding":{[^}]*}}$/\1/p' -e t -e 's/^{"error":"\(.*\)"}$/\1/p')
  echo "$(date '+%Y-%m-%d %H:%M:%S') heartbeat failed with HTTP $STATUS: ${msg:-$body}" >&2
  return 1
}

# Coding sessions. When the hub says so (see the heartbeat's answer), the agent reads the files that
# AI coding tools already write on this machine and reports counts: tokens by model and hour, which
# sessions ran, and each tool's rate limits. Nothing a person typed or a model answered is read into
# the report. Claude Code, Codex and Pi write JSONL that awk reads from a saved offset, so a file is
# never read twice; OpenCode keeps SQLite, which sqlite3, or else python3, reads.
#
# The numbers sent are totals as the files show them, not changes: the hub keeps the larger of what
# it has and what arrives, so sending twice, losing the state below or rescanning is harmless.
# State, in $SDIR: f/<id>.dat per file read (its first line says how far it was read, the rest is
# what the awk programs made of it), status.tsv, sent.tsv (what the hub already has), lock/.
SESSIONS_INTERVAL="${SESSIONS_INTERVAL:-60}"
SESSIONS_DAYS="${SESSIONS_DAYS:-90}"
STATE_DIR="${BIELD_AGENT_STATE:-$HOME/.local/state/bield-agent}"
SDIR="$STATE_DIR/sessions"
TAB=$'\t'

# Shared by the awk programs below, in the style of USAGE_AWK: POSIX awk only, and no apostrophes
# in here, because the programs sit in single quotes. A program reads what its last run left for
# this file (S state, R usage, M session and L limit lines), then @@DATA, then new lines of the
# file, and prints the same kinds of lines again. A usage row is a total for a session, a model, a
# speed and an hour (UTC, YYYY-MM-DDTHH). in is uncached input, cr cache reads, cw cache writes and
# out output with reasoning in it; reason is the part of out that was reasoning.
# shellcheck disable=SC2016
SESS_AWK='
function clean(s) { gsub(/[\001-\037\177"\\]/, "", s); return s }
function base(p) { gsub(/\/+$/, "", p); sub(/.*\//, "", p); return substr(clean(p), 1, 80) }
# The number after the first "key": in s, or 0.
function numafter(s, key,    i) {
  i = index(s, "\"" key "\":")
  return i ? substr(s, i + length(key) + 3, 24) + 0 : 0
}
# The string after the first "key":" in s, up to its closing quote. Ids, models and times have no
# escapes in them. It is "" if there is none.
function strafter(s, key,    i, r) {
  i = index(s, "\"" key "\":\"")
  if (!i) return ""
  r = substr(s, i + length(key) + 4, 400)
  i = index(r, "\"")
  return i ? substr(r, 1, i - 1) : ""
}
function okts(t) { return t ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]/ }
# Milliseconds since the epoch of a time such as 2026-11-01T07:59:33.587Z.
function tsms(t,    y, m, d, era, yoe, doy, days) {
  y = substr(t, 1, 4) + 0; m = substr(t, 6, 2) + 0; d = substr(t, 9, 2) + 0
  if (m <= 2) y--
  era = int(y / 400); yoe = y - era * 400
  doy = int((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + d - 1
  days = era * 146097 + yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy - 719468
  return ((days * 24 + substr(t, 12, 2)) * 60 + substr(t, 15, 2)) * 60000 + substr(t, 18, 2) * 1000 + (substr(t, 20, 1) == "." ? substr(t, 21, 3) : 0)
}
# Adds one request to its row, and the session it belongs to to the sessions.
function add(sess, model, speed, ts, i, cr, cw, o, re, cost,    h, k) {
  h = substr(ts, 1, 13)
  if (h < cutoff || sess == "" || model == "") return
  k = sess "\t" model "\t" speed "\t" h
  if (!(k in RN)) { RK[++nk] = k; RI[k] = RC[k] = RW[k] = RO[k] = RR[k] = RN[k] = RM[k] = 0 }
  RI[k] += i; RC[k] += cr; RW[k] += cw; RO[k] += o; RR[k] += re; RN[k]++; RM[k] += cost
  ts = substr(ts, 1, 19) "Z"
  if (!(sess in MF)) { MS[++ns] = sess; MF[sess] = ts; ML[sess] = ts }
  if (ts < MF[sess]) MF[sess] = ts
  if (ts > ML[sess]) ML[sess] = ts
}
function project(sess, p) { if (p != "" && MP[sess] == "") MP[sess] = p }
# Keeps the newest sighting of each limit.
function limit(id, window, used, resets, status, plan, seen) {
  if ((id in LS) && seen < LS[id]) return
  if (!(id in LS)) LK[++nl] = id
  LS[id] = seen; LW[id] = window; LU[id] = used; LR[id] = resets; LT[id] = status; LP[id] = plan
}
!data && $0 == "@@DATA" { data = 1; next }
!data {
  split($0, f, "\t")
  if (f[1] == "S") ST[f[2]] = f[3]
  else if (f[1] == "R") {
    k = f[3] "\t" f[4] "\t" f[5] "\t" f[6]
    RK[++nk] = k; RI[k] = f[7] + 0; RC[k] = f[8] + 0; RW[k] = f[9] + 0; RO[k] = f[10] + 0; RR[k] = f[11] + 0; RN[k] = f[12] + 0; RM[k] = f[13] + 0
  } else if (f[1] == "M") {
    if (!(f[3] in MF)) { MS[++ns] = f[3]; MF[f[3]] = f[5]; ML[f[3]] = f[6] }
    MP[f[3]] = f[4]
  } else if (f[1] == "L") limit(f[3], f[4], f[5], f[6], f[7], f[8], f[9])
  next
}
data { parse($0) }
END {
  for (k in ST) printf "S\t%s\t%s\n", k, ST[k]
  for (j = 1; j <= nk; j++) {
    k = RK[j]; split(k, f, "\t")
    printf "R\t%s\t%s\t%s\t%s\t%s\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\n", tool, f[1], f[2], f[3], f[4], RI[k], RC[k], RW[k], RO[k], RR[k], RN[k], RM[k]
  }
  for (j = 1; j <= ns; j++) printf "M\t%s\t%s\t%s\t%s\t%s\n", tool, MS[j], MP[MS[j]], MF[MS[j]], ML[MS[j]]
  for (j = 1; j <= nl; j++) printf "L\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", tool, LK[j], LW[LK[j]], LU[LK[j]], LR[LK[j]], LT[LK[j]], LP[LK[j]], LS[LK[j]]
}
'

# Claude Code: ~/.claude/projects/**/*.jsonl, sub-agents included. Each response is written on
# several lines in a row, all with the whole usage, so only the first of a run counts. The keys of
# a line come in two orders, but message and usage are always laid out the same way.
CLAUDE_AWK='
function parse(l,    m, rest, p, u, us, mt, tail, head, ts, sess, model, id, req, key, cost, q, qt, qw) {
  if (index(l, "\"type\":\"assistant\"") == 0) return
  m = index(l, "\"message\":{")
  if (!m) return
  tail = substr(l, length(l) > 3000 ? length(l) - 2999 : 1)
  head = substr(l, 1, 400)
  ts = strafter(head, "timestamp")
  if (ts == "") ts = strafter(tail, "timestamp")
  if (!okts(ts)) return
  # Claude only writes limits when one is hit: the window and when it ends.
  q = index(l, "\"quotaLimits\":{")
  if (q) {
    q = substr(l, q, 400)
    if (strafter(q, "status") == "rejected") {
      qt = strafter(q, "rateLimitType")
      qw = qt == "five_hour" ? 300 : (qt == "seven_day" ? 10080 : 0)
      if (qt != "") limit(clean(qt), qw, "", numafter(q, "resetsAt"), "rejected", "", substr(ts, 1, 19) "Z")
    }
  }
  mt = substr(l, m + 11, 300)
  model = strafter(mt, "model")
  if (model == "" || model == "<synthetic>") return
  rest = substr(l, m)
  p = index(rest, "\"stop_reason\":")
  if (!p) return
  rest = substr(rest, p)
  u = index(rest, "\"usage\":{")
  if (!u) return
  id = strafter(mt, "id"); req = strafter(tail, "requestId")
  key = id ":" req
  if (key != ":" && key == ST["last"]) return
  ST["last"] = key
  us = substr(rest, u, 6000)
  sess = strafter(tail, "sessionId")
  cost = numafter(tail, "costUSD")
  add(clean(sess), clean(model), index(us, "\"speed\":\"fast\"") ? "fast" : "standard", ts, numafter(us, "input_tokens"), numafter(us, "cache_read_input_tokens"), numafter(us, "cache_creation_input_tokens"), numafter(us, "output_tokens"), 0, int(cost * 1000000 + 0.5))
  project(clean(sess), base(strafter(tail, "cwd")))
}
'

# Codex: ~/.codex/sessions/**/*.jsonl. The model comes from the latest turn_context and the speed
# from the latest thread_settings_applied. A token_count carries the counters of the whole session
# and of the last request; the whole-session ones are only used to spot a repeated event, since they
# start over now and then. Input includes what was cached. A forked session begins with a burst of
# copied history that the parent already counted, which ends at the first gap of a second.
CODEX_AWK='
function parse(l,    h, pl, ts, lt, tt, sig, ls, i, c, w, o, re, rl, pi, ps, id, plan) {
  h = substr(l, 1, 260)
  if (index(h, "\"type\":\"turn_context\"")) {
    pl = substr(l, index(l, "\"payload\":{"), 3000)
    if (strafter(pl, "model") != "") ST["model"] = clean(strafter(pl, "model"))
    return
  }
  if (index(h, "\"type\":\"session_meta\"")) {
    if (ST["meta"] == "") {
      ST["meta"] = 1
      pl = substr(l, index(l, "\"payload\":{"), 3000)
      id = strafter(pl, "id")
      if (id != "") ST["sess"] = clean(id)
      project(ST["sess"], base(strafter(pl, "cwd")))
      ts = strafter(h, "timestamp")
      if (okts(ts) && (index(l, "\"forked_from_id\":\"") || index(l, "\"parent_thread_id\":\""))) { ST["supp"] = 1; ST["anchor"] = tsms(ts) }
    }
    return
  }
  if (index(h, "\"payload\":{\"type\":\"thread_settings_applied\"")) {
    i = strafter(l, "service_tier")
    ST["speed"] = (i == "priority" || i == "fast" || i == "ultrafast") ? "fast" : "standard"
    return
  }
  if (index(h, "\"payload\":{\"type\":\"token_count\"") == 0) return
  ts = strafter(h, "timestamp")
  if (!okts(ts)) return
  # The limits come with every event, even ones with no usage.
  rl = index(l, "\"rate_limits\":{")
  if (rl) {
    rl = substr(l, rl, 1200)
    id = strafter(rl, "limit_id"); plan = strafter(rl, "plan_type")
    for (c = 1; c <= 2; c++) {
      pi = index(rl, c == 1 ? "\"primary\":{" : "\"secondary\":{")
      if (!pi) continue
      ps = substr(rl, pi, 200)
      w = numafter(ps, "window_minutes")
      if (w > 0 && id != "") limit(clean(id) (c == 1 ? ":primary" : ":secondary"), w, sprintf("%.2f", numafter(ps, "used_percent")), numafter(ps, "resets_at"), "ok", clean(plan), substr(ts, 1, 19) "Z")
    }
  }
  lt = index(l, "\"last_token_usage\":{"); tt = index(l, "\"total_token_usage\":{")
  if (!lt || !tt || ST["model"] == "") return
  sig = substr(l, tt, lt - tt)
  if (ST["supp"] == 1) {
    if (tsms(ts) - ST["anchor"] < 1000) { ST["anchor"] = tsms(ts); ST["sig"] = sig; return }
    ST["supp"] = 0; ST["sig"] = ""
  }
  ls = substr(l, lt, 400)
  i = numafter(ls, "input_tokens"); o = numafter(ls, "output_tokens")
  if (i + o == 0 || sig == ST["sig"]) { ST["sig"] = sig; return }
  ST["sig"] = sig
  c = numafter(ls, "cached_input_tokens"); w = numafter(ls, "cache_write_input_tokens"); re = numafter(ls, "reasoning_output_tokens")
  i = i - c - w; if (i < 0) i = 0
  add(ST["sess"] != "" ? ST["sess"] : clean(fsess), ST["model"], ST["speed"] == "fast" ? "fast" : "standard", ts, i, c, w, o, re, 0)
}
'

# Pi: ~/.pi/agent/sessions/**/*.jsonl. A session line names the session; message and usage lines
# carry the counters and the cost Pi worked out. Written from Pi source and made-up files: no Pi was
# at hand to try it on.
PI_AWK='
function parse(l,    t, ts, u, us, mt, prov, model, id, cost, role) {
  if (index(l, "{\"type\":\"session\"")) {
    if (strafter(l, "id") != "") ST["sess"] = clean(strafter(l, "id"))
    if (strafter(l, "cwd") != "") ST["proj"] = base(strafter(l, "cwd"))
    return
  }
  u = index(l, "\"usage\":{")
  if (!u || ST["sess"] == "") return
  ts = strafter(substr(l, 1, 400), "timestamp")
  if (!okts(ts)) return
  id = strafter(substr(l, 1, 400), "id")
  if (id != "" && id == ST["last"]) return
  ST["last"] = id
  mt = substr(l, 1, u)
  role = strafter(mt, "role")
  prov = strafter(mt, "provider"); model = strafter(mt, "responseModel")
  if (model == "") model = strafter(mt, "model")
  if (role == "assistant" && model != "" && prov != "") model = prov "/" model
  else if (model == "" || role == "toolResult") model = "tools-summaries"
  us = substr(l, u, 600)
  t = index(us, "\"cost\":{")
  cost = t ? numafter(substr(us, t), "total") : 0
  if (t) us = substr(us, 1, t - 1)
  add(ST["sess"], clean(model), "standard", ts, numafter(us, "input"), numafter(us, "cacheRead"), numafter(us, "cacheWrite"), numafter(us, "output"), numafter(us, "reasoning"), int(cost * 1000000 + 0.5))
  project(ST["sess"], ST["proj"])
}
'

# The report is made in two steps. The first adds up what every file said (a session can have
# several files, and so can its sub-agents) and keeps only what the hub does not have yet, unless
# all is 1; it also leaves the full totals in agg, which becomes sent.tsv once the hub has them.
# The second step, after sort, writes the request bodies.
# shellcheck disable=SC2016
REPORT1_AWK='
function sev(t) { return t == "ok" ? 0 : 1 }
BEGIN {
  while ((getline line < sent) > 0) SENT[line] = 1
  close(sent)
}
$1 == "T" { if (!($2 in TS) || sev($3) > sev(TS[$2])) TS[$2] = $3; next }
$1 == "R" {
  k = $2 "\t" $3 "\t" $4 "\t" $5 "\t" $6
  if (!(k in N)) { RK[++nk] = k; for (c = 7; c <= 13; c++) V[k, c] = 0 }
  N[k] = 1
  for (c = 7; c <= 13; c++) V[k, c] += $c
  next
}
$1 == "M" {
  k = $2 "\t" $3
  if (!(k in MF)) { MK[++nm] = k; MF[k] = $5; ML[k] = $6; MP[k] = "" }
  if ($5 < MF[k]) MF[k] = $5
  if ($6 > ML[k]) ML[k] = $6
  if (MP[k] == "") MP[k] = $4
  next
}
$1 == "L" {
  k = $2 "\t" $3
  if (!(k in LS) || $9 >= LS[k]) { if (!(k in LS)) LK[++nl] = k; LS[k] = $9; LV[k] = $4 "\t" $5 "\t" $6 "\t" $7 "\t" $8 "\t" $9 }
}
function out(line,    dirty) {
  print line > agg
  dirty = all || !(line in SENT)
  return dirty
}
END {
  # The tools and limits are small, so one change sends them all.
  for (t in TS) { tl[++nt] = "T\t" t "\t" TS[t]; if (out(tl[nt])) chg = 1 }
  for (j = 1; j <= nl; j++) { ll[j] = "L\t" LK[j] "\t" LV[LK[j]]; if (out(ll[j])) chg = 1 }
  if (chg) { for (j = 1; j <= nt; j++) print tl[j]; for (j = 1; j <= nl; j++) print ll[j] }
  for (j = 1; j <= nk; j++) {
    k = RK[j]; line = "U\t" k
    for (c = 7; c <= 13; c++) line = line sprintf("\t%.0f", V[k, c])
    if (out(line)) print line
  }
  for (j = 1; j <= nm; j++) {
    k = MK[j]; line = "S\t" k "\t" MP[k] "\t" MF[k] "\t" ML[k]
    if (out(line)) print line
  }
  close(agg)
}
'

# shellcheck disable=SC2016
REPORT2_AWK='
function chunk(    f, s, i, first) {
  # A report that is shown is a whole document even when it is empty.
  if (mode != "print" && ((nu + ns == 0 && n > 0) || nu + ns + nl + nt == 0)) return
  s = "{\"v\":1,\"tools\":[" (n == 0 ? tj : "") "],\"sessions\":["
  for (i = 1; i <= ns; i++) s = s (i > 1 ? "," : "") sj[i]
  s = s "],\"usage\":["
  for (i = 1; i <= nu; i++) s = s (i > 1 ? "," : "") uj[i]
  s = s "],\"limits\":[" (n == 0 ? lj : "") "]}"
  n++
  if (mode == "print") print s
  else { f = outdir "/chunk." n ".json"; print s > f; close(f) }
  nu = 0; ns = 0
}
$1 == "T" { tj = tj (tj == "" ? "" : ",") sprintf("{\"tool\":\"%s\",\"status\":\"%s\"}", $2, $3); nt++; next }
$1 == "L" {
  lj = lj (lj == "" ? "" : ",") sprintf("{\"tool\":\"%s\",\"id\":\"%s\",\"window\":%d,\"used\":%s,\"resets\":%.0f,\"status\":\"%s\",\"plan\":\"%s\",\"seen\":\"%s\"}", $2, $3, $4, $5 == "" ? "null" : $5, $6, $7, $8, $9)
  nl++; next
}
$1 == "U" {
  uj[++nu] = sprintf("{\"tool\":\"%s\",\"session\":\"%s\",\"model\":\"%s\",\"speed\":\"%s\",\"hour\":\"%s\",\"in\":%s,\"cr\":%s,\"cw\":%s,\"out\":%s,\"reason\":%s,\"n\":%s,\"cost\":%.6f}", $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13 / 1000000)
  if (mode != "print" && nu >= maxrows) chunk()
  next
}
$1 == "S" {
  sj[++ns] = sprintf("{\"tool\":\"%s\",\"id\":\"%s\",\"project\":\"%s\",\"first\":\"%s\",\"last\":\"%s\"}", $2, $3, $4, $5, $6)
  if (mode != "print" && ns >= maxrows) chunk()
}
END { chunk() }
'

# Python reads OpenCode when there is no sqlite3 (Debian servers often have python3 and no sqlite3).
OC_PY='import sqlite3, sys, urllib.parse
c = sqlite3.connect("file:" + urllib.parse.quote(sys.argv[1]) + "?mode=ro", uri=True, timeout=2)
for q in sys.argv[2:]:
    for r in c.execute(q):
        print("\t".join("" if v is None else str(v) for v in r))'

if stat -c %s /dev/null >/dev/null 2>&1; then STAT_GNU=1; else STAT_GNU=0; fi
sess_size() { if [ "$STAT_GNU" = 1 ]; then stat -c %s "$1" 2>/dev/null; else stat -f %z "$1" 2>/dev/null; fi; }
sess_mtime() { if [ "$STAT_GNU" = 1 ]; then stat -c %Y "$1" 2>/dev/null; else stat -f %m "$1" 2>/dev/null; fi; }
# The UTC hour of a Unix time, as YYYY-MM-DDTHH. BSD date, which macOS has, takes -r; its -d is
# something else, so it is not tried there.
sess_hour() {
  if [ "$OS" = Darwin ]; then
    date -u -r "$1" '+%Y-%m-%dT%H'
  else
    date -u -d "@$1" '+%Y-%m-%dT%H' 2>/dev/null || date -u -r "$1" '+%Y-%m-%dT%H'
  fi
}

sessions_init() {
  local now
  mkdir -p "$SDIR/f" || return 1
  chmod 700 "$STATE_DIR" "$SDIR" 2>/dev/null
  now=$(date +%s)
  SESS_CUTOFF=$(sess_hour $((now - SESSIONS_DAYS * 86400)))
  SESS_CUTOFF_MS=$(((now - SESSIONS_DAYS * 86400) * 1000))
}

# Prints "tool<TAB>directory" for every place that holds sessions. BIELD_SESSION_HOMES lists the
# home directories to read, separated by spaces; it is $HOME if not set. The tools own variables
# (CLAUDE_CONFIG_DIR and so on) only apply to the home of the user the agent runs as.
sess_dirs() {
  local home own c x p d
  for home in ${BIELD_SESSION_HOMES:-$HOME}; do
    own=0
    [ "$home" = "$HOME" ] && own=1
    c="$home/.claude"
    [ "$own" = 1 ] && [ -n "${CLAUDE_CONFIG_DIR:-}" ] && c="$CLAUDE_CONFIG_DIR"
    [ -d "$c/projects" ] && printf 'claude\t%s\n' "$c/projects"
    x="$home/.codex"
    [ "$own" = 1 ] && [ -n "${CODEX_HOME:-}" ] && x="$CODEX_HOME"
    [ -d "$x/sessions" ] && printf 'codex\t%s\n' "$x/sessions"
    p="$home/.pi/agent"
    [ "$own" = 1 ] && [ -n "${PI_CODING_AGENT_DIR:-}" ] && p="$PI_CODING_AGENT_DIR"
    d="$p/sessions"
    [ "$own" = 1 ] && [ -n "${PI_CODING_AGENT_SESSION_DIR:-}" ] && d="$PI_CODING_AGENT_SESSION_DIR"
    [ -d "$d" ] && printf 'pi\t%s\n' "$d"
    x="$home/.local/share"
    [ "$own" = 1 ] && [ -n "${XDG_DATA_HOME:-}" ] && x="$XDG_DATA_HOME"
    [ -d "$x/opencode" ] && printf 'opencode\t%s\n' "$x/opencode"
  done
  return 0
}

# "size mtime path" of the JSONL files under a directory that changed within SESSIONS_DAYS.
sess_list() {
  if [ "$STAT_GNU" = 1 ]; then
    find "$1" -type f -name '*.jsonl' -mtime "-$SESSIONS_DAYS" -exec stat -c '%s %Y %n' {} + 2>/dev/null
  else
    find "$1" -type f -name '*.jsonl' -mtime "-$SESSIONS_DAYS" -exec stat -f '%z %m %N' {} + 2>/dev/null
  fi
}

# The file in $SDIR/f that holds what is known about a source file. Its name comes from a checksum
# of the path; the first line of the file says which path it is, so a clash gets another name.
sess_dat() {
  local a b id first
  read -r a b _ <<EOF
$(printf '%s' "$1" | cksum)
EOF
  id="$a-$b"
  while [ -f "$SDIR/f/$id.dat" ]; do
    IFS=$'\t' read -r _ first _ < "$SDIR/f/$id.dat"
    [ "$first" = "$1" ] && break
    id="$id-c"
  done
  printf '%s/f/%s.dat' "$SDIR" "$id"
}

# The lines of a file that can matter to the awk program of a tool, so that awk sees a few
# percent of what the file holds.
sess_filter() {
  case "$1" in
    claude) grep -a -F -e '"usage":{' -e '"quotaLimits":{' ;;
    codex) grep -a -F -e '"session_meta"' -e '"turn_context"' -e '"token_count"' -e '"thread_settings_applied"' ;;
    *) grep -a -F -e '"usage":{' -e '{"type":"session"' ;;
  esac
}
sess_prog() {
  case "$1" in
    claude) printf '%s' "$CLAUDE_AWK" ;;
    codex) printf '%s' "$CODEX_AWK" ;;
    *) printf '%s' "$PI_AWK" ;;
  esac
}

# A checksum of the first 512 bytes of a file, or of the first n if n is less.
sess_guard() {
  local n="$2"
  [ "$n" -gt 512 ] && n=512
  head -c "$n" "$1" 2>/dev/null | cksum | tr ' ' -
}

# Reads what is new in one JSONL file: from where the last run stopped to the last complete line,
# so a line that is still being written is read next time. A file that shrank, or begins
# differently, is read again from the start.
sess_scan_file() {
  local tool="$1" size="$2" path="$3" dat off hd nhead last plen size2 consumed have fsess rc
  dat=$(sess_dat "$path")
  off=0
  hd=""
  have=0
  if [ -f "$dat" ]; then
    IFS=$'\t' read -r _ _ off _ hd < "$dat"
    have=1
  fi
  # The first bytes of the part that was read tell a file that grew from one that was written
  # again: they do not change when a file grows.
  if [ "$have" = 1 ] && [ "${off:-0}" -gt 0 ]; then
    nhead=$(sess_guard "$path" "$off")
    if [ "$size" -lt "$off" ] || [ "$nhead" != "$hd" ]; then
      have=0
      off=0
    fi
  fi
  [ "$have" = 1 ] || off=0
  last=$(tail -c 1 "$path" 2>/dev/null | od -An -tx1 | tr -d ' \n')
  plen=0
  [ "$last" != 0a ] && plen=$(tail -n 1 "$path" 2>/dev/null | wc -c | tr -d ' ')
  # A file that grew meanwhile is left for next time.
  size2=$(sess_size "$path")
  [ "$size2" = "$size" ] || return 0
  consumed=$((size - plen))
  [ "$consumed" -ge "$off" ] || consumed=$off
  fsess=${path##*/}
  fsess=${fsess%.jsonl}
  [ "${#fsess}" -gt 36 ] && fsess=${fsess: -36}
  {
    [ "$have" = 1 ] && tail -n +2 "$dat"
    printf '@@DATA\n'
    [ "$consumed" -gt "$off" ] && tail -c +$((off + 1)) "$path" | head -c $((consumed - off)) | sess_filter "$tool"
  } | LC_ALL=C awk -v tool="$tool" -v cutoff="$SESS_CUTOFF" -v fsess="$fsess" "$SESS_AWK$(sess_prog "$tool")" > "$dat.new"
  rc=$?
  if [ "$rc" -ne 0 ]; then
    rm -f "$dat.new"
    printf '%s\tunreadable\n' "$tool" >> "$SDIR/status.new"
    return 1
  fi
  nhead=$(sess_guard "$path" "$consumed")
  { printf 'O\t%s\t%s\t%s\t%s\n' "$path" "$consumed" "$size" "$nhead"; cat "$dat.new"; } > "$dat.tmp" && mv -f "$dat.tmp" "$dat"
  rm -f "$dat.new"
}

# Runs SQL statements on a database and prints the rows, tab separated: with sqlite3 if there is
# one, else with python3. Returns 3 if there is neither.
sess_sql() {
  local db="$1" s sql="" out
  shift
  if command -v sqlite3 >/dev/null 2>&1; then
    for s in "$@"; do sql="$sql$s;"; done
    out=$(sqlite3 -readonly -cmd '.timeout 2000' -separator "$TAB" "$db" "$sql" 2>/dev/null) && { printf '%s\n' "$out"; return 0; }
  fi
  if command -v python3 >/dev/null 2>&1; then
    out=$(python3 -I -c "$OC_PY" "$db" "$@" 2>/dev/null) && { printf '%s\n' "$out"; return 0; }
    return 1
  fi
  command -v sqlite3 >/dev/null 2>&1 && return 1
  return 3
}

# What OpenCode saved: D (session, directory), U (session, model, hour, in, cr, cw, out, reason,
# requests, cost in millionths of a dollar) and S (session, first, last) lines. OpenCode keeps one
# row per message, with its tokens in JSON, in session_message or, in older versions, message.
# Input is uncached, output leaves reasoning out, and a cost of 0 means OpenCode did not know it.
sess_oc_rows() {
  local db="$1" tables tbl cond dirq t="data" f all
  tables=$(sess_sql "$db" "SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('session_message', 'message', 'session_v2', 'session')") || return $?
  if printf '%s\n' "$tables" | grep -qx session_message; then
    tbl=session_message
    cond="type = 'assistant'"
  elif printf '%s\n' "$tables" | grep -qx message; then
    tbl=message
    cond="json_extract(data, '\$.role') = 'assistant'"
  else
    return 0
  fi
  if printf '%s\n' "$tables" | grep -qx session_v2; then
    dirq="SELECT 'D', id, directory FROM session_v2"
  elif printf '%s\n' "$tables" | grep -qx session; then
    dirq="SELECT 'D', id, directory FROM session"
  else
    dirq="SELECT 'D', '', ''"
  fi
  # A message that is still being written has no tokens yet, and is not a request.
  all="COALESCE(json_extract($t, '\$.tokens.input'), 0) + COALESCE(json_extract($t, '\$.tokens.cache.read'), 0)
       + COALESCE(json_extract($t, '\$.tokens.cache.write'), 0) + COALESCE(json_extract($t, '\$.tokens.output'), 0)
       + COALESCE(json_extract($t, '\$.tokens.reasoning'), 0)"
  f="FROM $tbl WHERE $cond AND time_created >= $SESS_CUTOFF_MS AND ($all) > 0"
  sess_sql "$db" "$dirq" \
    "SELECT 'U', session_id, COALESCE(json_extract($t, '\$.model.id'), json_extract($t, '\$.modelID'), ''),
       strftime('%Y-%m-%dT%H', time_created / 1000, 'unixepoch'),
       SUM(COALESCE(json_extract($t, '\$.tokens.input'), 0)),
       SUM(COALESCE(json_extract($t, '\$.tokens.cache.read'), 0)),
       SUM(COALESCE(json_extract($t, '\$.tokens.cache.write'), 0)),
       SUM(COALESCE(json_extract($t, '\$.tokens.output'), 0) + COALESCE(json_extract($t, '\$.tokens.reasoning'), 0)),
       SUM(COALESCE(json_extract($t, '\$.tokens.reasoning'), 0)), COUNT(*),
       CAST(ROUND(SUM(CASE WHEN COALESCE(json_extract($t, '\$.cost'), 0) > 0 THEN json_extract($t, '\$.cost') ELSE 0 END) * 1000000) AS INTEGER)
     $f GROUP BY 2, 3, 4" \
    "SELECT 'S', session_id, strftime('%Y-%m-%dT%H:%M:%SZ', MIN(time_created) / 1000, 'unixepoch'),
       strftime('%Y-%m-%dT%H:%M:%SZ', MAX(time_created) / 1000, 'unixepoch') $f GROUP BY 2"
}

# Reads every OpenCode database in a directory again when it changed, or changed lately (the
# time stamps are only to the second). What comes out is already a total, so no offset is kept.
sess_opencode() {
  local db dat sig m1 m2 now out rc osig
  for db in "$1"/opencode.db "$1"/opencode-*.db; do
    [ -f "$db" ] || continue
    dat=$(sess_dat "$db")
    now=$(date +%s)
    m1=$(sess_mtime "$db")
    m2=$(sess_mtime "$db-wal")
    [ "${m2:-0}" -gt "${m1:-0}" ] && m1=$m2
    sig="$(sess_size "$db") $(sess_mtime "$db") $(sess_size "$db-wal") ${m2:-}"
    if [ -f "$dat" ]; then
      IFS=$'\t' read -r _ _ osig < "$dat"
      if [ "$osig" = "$sig" ] && [ $((now - ${m1:-0})) -gt 120 ]; then
        printf 'opencode\tok\n' >> "$SDIR/status.new"
        continue
      fi
    fi
    out=$(sess_oc_rows "$db")
    rc=$?
    case "$rc" in
      0) ;;
      3) printf 'opencode\tneeds-sqlite\n' >> "$SDIR/status.new"; continue ;;
      *) printf 'opencode\tunreadable\n' >> "$SDIR/status.new"; continue ;;
    esac
    printf 'opencode\tok\n' >> "$SDIR/status.new"
    {
      printf 'O\t%s\t%s\n' "$db" "$sig"
      printf '%s\n' "$out" | LC_ALL=C awk -F '\t' -v cutoff="$SESS_CUTOFF" '
        function clean(s) { gsub(/[\001-\037\177"\\]/, "", s); return s }
        function base(p) { gsub(/\/+$/, "", p); sub(/.*\//, "", p); return substr(clean(p), 1, 80) }
        $1 == "D" { DIR[$2] = $3; next }
        $1 == "U" && $3 != "" && $4 >= cutoff {
          printf "R\topencode\t%s\t%s\tstandard\t%s\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\t%.0f\n", clean($2), clean($3), $4, $5, $6, $7, $8, $9, $10, $11
          HAS[$2] = 1
        }
        $1 == "S" { FI[$2] = $3; LA[$2] = $4 }
        END { for (s in FI) if (s in HAS) printf "M\topencode\t%s\t%s\t%s\t%s\n", clean(s), base(DIR[s]), FI[s], LA[s] }'
    } > "$dat.tmp" && mv -f "$dat.tmp" "$dat"
  done
}

# Reads whatever changed since the last run, in every place there are sessions.
sess_collect() {
  local tool dir size path
  sessions_init || return 1
  : > "$SDIR/status.new"
  : > "$SDIR/cand.tsv"
  while IFS=$'\t' read -r tool dir; do
    [ -n "$tool" ] || continue
    if [ "$tool" = opencode ]; then
      sess_opencode "$dir"
      continue
    fi
    printf '%s\tok\n' "$tool" >> "$SDIR/status.new"
    sess_list "$dir" | while read -r size _ path; do
      printf '%s\t%s\t%s\n' "$tool" "$size" "$path"
    done >> "$SDIR/cand.tsv"
  done <<EOF
$(sess_dirs)
EOF
  # The files whose size is not what it was when they were last read.
  { grep -h '^O' "$SDIR"/f/*.dat 2>/dev/null; printf '@@\n'; cat "$SDIR/cand.tsv"; } |
    awk -F '\t' '$0 == "@@" { c = 1; next } !c { SZ[$2] = $4; next } !($3 in SZ) || SZ[$3] != $2' > "$SDIR/todo.tsv"
  while IFS=$'\t' read -r tool size path; do
    sess_scan_file "$tool" "$size" "$path"
  done < "$SDIR/todo.tsv"
  mv -f "$SDIR/status.new" "$SDIR/status.tsv"
  # What a file said stays until it is older than the history that is kept.
  find "$SDIR/f" -name '*.dat' -mtime "+$SESSIONS_DAYS" -exec rm -f {} + 2>/dev/null
  return 0
}

# Makes the report from what is known: "print" shows all of it as one JSON document; "post" writes
# what the hub lacks as request bodies, $SDIR/chunk.N.json, and leaves the new totals in agg.new.
sess_report() {
  local mode="$1" all=0
  [ "$mode" = print ] && all=1
  rm -f "$SDIR"/chunk.*.json
  : > "$SDIR/agg.new"
  { awk '{ print "T\t" $0 }' "$SDIR/status.tsv" 2>/dev/null; cat "$SDIR"/f/*.dat 2>/dev/null; } |
    LC_ALL=C awk -F '\t' -v sent="$SDIR/sent.tsv" -v agg="$SDIR/agg.new" -v all="$all" "$REPORT1_AWK" |
    LC_ALL=C sort |
    LC_ALL=C awk -F '\t' -v mode="$mode" -v outdir="$SDIR" -v maxrows=400 "$REPORT2_AWK"
}

# Sends the chunks. The hub has the totals only once every chunk went through, so only then
# do they become what is known to be sent.
sess_post() {
  local c out status body
  for c in "$SDIR"/chunk.*.json; do
    [ -f "$c" ] || continue
    out=$(printf 'header = "Authorization: Bearer %s"\n' "$SECRET" |
      curl -sS -m 30 -K - -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -H 'X-Bield: agent' \
        --data-binary "@$c" "$SERVER_URL/api/agent/sessions") || {
      echo "$(date '+%Y-%m-%d %H:%M:%S') coding sessions: couldn't reach $SERVER_URL" >&2
      return 1
    }
    status=${out##*$'\n'}
    body=${out%$'\n'*}
    case "$status" in
      2??) ;;
      404)
        # An older hub. It won't learn the new route in a minute.
        echo $(($(date +%s) + 600)) > "$SDIR/backoff"
        echo "$(date '+%Y-%m-%d %H:%M:%S') coding sessions: this hub doesn't take them, so the agent asks again in 10 minutes" >&2
        return 1
        ;;
      *)
        echo "$(date '+%Y-%m-%d %H:%M:%S') coding sessions: HTTP $status: $(printf '%s' "$body" | cut -c 1-200)" >&2
        return 1
        ;;
    esac
  done
  mv -f "$SDIR/agg.new" "$SDIR/sent.tsv"
}

sess_lock() {
  local pid
  if ! mkdir "$SDIR/lock" 2>/dev/null; then
    pid=$(cat "$SDIR/lock/pid" 2>/dev/null)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then return 1; fi
    rm -rf "$SDIR/lock"
    mkdir "$SDIR/lock" 2>/dev/null || return 1
  fi
  printf '%s' "$$" > "$SDIR/lock/pid"
}

# One round: read what is new, and send what the hub does not have.
sessions_once() {
  local b
  sessions_init || return 1
  b=$(cat "$SDIR/backoff" 2>/dev/null)
  [ -n "$b" ] && [ "$b" -gt "$(date +%s)" ] 2>/dev/null && return 0
  sess_lock || return 0
  # shellcheck disable=SC2064
  trap "rm -rf '$SDIR/lock'" EXIT
  sess_collect || return 1
  sess_report post
  if ls "$SDIR"/chunk.*.json >/dev/null 2>&1; then sess_post; else mv -f "$SDIR/agg.new" "$SDIR/sent.tsv"; fi
}

CODING=0
NEXT_SCAN=0
SCAN_PID=""
# After a heartbeat that said coding is on, starts a round in the background, niced and not more
# often than SESSIONS_INTERVAL, so that a long first read never makes a heartbeat late.
start_sessions() {
  local now
  [ "$CODING" = 1 ] || return 0
  now=$(date +%s)
  [ "$now" -ge "$NEXT_SCAN" ] || return 0
  if [ -n "$SCAN_PID" ] && kill -0 "$SCAN_PID" 2>/dev/null; then return 0; fi
  NEXT_SCAN=$((now + SESSIONS_INTERVAL))
  if command -v nice >/dev/null 2>&1; then
    nice -n 19 "${BASH:-/bin/bash}" "$0" --sessions-once &
  else
    "${BASH:-/bin/bash}" "$0" --sessions-once &
  fi
  SCAN_PID=$!
}

if [ "${1:-}" = "--print" ]; then
  build_payload
  printf '%s\n' "$payload"
  exit 0
fi

if [ "${1:-}" = "--once" ]; then
  send_heartbeat && echo "heartbeat sent to $SERVER_URL"
  exit $?
fi

# Reads the sessions on this machine and shows the report instead of sending it. All of it, as
# the files say now; BIELD_AGENT_STATE says where to keep what it has read.
if [ "${1:-}" = "--sessions-print" ]; then
  sess_collect && sess_report print
  exit $?
fi

# One round of reading and sending, as the loop below starts it in the background.
if [ "${1:-}" = "--sessions-once" ]; then
  sessions_once
  exit $?
fi

# shellcheck disable=SC2064
trap '[ -n "$SCAN_PID" ] && kill "$SCAN_PID" 2>/dev/null' EXIT
while true; do
  delay="$INTERVAL"
  # A hub that refuses this agent, such as after an admin revoked it, won't
  # change its mind in 15 seconds, so ask every 5 minutes instead.
  send_heartbeat || case "$STATUS" in 401 | 403 | 409) delay=300 ;; esac
  start_sessions
  sleep "$delay"
done
