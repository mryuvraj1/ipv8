# IPv8

IPv8 is an open-source Go prototype for 256-bit experimental addresses, a local packet envelope/simulator, an account panel, and an optional Linux heartbeat agent. The addresses are local identifiers; this project does not implement peer transport, public routing, VPN behavior, or an IPv4/IPv6 replacement. RTT, bandwidth, geolocation, traffic, and history metrics are not measured.

## Requirements

- Go 1.24 or newer for the panel and development.
- Linux systemd 247+ on amd64 or arm64 for the optional agent installer; see the system requirements in **My IPv8 → Install IPv8**.
- The panel has no npm dependencies. Agent installation uses a published checksum-verified GitHub Release and does not need Go on the target after a release exists.

## Local setup and run

```sh
git clone https://github.com/mryuvraj1/ipv8.git
cd ipv8
cp .env.example .env # reference only; the Go app does not auto-load .env
```

The panel can start immediately. Create a user through the registration page. To bootstrap the first administrator, provide credentials only for the one-time command; no public admin registration exists:

```sh
read -r -p 'Admin email: ' IPV8_ADMIN_EMAIL
read -r -s -p 'Admin password (14+ characters): ' IPV8_ADMIN_PASSWORD; printf '\n'
export IPV8_ADMIN_EMAIL IPV8_ADMIN_PASSWORD
go run ./cmd/ipv8 bootstrap-admin
unset IPV8_ADMIN_EMAIL IPV8_ADMIN_PASSWORD
go run ./cmd/ipv8 serve
```

Open <http://127.0.0.1:8080>. The first database write creates `./data/ipv8.json` with restrictive permissions. Storage is a single-process JSON file; stop the app before copying its database and keep backups private.

## Configuration

Set environment variables in the shell or service manager. `.env.example` contains placeholders only, and this application does not parse `.env` files.

| Variable | Default | Purpose |
| --- | --- | --- |
| `IPV8_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `IPV8_DATA` | `./data/ipv8.json` | Persistent account, address, installation, session, and branding store |
| `IPV8_PUBLIC_URL` | empty | Actual public HTTPS origin; enables remote install command generation |
| `IPV8_API_URL` | empty | Same-origin API URL; if set, must match `IPV8_PUBLIC_URL` |
| `IPV8_COOKIE_SECURE` | `false` locally | Set `true` for a public HTTPS deployment; explicit `false` with a public HTTPS URL is rejected |
| `IPV8_ADMIN_EMAIL` | unset | Used only by `bootstrap-admin` |
| `IPV8_ADMIN_PASSWORD` | unset | Used only by `bootstrap-admin`; at least 14 characters |
| `CLOUDFLARE_TUNNEL_TOKEN` | unset | Placeholder for the separate tunnel setup helper only; never configure it in the Go app, frontend, or agent |

Public admin registration is disabled. `bootstrap-admin` refuses to create a second administrator. The panel uses HttpOnly, SameSite=Strict session cookies and automatically enables Secure cookies for a configured HTTPS public URL.

## Build and checks

```sh
gofmt -w cmd internal web
go test ./...
go vet ./...
go build ./...
sh -n install.sh
sh -n deploy/configure-cloudflare.sh
node --check web/app.js
```

CI additionally runs Go race tests and release builds on GitHub-hosted Linux runners.

## Deploy behind Cloudflare Tunnel

The Go panel, API, and `/healthz` endpoint share one port: default `127.0.0.1:8080`. Configure `IPV8_PUBLIC_URL` and `IPV8_API_URL` to the same real HTTPS origin and `IPV8_COOKIE_SECURE=true`, then route the Cloudflare published hostname to `http://127.0.0.1:8080`. Do not expose the origin port publicly. Follow [DEPLOYMENT.md](DEPLOYMENT.md) for tunnel creation, protected token setup, persistent service commands, DNS, and 502 troubleshooting. No public domain is configured by this repository.

## Install an agent

After a published GitHub Release exists, sign in and open **Install IPv8**. The panel creates a one-time token, valid for 15 minutes, and shows a copyable command only when the configured public HTTPS origin matches the panel currently open. On supported Linux, the installer prompts for experimental terms, reviews the target, and asks before system changes. It downloads the matching amd64/arm64 binary and service unit from the latest GitHub Release, verifies both using `SHA256SUMS`, enrolls over HTTPS, and enables `ipv8-agent.service`.

The command shape is:

```sh
curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh | sudo sh -s -- --server 'https://YOUR_CONFIGURED_PANEL' --token 'ONE_TIME_TOKEN' --name 'NODE_NAME' --interactive
```

Use the generated command from the panel for the actual configured HTTPS origin and token. It contains a temporary enrollment credential; do not save or share it. `--dry-run` checks the supported host and prints the intended work without downloading, consuming the token, or changing files. The agent sends authenticated heartbeats every 30 seconds; Online means one arrived in the previous 90 seconds. It does not carry IPv8 packets or change routes/firewall rules.

To uninstall, first revoke the installation in **My IPv8**, then run:

```sh
sudo systemctl disable --now ipv8-agent
sudo rm -f /etc/systemd/system/ipv8-agent.service /usr/local/bin/ipv8 /etc/ipv8/agent.json
sudo rmdir /etc/ipv8 # only removes the directory if empty
sudo systemctl daemon-reload
```

## Development and license

See [DEVELOPMENT.md](DEVELOPMENT.md) for architecture, API, storage, authentication, address/packet formats, and contribution checks. See [SPECIFICATION.md](SPECIFICATION.md) for the exact core format and limitations. License: MIT, in [LICENSE](LICENSE).
