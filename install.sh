#!/bin/sh
set -eu

REPO=https://github.com/mryuvraj1/ipv8
RAW=https://raw.githubusercontent.com/mryuvraj1/ipv8/main
SERVER=
TOKEN=
NAME=$(hostname 2>/dev/null || printf 'IPv8 installation')

usage() {
  cat <<'EOF'
Usage: install.sh --server URL --token TOKEN [--name NAME]

Installs the IPv8 heartbeat agent on Debian, Ubuntu, Fedora, or Arch Linux
(systemd, amd64 or arm64). The token is single use and expires after 15 minutes.
An existing /etc/ipv8/agent.json is validated and preserved on reruns.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) [ "$#" -ge 2 ] || { echo 'Missing --server value' >&2; exit 2; }; SERVER=$2; shift 2 ;;
    --token) [ "$#" -ge 2 ] || { echo 'Missing --token value' >&2; exit 2; }; TOKEN=$2; shift 2 ;;
    --name) [ "$#" -ge 2 ] || { echo 'Missing --name value' >&2; exit 2; }; NAME=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

fail() { echo "IPv8 installer: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail 'run as root, for example with sudo.'
[ -r /etc/os-release ] || fail 'cannot identify Linux distribution.'
. /etc/os-release
case "${ID:-}" in debian|ubuntu|fedora|arch) ;; *) fail "unsupported distribution '${ID:-unknown}'; supported: Debian, Ubuntu, Fedora, Arch Linux." ;; esac
[ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1 || fail 'systemd is required and must be running.'
command -v curl >/dev/null 2>&1 || fail 'curl is required; install it with your distribution package manager.'
command -v tar >/dev/null 2>&1 || fail 'tar is required; install it with your distribution package manager.'

case "$(uname -m)" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) fail "unsupported architecture '$(uname -m)'; supported: amd64 and arm64." ;; esac
CONFIG=/etc/ipv8/agent.json
BIN=/usr/local/bin/ipv8
UNIT=/etc/systemd/system/ipv8-agent.service

if [ -e "$BIN" ] && [ ! -e "$CONFIG" ]; then
  fail "$BIN already exists without an IPv8 agent config; refusing to overwrite an unidentified binary."
fi

if [ -e "$CONFIG" ]; then
  [ -x "$BIN" ] || fail "$CONFIG exists but $BIN is missing; preserve the config and repair the binary manually."
  "$BIN" check-config --config "$CONFIG" || fail "$CONFIG is invalid or has unsafe permissions; it was not changed."
else
  [ -n "$SERVER" ] && [ -n "$TOKEN" ] || fail '--server and --token are required for first enrollment.'
fi

if [ -e "$UNIT" ] && ! grep -Fxq '# Managed by the IPv8 installer.' "$UNIT"; then
  fail "$UNIT already exists and is not an IPv8 agent unit; it was not changed."
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
ARCHIVE="$TMP/ipv8.tar.gz"
URL="$REPO/archive/refs/heads/main.tar.gz"
echo "Building IPv8 from $URL for linux/$ARCH ..."
if ! curl --fail --location --silent --show-error "$URL" --output "$ARCHIVE"; then
  fail 'could not download project source from GitHub.'
fi
mkdir "$TMP/src"
if ! tar -xzf "$ARCHIVE" -C "$TMP/src" --strip-components=1; then
  fail 'downloaded source archive is invalid.'
fi
command -v go >/dev/null 2>&1 || fail 'Go 1.24 or later is required to build from source.'
GOVERSION=$(go env GOVERSION 2>/dev/null || true)
GONUM=$(printf '%s' "$GOVERSION" | sed 's/^go//; s/[^0-9.].*$//')
[ -n "$GONUM" ] || fail 'could not determine the installed Go version.'
if [ "$(printf '%s\n%s\n' 1.24 "$GONUM" | sort -V | head -n 1)" != 1.24 ]; then
  fail "Go 1.24 or later is required; found $GOVERSION."
fi
if ! (cd "$TMP/src" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags='-s -w' -o "$TMP/ipv8" ./cmd/ipv8); then
  fail 'Go build failed; the existing installation was left running.'
fi
chmod 0755 "$TMP/ipv8"
install -m 0755 "$TMP/ipv8" "$TMP/ipv8.new"
mv -f "$TMP/ipv8.new" "$BIN"
install -d -m 0755 /etc/ipv8

if [ ! -e "$CONFIG" ]; then
  if ! "$BIN" enroll --server "$SERVER" --token "$TOKEN" --name "$NAME" --config "$CONFIG"; then
    fail 'enrollment failed; no agent service was enabled.'
  fi
else
  echo 'Keeping existing enrollment configuration.'
fi

cat > "$TMP/ipv8-agent.service" <<'EOF'
# Managed by the IPv8 installer.
[Unit]
Description=IPv8 experimental heartbeat agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ipv8 agent --config /etc/ipv8/agent.json
Restart=on-failure
RestartSec=5
User=root
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/ipv8

[Install]
WantedBy=multi-user.target
EOF
install -m 0644 "$TMP/ipv8-agent.service" "$UNIT"
systemctl daemon-reload
systemctl enable ipv8-agent.service
if systemctl is-active --quiet ipv8-agent.service; then
  systemctl restart ipv8-agent.service
else
  systemctl start ipv8-agent.service
fi
echo 'IPv8 agent installed. It sends authenticated status heartbeats only; it does not modify routes or firewall rules.'
cat <<'EOF'
Uninstall (the rmdir step removes the directory only if it is empty):
  sudo systemctl disable --now ipv8-agent
  sudo rm /etc/systemd/system/ipv8-agent.service /usr/local/bin/ipv8 /etc/ipv8/agent.json
  sudo rmdir /etc/ipv8
  sudo systemctl daemon-reload
EOF
