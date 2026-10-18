# IPv8

IPv8 is an open-source Go prototype for experimental 256-bit addresses, a local packet envelope/simulator, an account panel, DNS TXT metadata verification, and an optional heartbeat agent. IPv8 addresses are local identifiers: this project has no peer packet transport, public routing, VPN, or IPv4/IPv6 replacement. It does not measure RTT, bandwidth, geolocation, or traffic.

## Requirements and supported platforms

- Go 1.24+ to run/build the panel and to build the agent from source.
- The installer runs in Bash and requires `curl`, `tar`, `sha256sum`, `grep`, `sed`, `install`, `df`, `awk`, `mktemp`, `stty`, `cp`, `cmp`, `chmod`, `mv`, and `stat`. It does not install packages.
- It checks for at least 20 MiB free for installation/release downloads and 100 MiB in its temporary area when building from source.
- Panel and agent service management: Debian, Ubuntu, Fedora, or Arch Linux with systemd 247+ on Linux amd64 or arm64. Root access is requested only after the full review and a separate consent prompt.
- Termux: Android arm64 is supported as a user-local Go source build. Go 1.24+ must already be installed. Termux has no service manager configured by this installer, so the agent runs in a foreground session.
- The tagged-release workflow publishes Linux amd64/arm64 assets and `SHA256SUMS`; no published release is currently available. Until one is published, Linux and Termux offer the disclosed Go source-build path. Source builds require outbound HTTPS and Go module access. A source snapshot is not signed or checksummed.

## Run the panel locally

```sh
git clone https://github.com/mryuvraj1/ipv8.git
cd ipv8
go run ./cmd/ipv8 serve
```

Open <http://127.0.0.1:8080> and register a user. For the first administrator, set credentials just for the bootstrap command:

```sh
read -r -p 'Admin email: ' IPV8_ADMIN_EMAIL
read -r -s -p 'Admin password (14+ characters): ' IPV8_ADMIN_PASSWORD; printf '\n'
export IPV8_ADMIN_EMAIL IPV8_ADMIN_PASSWORD
go run ./cmd/ipv8 bootstrap-admin
unset IPV8_ADMIN_EMAIL IPV8_ADMIN_PASSWORD
```

The app creates `./data/ipv8.json` on its first write. Keep it private and stop the single-process server before copying it. See `.env.example` for placeholder configuration names; the app does not load `.env` automatically.

For a panel installed by the MOTD manager, create the first admin using the same systemd-managed data directory. Stop the service to avoid concurrent database writes, then enter the password twice without echo; it is sent through stdin, not command arguments or a file:

```bash
read -r -p 'Admin email: ' IPV8_ADMIN_EMAIL
read -r -s -p 'Admin password (14+ characters): ' IPV8_ADMIN_PASSWORD; printf '\n'
read -r -s -p 'Confirm password: ' IPV8_ADMIN_CONFIRM; printf '\n'
if [[ $IPV8_ADMIN_PASSWORD != "$IPV8_ADMIN_CONFIRM" ]]; then echo 'Passwords do not match'; unset IPV8_ADMIN_PASSWORD IPV8_ADMIN_CONFIRM; return 1 2>/dev/null || exit 1; fi
sudo systemctl stop ipv8-panel.service
printf '%s' "$IPV8_ADMIN_PASSWORD" | sudo systemd-run --pipe --wait --collect \
  --property=DynamicUser=yes --property=StateDirectory=ipv8 \
  --property=EnvironmentFile=/etc/ipv8/panel.env \
  --setenv="IPV8_ADMIN_EMAIL=$IPV8_ADMIN_EMAIL" \
  /usr/local/bin/ipv8 bootstrap-admin --password-stdin
sudo systemctl start ipv8-panel.service
unset IPV8_ADMIN_EMAIL IPV8_ADMIN_PASSWORD IPV8_ADMIN_CONFIRM
```

