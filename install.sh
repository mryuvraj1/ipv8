#!/bin/sh
set -eu

REPO=https://github.com/mryuvraj1/ipv8
SERVER=
TOKEN=
NAME=$(hostname 2>/dev/null || printf 'IPv8 installation')
DRY_RUN=0

usage() {
  cat <<'HELP'
Usage: install.sh --server HTTPS_ORIGIN --token ONE_TIME_TOKEN [--name NAME] [--interactive] [--dry-run]

Downloads a checksum-verified IPv8 agent release and installs it on Debian,
Ubuntu, Fedora, or Arch Linux with systemd 247+ on amd64 or arm64. The installer
asks for terms acceptance and confirmation before privileged system changes.
The enrollment token expires after 15 minutes and can be used once.
HELP
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) [ "$#" -ge 2 ] || { echo 'Missing --server value' >&2; exit 2; }; SERVER=$2; shift 2 ;;
    --token) [ "$#" -ge 2 ] || { echo 'Missing --token value' >&2; exit 2; }; TOKEN=$2; shift 2 ;;
    --name) [ "$#" -ge 2 ] || { echo 'Missing --name value' >&2; exit 2; }; NAME=$2; shift 2 ;;
    --interactive) shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "IPv8 installer: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail 'run as root, for example with sudo.'
[ "$(uname -s)" = Linux ] || fail 'only Linux is supported; macOS, BSD, and Android are unsupported.'
[ -r /etc/os-release ] || fail 'cannot identify Linux distribution.'
. /etc/os-release
case "${ID:-}" in debian|ubuntu|fedora|arch) ;; *) fail "unsupported distribution '${ID:-unknown}'; supported: Debian, Ubuntu, Fedora, Arch Linux." ;; esac
[ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1 || fail 'systemd is required and must be running.'
for utility in curl sha256sum grep sed install; do command -v "$utility" >/dev/null 2>&1 || fail "$utility is required; install it with your distribution package manager."; done
SYSTEMD_VERSION=$(systemctl --version | sed -n '1s/^systemd \([0-9][0-9]*\).*/\1/p')
[ -n "$SYSTEMD_VERSION" ] || fail 'could not determine the systemd version.'
[ "$SYSTEMD_VERSION" -ge 247 ] || fail "systemd 247 or later is required; found $SYSTEMD_VERSION."
case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) fail "unsupported architecture '$(uname -m)'; supported: amd64 and arm64." ;; esac
[ -n "$SERVER" ] && [ -n "$TOKEN" ] || fail '--server and --token are required.'
case "$SERVER" in https://?*) ;; http://localhost|http://localhost:*|http://127.0.0.1|http://127.0.0.1:*|http://\[::1\]|http://\[::1\]:*) ;; *) fail '--server must be an HTTPS origin; HTTP is allowed only for localhost prototypes.' ;; esac
case "$SERVER" in */|https://*/*|http://*/*|*\?*|*\#*|*\@*|*' '*) fail '--server must be an origin without path, credentials, query, or fragment.' ;; esac
[ "${#NAME}" -ge 1 ] && [ "${#NAME}" -le 60 ] || fail 'node name must contain 1 to 60 characters.'
case "$NAME" in *[![:print:]]*) fail 'node name contains unsupported control characters.' ;; esac

CONFIG=/etc/ipv8/agent.json
BIN=/usr/local/bin/ipv8
UNIT=/etc/systemd/system/ipv8-agent.service
HAD_CONFIG=0
if [ -L /etc/ipv8 ] || { [ -e /etc/ipv8 ] && [ ! -d /etc/ipv8 ]; }; then fail '/etc/ipv8 must be a real directory; refusing to follow or replace it.'; fi
if [ -e "$CONFIG" ] || [ -L "$CONFIG" ]; then HAD_CONFIG=1; fi
if [ -L "$BIN" ]; then
  fail "$BIN is a symlink; refusing to replace it."
elif [ -e "$BIN" ]; then
  [ -f "$BIN" ] || fail "$BIN is not a regular file; refusing to replace it."
  "$BIN" version 2>/dev/null | grep -Fq 'IPv8 experimental agent' || fail "$BIN is not a recognized IPv8 binary; refusing to replace it."
  [ "$HAD_CONFIG" -eq 1 ] || fail "$BIN exists without an agent config; refusing to replace it."
fi
if [ -L "$UNIT" ]; then
  fail "$UNIT is a symlink; refusing to replace it."
elif [ -e "$UNIT" ] && ! grep -Fxq '# Managed by the IPv8 installer.' "$UNIT"; then
  fail "$UNIT already exists and is not an IPv8 agent unit; it was not changed."
fi
if [ "$HAD_CONFIG" -eq 1 ]; then
  [ -x "$BIN" ] || fail 'existing agent config found without a recognized IPv8 binary; resolve it manually before reinstalling.'
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "Dry run: would download the verified linux/$ARCH release from $REPO, enroll '$NAME' at $SERVER, install $BIN and $UNIT, and start ipv8-agent.service."
  echo 'No files, services, or enrollment tokens were changed or consumed.'
  exit 0
