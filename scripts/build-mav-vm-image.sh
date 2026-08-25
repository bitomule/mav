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

# Build cua-driver from our fork and put it inside the app bundle.
#
# The released driver enumerates layer-0 windows only, so an app whose whole UI
# is an accessory window -- a floating panel, a HUD, a SwiftUI onboarding -- is
# invisible to it and mav cannot reach it. The fix is open upstream as
# trycua/cua#3375. Building it here instead of shipping a binary keeps the image
# reproducible from source, and the day the PR lands this whole block goes away
# and the installer above is enough.
#
# The binary has to live inside CuaDriver.app: the app is what holds the TCC
# grants, and a binary sitting anywhere else has none.
echo "    building patched cua-driver (trycua/cua#3375)"
brew install rust >/dev/null 2>&1 || true
rm -rf "$HOME/.cua-src"
git clone -q --depth 1 --filter=blob:none --sparse \
  -b feat/list-windows-include-all-layers https://github.com/bitomule/cua.git "$HOME/.cua-src"
git -C "$HOME/.cua-src" sparse-checkout set libs/cua-driver/rust >/dev/null
(cd "$HOME/.cua-src/libs/cua-driver/rust" && cargo build --release -p cua-driver >/dev/null 2>&1)
PATCHED="$HOME/.cua-src/libs/cua-driver/rust/target/release/cua-driver"
if [ ! -x "$PATCHED" ]; then
  echo "    ERROR: patched cua-driver did not build" >&2
  exit 1
fi
# Replace by new inode, never in place: overwriting a Mach-O that macOS has
# already seen invalidates its cached signature and the next run dies with
# SIGKILL and no message.
APP_BIN=/Applications/CuaDriver.app/Contents/MacOS/cua-driver
sudo rm -f "$APP_BIN"
sudo cp "$PATCHED" "$APP_BIN"
sudo codesign -f -s - --deep /Applications/CuaDriver.app >/dev/null 2>&1
rm -rf "$HOME/.cua-src"

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
# Register the driver in the privacy panes and leave the daemon running.
#
# This is as far as scripting goes, and it is worth knowing why: Accessibility
# and Screen Recording cannot be granted from a script on macOS 26. Seeding
# TCC.db does not work (tried four ways, including a valid csreq and a reboot),
# PPPC profiles are only honored when they arrive from an MDM, and AppleScript
# cannot reach System Settings over SSH -- it times out, and `launchctl asuser`
# turns that into "Connection is invalid" instead.
#
# What `permissions grant` does do is register CuaDriver in both panes, so the
# switches exist and only have to be flipped. Flip them ONCE here, while the
# image is being built, and every VM cloned from it starts with them on: the
# grants live on the disk that becomes the image. No agent ever clicks anything
# at run time, which is the point.
echo "    registering CuaDriver in the privacy panes"
open -n -g -a CuaDriver --args serve >/dev/null 2>&1 || true
sleep 6
cua-driver permissions grant >/dev/null 2>&1 &
sleep 20

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

# Refuse to call the image ready while blind. An image announced as done with
# the driver unable to see anything fails on the first run, far from the cause.
echo "==> checking the driver's permissions"
GRANTS=$(SSHPASS="$PASSWORD" sshpass -e ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$USER_NAME@$IP" \
  'export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH; cua-driver permissions status --json 2>/dev/null' || true)
case "$GRANTS" in
  *'"accessibility": true'*) ACCESSIBILITY=yes ;;
  *) ACCESSIBILITY=no ;;
esac
case "$GRANTS" in
  *'"screen_recording": true'*) SCREEN=yes ;;
  *) SCREEN=no ;;
esac
printf "      accessibility    %s\n      screen recording %s\n" "$ACCESSIBILITY" "$SCREEN"
if [ "$ACCESSIBILITY" != yes ] || [ "$SCREEN" != yes ]; then
  cat >&2 <<GRANTHELP

The image is NOT ready. macOS 26 has no scriptable way to grant these: seeding
TCC.db does not work, PPPC profiles are only honored from an MDM, and
AppleScript cannot reach System Settings over SSH. They have to be switched on
once, by hand, here -- after that they live on the disk that becomes the image
and every VM cloned from it starts with them on.

  1. tart run $NAME --vnc-experimental      (prints a vnc:// URL with a password)
  2. In the VM: System Settings > Privacy & Security
       Accessibility           -> CuaDriver on
       Screen & System Audio   -> CuaDriver on
     Both entries are already listed; `permissions grant` registered them.
  3. Re-run this script. It resumes on the existing VM and will verify again.

GRANTHELP
  echo "the VM $NAME is left running so the switches can be flipped" >&2
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
echo "The driver has its permissions. The app under test still needs its own."
