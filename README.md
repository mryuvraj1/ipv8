# IPv8

IPv8 is an open-source Go prototype for an experimental address format, packet envelope, web panel, and heartbeat-only Linux agent. The panel supports separate user/admin access, address generation, user suspension, profile settings, and persistent platform branding. **Addresses are local identifiers: there is no packet transport, public routing, or IPv4/IPv6 replacement.** See [SPECIFICATION.md](SPECIFICATION.md) for the implemented format and limits.

## Requirements

- Go 1.24 or newer.
- Linux is needed only for the optional agent installer.
- The panel uses the Go standard library and bundled HTML/CSS/JavaScript; there are no npm dependencies.

## Run locally

```sh
git clone https://github.com/mryuvraj1/ipv8.git
cd ipv8
cp .env.example .env
# Edit .env and replace the admin password placeholder with a random password.
set -a
. ./.env
set +a
go run ./cmd/ipv8 bootstrap-admin
unset IPV8_ADMIN_PASSWORD
go run ./cmd/ipv8 serve
```

Open <http://127.0.0.1:8080>. Register a user at the panel or sign in as the bootstrapped administrator. The first write creates `./data/ipv8.json` with restrictive permissions. The configured database is a single-process JSON file; it needs no separate database setup. Back it up securely and stop the server before copying it.

## Configuration

The server reads process environment variables. It does **not** automatically load `.env`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `IPV8_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `IPV8_DATA` | `./data/ipv8.json` | Persistent account, address, session, installation, and branding data |
| `IPV8_COOKIE_SECURE` | `false` | Set `true` when users access the panel through HTTPS |
| `IPV8_ADMIN_EMAIL` | unset | Required only by `bootstrap-admin` |
| `IPV8_ADMIN_PASSWORD` | unset | Required only by `bootstrap-admin`; at least 14 characters |

Public admin registration is disabled. Create the first admin once with `go run ./cmd/ipv8 bootstrap-admin`; remove the admin password from the environment immediately afterward. Do not commit `.env` or the database.

## Build, tests, and deployment

```sh
go test -race ./...
go vet ./...
go build -o ./ipv8 ./cmd/ipv8
./ipv8 serve
```

For deployment, run the server behind a TLS reverse proxy, keep the Go listener on loopback where possible, set `IPV8_COOKIE_SECURE=true`, and persist the directory containing `IPV8_DATA`. The application does not configure TLS, firewall rules, or host networking. One server process must own a JSON database at a time.

## Install the optional agent

Create a short-lived enrollment command in the user dashboard. It uses a single-use token that expires after 15 minutes. The installer supports Debian, Ubuntu, Fedora, and Arch Linux with systemd on `amd64` or `arm64`; it requires Go 1.24+, `curl`, and `tar`, and builds the agent from this repository's `main` source. The agent sends an authenticated heartbeat every 30 seconds and does not carry IPv8 packets or alter routes/firewall rules.

The panel provides the exact copyable command. Its form is:

```sh
curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh | sudo sh -s -- --server https://panel.example.com --token "$IPV8_ENROLLMENT_TOKEN"
```

Replace the server and token with your actual panel origin and the one-time token. Keep the token private. Uninstall with:

```sh
sudo systemctl disable --now ipv8-agent
sudo rm /etc/systemd/system/ipv8-agent.service /usr/local/bin/ipv8 /etc/ipv8/agent.json
sudo rmdir /etc/ipv8 # succeeds only if no unrelated files remain
sudo systemctl daemon-reload
```

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md) for the source layout, API/authentication flow, storage model, packet processing, and contribution checks. CI runs formatting checks, Go tests with the race detector, `go vet`, a build, shell syntax validation, and the JavaScript parser check. Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