fi
[ -r /dev/tty ] || fail 'an interactive terminal is required to accept the terms and confirm system changes.'
ask() { printf '%s' "$1" >/dev/tty; IFS= read -r REPLY </dev/tty || fail 'could not read terminal input.'; }
printf '\nIPv8 is experimental networking software. This agent sends authenticated status heartbeats only; it does not route packets, VPN traffic, or modify firewall/network settings.\n' >/dev/tty
ask 'Accept these experimental software terms? [y/N] '
case "$REPLY" in y|Y|yes|YES) ;; *) fail 'terms were not accepted; no changes made.' ;; esac
ask "Node name [$NAME]: "
[ -n "$REPLY" ] && NAME=$REPLY
[ "${#NAME}" -ge 1 ] && [ "${#NAME}" -le 60 ] || fail 'node name must contain 1 to 60 characters.'
case "$NAME" in *[![:print:]]*) fail 'node name contains unsupported control characters.' ;; esac
printf '\nReview installation\n  Panel: %s\n  Node: %s\n  System: %s %s / linux/%s\n  Service: ipv8-agent.service\n  Changes: install agent and service; no route or firewall changes\n\n' "$SERVER" "${NAME}" "${PRETTY_NAME:-$ID}" "${VERSION_ID:-}" "$ARCH" >/dev/tty
ask 'Proceed with installation and privileged system changes? [y/N] '
case "$REPLY" in y|Y|yes|YES) ;; *) fail 'installation cancelled; no changes made.' ;; esac

echo "Downloading checksum-verified IPv8 release for linux/$ARCH ..."
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM
ASSET="ipv8-linux-$ARCH"
SERVICE=ipv8-agent.service
RELEASE="$REPO/releases/latest/download"
if ! curl --fail --location --silent --show-error "$RELEASE/$ASSET" --output "$TMP/$ASSET"; then fail 'could not download a published IPv8 release; check GitHub Releases and retry.'; fi
if ! curl --fail --location --silent --show-error "$RELEASE/SHA256SUMS" --output "$TMP/SHA256SUMS"; then fail 'could not download the release checksum manifest.'; fi
if ! curl --fail --location --silent --show-error "$RELEASE/$SERVICE" --output "$TMP/$SERVICE"; then fail 'could not download the systemd service unit.'; fi
echo 'Downloads complete; verifying release checksums ...'
CHECKSUM=$(grep -E "^[[:xdigit:]]{64}  $ASSET$" "$TMP/SHA256SUMS" | sed -n '1s/  .*//p')
[ "${#CHECKSUM}" -eq 64 ] || fail 'release checksum manifest does not contain this architecture.'
SERVICE_CHECKSUM=$(grep -E "^[[:xdigit:]]{64}  $SERVICE$" "$TMP/SHA256SUMS" | sed -n '1s/  .*//p')
[ "${#SERVICE_CHECKSUM}" -eq 64 ] || fail 'release checksum manifest does not contain the service unit.'
(cd "$TMP" && printf '%s  %s\n%s  %s\n' "$CHECKSUM" "$ASSET" "$SERVICE_CHECKSUM" "$SERVICE" | sha256sum -c - >/dev/null) || fail 'release checksum verification failed.'
chmod 0755 "$TMP/$ASSET"
"$TMP/$ASSET" version 2>/dev/null | grep -Fq 'IPv8 experimental agent' || fail 'downloaded release is not a recognized IPv8 agent.'
echo 'Release checksums and agent version verified.'

if [ "$HAD_CONFIG" -eq 1 ]; then
  "$TMP/$ASSET" check-config --config "$CONFIG" || fail "$CONFIG is invalid or has unsafe permissions; it was not changed."
else
  echo 'Enrolling this node with the panel ...'
  install -d -m 0700 /etc/ipv8
  if ! "$TMP/$ASSET" enroll --server "$SERVER" --token "$TOKEN" --name "$NAME" --config "$CONFIG"; then
    fail 'enrollment failed; create a fresh one-time token if this token was consumed.'
  fi
fi
echo 'Installing agent and persistent systemd service ...'
install -m 0755 "$TMP/$ASSET" "$TMP/ipv8.new"
mv -f "$TMP/ipv8.new" "$BIN"
install -d -m 0700 /etc/ipv8
[ "$HAD_CONFIG" -eq 0 ] || echo 'Keeping existing enrollment configuration.'
install -m 0644 "$TMP/$SERVICE" "$UNIT"
systemctl daemon-reload
systemctl enable ipv8-agent.service
if systemctl is-active --quiet ipv8-agent.service; then systemctl restart ipv8-agent.service; else systemctl start ipv8-agent.service; fi
echo 'Agent files and systemd service installed. The dashboard shows Online only after the first authenticated heartbeat.'
if systemctl is-active --quiet ipv8-agent.service; then echo 'Systemd reports the service active; check My IPv8 for heartbeat-confirmed status.'; else fail 'service did not start; inspect journalctl -u ipv8-agent.'; fi
cat <<'UNINSTALL'
Uninstall after revoking the node in My IPv8:
  sudo systemctl disable --now ipv8-agent
  sudo rm -f /etc/systemd/system/ipv8-agent.service /usr/local/bin/ipv8 /etc/ipv8/agent.json
  sudo rmdir /etc/ipv8 # removes only when empty
  sudo systemctl daemon-reload
UNINSTALL
