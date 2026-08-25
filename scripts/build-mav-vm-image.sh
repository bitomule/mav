#!/bin/sh
# Builds a tart image with mav and its macOS drivers already installed.
#
# What it does NOT do, on purpose: grant TCC permissions. Accessibility and
# Screen Recording go to the tools and could be baked in, but the ones the app
# under test asks for (microphone, calendar, Apple Events...) depend on each
# app, so an image "with permissions" would be one image per app. This one
# ships tools; permissions are granted later, against the specific app.
#
# Transparent about your tart setup: it does not touch TART_HOME or assume
# where you keep your images. If you keep them on an external disk, yours is used.
set -eu

BASE="${MAV_VM_BASE:-ghcr.io/cirruslabs/macos-tahoe-base:latest}"
NAME="${MAV_VM_NAME:-mav-macos}"
USER_NAME="${MAV_VM_USER:-admin}"
PASSWORD="${MAV_VM_PASSWORD:-admin}"

command -v tart >/dev/null || { echo "tart is not installed" >&2; exit 1; }

# tart >= 2.29: in 2.28.1 `tart exec` blows up without a TTY (unguarded try!
# over the terminal size), and crabbox uses it to inject the SSH key. With the
# old version, the whole provider fails to start from a script.
VERSION=$(tart --version 2>/dev/null | head -1)
case "$VERSION" in
  2.2[0-8].*|2.1*|1.*) echo "tart $VERSION is too old; >= 2.29 is required" >&2; exit 1 ;;
esac

echo "==> cloning $BASE -> $NAME"
tart clone "$BASE" "$NAME"

echo "==> booting (headless)"
tart run "$NAME" --no-graphics &
RUN_PID=$!
trap 'kill "$RUN_PID" 2>/dev/null || true' EXIT

echo "==> waiting for IP"
IP=""
for _ in $(seq 1 60); do
  IP=$(tart ip "$NAME" 2>/dev/null || true)
  [ -n "$IP" ] && break
  sleep 5
done
[ -n "$IP" ] || { echo "the VM never got an IP" >&2; exit 1; }
echo "    $IP"

echo "==> waiting for SSH"
for _ in $(seq 1 60); do
  SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 \
    "$USER_NAME@$IP" true 2>/dev/null && break
  sleep 5
done

echo "==> installing mav and drivers"
# Provisioning goes in a file, not in a heredoc inside an `if`: mixing the
# two broke the heredoc in an earlier version and the last lines -- exactly
# the ones that fix the PATH and verify -- ended up printed as text instead
# of executing. The image shipped without PATH and the script never
# noticed.
PROVISION_SCRIPT=$(mktemp)
trap 'rm -f "$PROVISION_SCRIPT"; kill "$RUN_PID" 2>/dev/null || true' EXIT
cat > "$PROVISION_SCRIPT" <<'PROVISION'
set -eu
export NONINTERACTIVE=1

if ! command -v brew >/dev/null 2>&1; then
  echo "    installing homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
eval "$(/opt/homebrew/bin/brew shellenv)"

brew install bitomule/tap/mav bitomule/tap/axcli mitmproxy
curl -fsSL https://cua.ai/driver/install.sh | bash

# In a non-interactive SSH session the PATH is /usr/bin:/bin:/usr/sbin:/sbin,
# without /opt/homebrew/bin. Without this, everything just installed is
# invisible when driving the VM from outside -- including from crabbox.
LINE='export PATH=/opt/homebrew/bin:/usr/local/bin:$PATH'
grep -qs "/opt/homebrew/bin" "$HOME/.zshenv" 2>/dev/null || echo "$LINE" >> "$HOME/.zshenv"
grep -qs "/opt/homebrew/bin" "$HOME/.bashrc" 2>/dev/null || echo "$LINE" >> "$HOME/.bashrc"

# Verify, do not trust: `brew install` with several formulas fails as a whole
# if one does not exist, and without this check the script reported success
# with a half-built image. An incomplete image announced as ready is worse
# than a failure: the error resurfaces on the first run, unrelated to the cause.
missing=""
for t in mav cua-driver axcli mitmdump; do
  if command -v "$t" >/dev/null 2>&1; then
    printf "      %-10s %s\n" "$t" "$(command -v "$t")"
  else
    printf "      %-10s NOT INSTALLED\n" "$t"
    missing="$missing $t"
  fi
done
[ -z "$missing" ] || { echo "missing tools:$missing" >&2; exit 1; }
PROVISION

if ! SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" \
  'bash -s' < "$PROVISION_SCRIPT"
then
  echo "provisioning failed; the VM $NAME is left running for inspection" >&2
  trap 'rm -f "$PROVISION_SCRIPT"' EXIT
  exit 1
fi

echo "==> shutting down"
SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" \
  'sudo shutdown -h now' 2>/dev/null || true
for _ in $(seq 1 30); do tart ip "$NAME" >/dev/null 2>&1 || break; sleep 2; done

echo
echo "Image ready: $NAME"
echo "  use with crabbox:  CRABBOX_TART_IMAGE=$NAME crabbox job run <job>"
echo "  publish:           tart push $NAME <registry>/<org>/mav-macos:latest"
echo
echo "TCC permissions are NOT granted: they depend on the app under test."