Use the selected installation prefix in place of `/usr/local/bin/ipv8` if changed. Start the panel even if bootstrap reports that an admin already exists. For local development the shorter environment-based bootstrap command above uses the local `IPV8_DATA` path.

The app creates its configured JSON database on first write. Keep it private and stop the single-process server before copying it. See `.env.example` for placeholder configuration names; the app does not load `.env` automatically.

## Configuration and deployment

Configure the app in its process/service environment:

| Variable | Default | Purpose |
| --- | --- | --- |
| `IPV8_ADDR` | `127.0.0.1:8080` | Panel/API/health listen address |
| `IPV8_DATA` | `./data/ipv8.json` | Persistent JSON database path |
| `IPV8_PUBLIC_URL` | unset | Real public HTTPS origin; enables remote install command |
| `IPV8_API_URL` | unset | Same-origin API URL; must match public URL when set |
| `IPV8_COOKIE_SECURE` | local false | Set `true` for public HTTPS; HTTPS automatically enables Secure cookies |
| `IPV8_ADMIN_EMAIL` | unset | Used only by `bootstrap-admin` |
| `IPV8_ADMIN_PASSWORD` | unset | Used only by `bootstrap-admin`; 14+ characters |
| `CLOUDFLARE_TUNNEL_TOKEN` | unset | Used only by the separate tunnel helper; never put in the app or browser environment |

For Cloudflare Tunnel deployment, route the public hostname to the existing panel at `http://127.0.0.1:8080`, set the actual public and API HTTPS origins, and keep the origin port private. Follow [DEPLOYMENT.md](DEPLOYMENT.md). This repository has no configured public domain.

## One-line interactive installer

