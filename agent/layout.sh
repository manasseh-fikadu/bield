# shellcheck shell=bash disable=SC2034
# SPDX-License-Identifier: MIT
# Where the agent lives on a machine and what runs it. The hub puts this at the
# top of install.sh and uninstall.sh, so they agree.
NAME="bield-agent"
LABEL="dev.bield.agent"
CONF_DIR="$HOME/.config/$NAME"
# What the agent remembers about the coding sessions it has read, so it never reads a file twice.
STATE_DIR="$HOME/.local/state/$NAME"

case "$(uname -s)" in
  Darwin)
    BIN_DIR="$HOME/Library/Application Support/$NAME"
    PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
    LOG="$HOME/Library/Logs/$NAME.log"
    ;;
  Linux)
    BIN_DIR="$HOME/.local/share/$NAME"
    USER_UNIT="$HOME/.config/systemd/user/$NAME.service"
    # su, Dropbear and commands run over SSH can leave this unset even though
    # the user's systemd is running, and systemctl --user needs it.
    if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -d "/run/user/$(id -u)" ]; then
      XDG_RUNTIME_DIR="/run/user/$(id -u)"
      export XDG_RUNTIME_DIR
    fi
    if [ "$(id -u)" = 0 ]; then
      # Root often has no user session to run in, as on DietPi, which logs in
      # with Dropbear. A system service needs none, and runs from boot.
      SCOPE="--system"
      UNIT="/etc/systemd/system/$NAME.service"
      WANTED_BY="multi-user.target"
      LOGS="journalctl -u $NAME"
    else
      SCOPE="--user"
      UNIT="$USER_UNIT"
      WANTED_BY="default.target"
      LOGS="journalctl --user -u $NAME"
    fi
    ;;
  *)
    echo "Unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

# remove_service stops the agent's service in a scope (--user or --system) and
# deletes its unit. It's fine if there's no such service.
remove_service() {
  systemctl "$1" disable --now "$NAME.service" 2>/dev/null || true
  rm -f "$2"
  systemctl "$1" daemon-reload 2>/dev/null || true
}
