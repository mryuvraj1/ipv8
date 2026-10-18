#!/bin/sh
set -eu

fail() { echo "IPv8 Cloudflare setup: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail 'run through sudo while preserving only CLOUDFLARE_TUNNEL_TOKEN.'
[ -n "${CLOUDFLARE_TUNNEL_TOKEN:-}" ] || fail 'CLOUDFLARE_TUNNEL_TOKEN is not set.'
command -v systemctl >/dev/null 2>&1 || fail 'systemd is required.'
command -v install >/dev/null 2>&1 || fail 'install utility is required.'
CLOUDFLARED=$(command -v cloudflared 2>/dev/null || true)
[ -n "$CLOUDFLARED" ] && [ -x "$CLOUDFLARED" ] || fail 'install cloudflared first.'
case "$CLOUDFLARED" in *[!A-Za-z0-9_./+-]*) fail 'cloudflared executable path contains unsupported characters.' ;; esac
cloudflared tunnel run --help 2>&1 | grep -q -- '--token-file' || fail 'cloudflared 2025.4.0 or later with --token-file support is required.'

DIR=/etc/ipv8
TOKEN_FILE=$DIR/cloudflared-token
UNIT=/etc/systemd/system/ipv8-cloudflared.service
[ ! -L "$DIR" ] || fail "$DIR is a symlink; refusing to follow it."
[ ! -e "$DIR" ] || [ -d "$DIR" ] || fail "$DIR exists and is not a directory."
[ ! -e "$TOKEN_FILE" ] && [ ! -L "$TOKEN_FILE" ] || fail "$TOKEN_FILE already exists; refusing to replace it. Rotate or remove it manually first."
[ ! -e "$UNIT" ] && [ ! -L "$UNIT" ] || fail "$UNIT already exists; refusing to replace it."
[ -r /dev/tty ] || fail 'an interactive terminal is required to confirm creating the service.'
printf 'This creates %s (mode 0600) and enables %s. The tunnel publishes only the route configured in Cloudflare.\n' "$TOKEN_FILE" "$UNIT" >/dev/tty
printf 'Continue? [y/N] ' >/dev/tty
IFS= read -r answer </dev/tty || fail 'could not read terminal input.'
case "$answer" in y|Y|yes|YES) ;; *) fail 'Cloudflare Tunnel setup cancelled; no files changed.' ;; esac

install -d -m 0700 "$DIR"
tmp_token=$(mktemp "$DIR/.cloudflared-token.XXXXXX")
tmp_unit=$(mktemp /etc/systemd/system/.ipv8-cloudflared.XXXXXX)
cleanup() { rm -f "$tmp_token" "$tmp_unit"; }
trap cleanup EXIT HUP INT TERM
chmod 0600 "$tmp_token"
printf '%s' "$CLOUDFLARE_TUNNEL_TOKEN" > "$tmp_token"
cat > "$tmp_unit" <<UNIT
[Unit]
Description=IPv8 dashboard Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
DynamicUser=yes
LoadCredential=tunnel-token:/etc/ipv8/cloudflared-token
ExecStart=$CLOUDFLARED tunnel run --token-file=%d/tunnel-token
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
UNIT
chmod 0644 "$tmp_unit"
ln "$tmp_token" "$TOKEN_FILE" || fail 'could not create the tunnel token file without overwriting another file.'
ln "$tmp_unit" "$UNIT" || { rm -f "$TOKEN_FILE"; fail 'could not install the tunnel service without overwriting another file.'; }
rm -f "$tmp_token" "$tmp_unit"
trap - EXIT HUP INT TERM
unset CLOUDFLARE_TUNNEL_TOKEN
systemctl daemon-reload
systemctl enable --now ipv8-cloudflared.service
printf 'Cloudflare Tunnel service enabled. Token stored at %s with mode 0600.\n' "$TOKEN_FILE"
printf 'Verify tunnel health with: sudo systemctl status ipv8-cloudflared.service\n'