Run this in **Bash** on the target host:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh)
```

The script starts as the current user and shows a terminal-width-aware MOTD with the IPv8 “8” logo, detected host, installed version when available, and verified service state. The numbered menu provides panel/agent installation, configuration, separate uninstallation, status/health, network and DNS information, prerequisites, update, logs, and exit. Unsupported systemd actions are disabled clearly on Termux. Every installation or persistent configuration change is reviewed and explicitly confirmed before downloads or system changes; a normal Linux user separately consents before the installer requests `sudo`. No packages are installed automatically.

Menu: **1** install panel, **2** install agent, **3** configure IPv8, **4** uninstall agent, **5** uninstall panel, **6** status/health, **7** network information, **8** DNS guide, **9** prerequisites, **10** update, **11** logs/troubleshooting, **12** exit. The 8 mark and menu work without color; Termux hides no unsupported systemd behavior.

Panel installation is currently available on supported systemd Linux hosts. It builds/downloads the same Go executable and creates `/etc/ipv8/panel.env` plus `ipv8-panel.service`; the default listener is `127.0.0.1:8080`, the database is `/var/lib/ipv8/ipv8.json`, and systemd runs it with `DynamicUser`. The wizard permits only a loopback origin listener. Public access requires an HTTPS reverse proxy or Cloudflare Tunnel and matching `IPV8_PUBLIC_URL`/`IPV8_API_URL`. The Go process does not load `.env` files. Existing databases are preserved. Configure edits the managed environment file with a backup, then restarts the service after confirmation.

Agent installation asks for the node name, install prefix, HTTPS panel origin, terms acceptance, and (for a new enrollment) a one-time token read without terminal echo. It reviews target files, platform, network use, and privileges. In the summary choose 2 to edit or 3 to cancel. The panel itself has no public release asset yet unless a compatible GitHub Release is published; the installer offers a clearly disclosed Go 1.24+ source build when no compatible Linux asset exists.

In the account panel, **Install IPv8** creates a 15-minute, one-use enrollment token and gives a copyable command with the real configured HTTPS panel origin. It never puts the token in the command. The script source itself is fetched over HTTPS through process substitution; the Linux agent release is checksum-checked against the release's `SHA256SUMS`. That manifest is not a cryptographic signature. When no compatible release is published, the installer explains the source-build fallback and asks before fetching the repository source; source archives are not signed and are compiled without root.

On Linux the installer installs `/usr/local/bin/ipv8` (or the selected prefix), `/etc/ipv8/agent.json` mode `0600`, and a managed systemd unit. Existing credentials are preserved; unrelated files and symlinks are refused. The service reports status only through authenticated heartbeats. On Termux it installs under `$PREFIX`, creates no Android service, and prints the foreground command to start the agent. No route, firewall, DNS, or other system networking configuration is changed.

The installer requires an interactive terminal for review and token entry. To inspect the source without executing it, run `curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh | less`. The one-line command above uses Bash process substitution and does not need the script's directory.

Use menu option 4 to uninstall the agent or option 5 to uninstall the panel. The utility stops and disables only the managed IPv8 service, removes its unit, and keeps agent credentials, panel configuration, and database by default. Database deletion requires a separate exact `DELETE` confirmation. Revoke an agent in **My IPv8** before removing its local credentials where possible. In Termux, stop the foreground agent with Ctrl+C and revoke it in the panel before deleting its user-local files manually. The manager does not remove backups or Cloudflare Tunnel configuration.

## DNS metadata

To publish metadata, add this conventional TXT record at `_ipv8.<your-domain>`:

```text
Type:  TXT
Name:  _ipv8
Value: v=ipv8-1; address=YOUR_CANONICAL_IPV8_ADDRESS
```

Admins can compare the public TXT response with an expected address in **Nodes → Verify an IPv8 DNS mapping**. The record is an application-defined format, not a new DNS record type or standard. DNS TTL and resolver caching apply. A record does not prove cryptographic identity or create a transport/reachability route. No IPv8 DNS server is provided. See [DNS.md](DNS.md).

## Troubleshooting, security, and privacy

- **No Linux release asset:** after the installation review the installer offers a Go 1.24+ source build from the HTTPS GitHub source archive. GitHub archives are not signed; the source build is performed as the current user. If declined/unavailable, wait for a compatible tagged release.
- **Termux prerequisites:** use Android arm64, install Go yourself with Termux's package manager, then rerun; IPv8 never installs packages for you.
- **Enrollment fails:** verify the configured panel `/healthz` endpoint is reachable over HTTPS and request a fresh token. Tokens expire after 15 minutes and are single-use.
- **Service fails or node is Offline:** use `sudo systemctl status ipv8-agent` and `sudo journalctl -u ipv8-agent -n 50`; confirm outbound HTTPS. Active service does not itself prove Online; a valid heartbeat must arrive within 90 seconds.
- **Cloudflare 502:** verify `ipv8-panel.service`, local `/healthz`, and the tunnel route to `http://127.0.0.1:8080`; see [DEPLOYMENT.md](DEPLOYMENT.md).
- **Panel service unavailable:** check `sudo systemctl status ipv8-panel` and `sudo journalctl -u ipv8-panel -n 50 --no-pager`. The database path must be under `/var/lib/ipv8/`; configuration values are supplied through the managed systemd environment file, not by `.env`.

Passwords use PBKDF2 hashing. The panel stores session and agent credential hashes; an agent stores its credential in a mode-`0600` config file. Enrollment tokens are transient, short-lived, and hidden during terminal input. The database stores user email/display name, IPv8 address, node name/platform/version, and heartbeat times. The agent sends no location, host metrics, traffic, or packet payload. Protect the database and backups. See [SPECIFICATION.md](SPECIFICATION.md) for actual address, packet, and DNS formats and limitations.

## Development

```sh
gofmt -w cmd internal web
go test ./...
go build ./...
go vet ./...
bash -n install.sh
sh -n deploy/configure-cloudflare.sh
node --check web/app.js
python3 tests/installer_test.py -v
git diff --check
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for architecture, API/auth flow, storage, packet/DNS handling, installer behavior, and contributions. CI runs Go, shell, JavaScript, release-build, and installer pseudo-terminal checks. License: MIT ([LICENSE](LICENSE)).
