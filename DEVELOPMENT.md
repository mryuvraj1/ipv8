# Development Guide

## Architecture and folders

IPv8 is a dependency-free Go module (Go 1.24+). One process serves the API, embedded panel assets, and `/healthz` at `IPV8_ADDR` (default `127.0.0.1:8080`). The browser is vanilla JavaScript/CSS, uses same-origin JSON `fetch`, and needs no Node build step.

```text
cmd/ipv8/                  Server, bootstrap-admin, enrollment CLI, heartbeat agent
internal/app/              HTTP API, auth, rate limits, JSON store, address allocation
internal/core/             256-bit address codec and v2 packet codec
web/                       Embedded HTML, CSS, JavaScript dashboard
packaging/ipv8-agent.service Hardened systemd unit for the user agent
install.sh                 Interactive Linux installer for release assets
deploy/configure-cloudflare.sh Secure token-file and Cloudflare Tunnel service setup
.github/workflows/         Go/JS/shell CI and tagged Linux release workflow
SPECIFICATION.md           Canonical address and packet formats
DEPLOYMENT.md              Cloudflare Tunnel configuration and diagnostics
```

## Browser/API contract

`web/app.js` calls the Go service via same-origin `/api/*` requests and receives the HttpOnly session cookie. The API and frontend currently must use one origin; split origins and CORS are not supported. `GET /api/runtime-config` exposes configured public and API origins only, never secrets. The install wizard creates a one-use enrollment token through `POST /api/installations/token` and forms a terminal command using the configured public URL. It cannot execute software on the user's machine and reports no installation success until the real agent sends a heartbeat.

Key routes:

- `GET /api/branding`, `GET /api/runtime-config`, `GET /healthz`: public branding, nonsecret runtime URLs, readiness.
- `POST /api/register`, `/api/login`, `/api/admin/login`, `/api/logout`, `GET /api/me`, `PATCH /api/me`: account/session/profile.
- `POST /api/address`, `GET /api/config`, `POST /api/simulate`: address allocation and local packet loopback.
- `GET /api/my-ipv8`, `GET /api/installations`, `DELETE /api/installations/{id}`: only the signed-in user's node data and revocation.
- `POST /api/installations/token`, `/api/installations/enroll`, `/api/agent/heartbeat`: short-lived enrollment and credential-authenticated agent heartbeat.
- `GET /api/admin/overview`, `GET /api/admin/nodes?limit=&offset=`, `GET /api/admin/nodes/{id}`, `GET /api/admin/users`, `PATCH /api/admin/users/{id}`, `PATCH /api/admin/branding`: admin-only overview, paginated inventory, details, account management, and branding.

All access checks happen in Go handlers. A user cannot request another account's installation; admin-only routes reject regular users. Admin node records include owner email and node metadata, so they are restricted to admins. Node history is not retained.

## Database and authentication

`IPV8_DATA` points to one JSON file. It holds account profile and password hash, sessions as hashed opaque tokens, address assignments, one-use enrollment token hashes, installation-secret hashes, installation name/platform/version/timestamps, and platform branding. Writes are mutex-serialized, atomically replaced, synced, and use restrictive file permissions. The JSON database supports one server process on one host; it does not have cross-process locking, replication, encryption at rest, or multi-host uniqueness.

Public registration always makes a user role. Admin registration is disabled. Create the first administrator with `IPV8_ADMIN_EMAIL` and `IPV8_ADMIN_PASSWORD` for `go run ./cmd/ipv8 bootstrap-admin`; the command refuses if an admin already exists. Password hashing is PBKDF2-HMAC-SHA-256 (600,000 iterations, 16-byte random salt, 32-byte result). Session cookies are HttpOnly, SameSite=Strict, 12-hour, and Secure for HTTPS deployments. Registration, login, enrollment token creation, enrollment, and heartbeats have in-process rate limits. Request bodies are limited, JSON unknown fields are rejected, and the API does not enable CORS.

`IPV8_PUBLIC_URL` and `IPV8_API_URL` are optional HTTP(S) origins. Remote use requires HTTPS; when configured for deployment they must match. The app never reads `.env`; put only nonsecret app settings in the panel service environment. `CLOUDFLARE_TUNNEL_TOKEN` is handled only by the separate tunnel helper and must not enter the Go service environment.

## IPv8 address allocation

`internal/core/address.go` encodes 32-byte values (256 bits) using uppercase unpadded RFC 4648 Base32, grouped as 13 four-character groups after `ipv8:`. Parsing requires the exact prefix/grouping/alphabet and canonical zero pad bits. All-zero and all-`0xff` are reserved. Generation uses `crypto/rand`; the app checks collisions against persisted accounts and retries before atomically assigning. Capacity is `2^256 - 2`, and the random birthday collision chance is negligible at the intended scale. Uniqueness is only within one intact database and allocator; there is no cross-service/global uniqueness or routing guarantee. Old noncanonical stored values are replaced with new valid addresses during startup migration; valid unique values are retained.

See [SPECIFICATION.md](SPECIFICATION.md) for complete bits, grouping, reserved values, packet header offsets, and checks.

## Packet processing and measurements

`internal/core/packet.go` implements packet version 2 with a 104-byte header, 32-byte source/destination IDs, 16-bit payload length, SHA-256 corruption check, and a maximum 1200-byte payload. Decoding rejects unsupported versions, reserved addresses, invalid lengths, trailing bytes, and checksum errors. The hash is not a signature.

`POST /api/simulate` accepts only a user's own address, encodes then decodes a packet to itself, and returns it inside the same process. There is no socket transport, route, peer discovery, forwarding, or public connectivity. Therefore the admin panel reports no node-to-node RTT, jitter, loss, bandwidth, traffic counters, geolocation, measurement history, or network map. These are explicitly unmeasured, not synthesized from heartbeat intervals. An authenticated heartbeat measures agent-to-panel reachability only.

## Enrollment and Linux installer

The user requests a token from the authenticated panel. The database stores its hash; it expires after 15 minutes, can be used once, and a new request invalidates the prior pending token for that user. Enrollment returns a unique node ID and 256-bit secret once; the server stores only the secret hash. The agent stores its secret in `/etc/ipv8/agent.json` mode `0600`. The systemd service uses `DynamicUser` and `LoadCredential` to provide it only at runtime. Heartbeats happen immediately and every 30 seconds over HTTPS; redirects are not followed. Status is Online when a matching authenticated heartbeat was received within 90 seconds, Offline otherwise, and Not Installed when the account has no node.

A tagged release workflow builds checksum-manifested `linux/amd64` and `linux/arm64` assets plus the service unit. `install.sh` supports Debian, Ubuntu, Fedora, Arch Linux, systemd 247+, and those architectures. It checks root, platform, systemd, dependencies, safe target paths, then prompts for experimental terms and confirms reviewed node details. It fetches the selected architecture and service unit from the latest GitHub Release and verifies both against `SHA256SUMS` before enrollment/install. Existing enrollment config is not overwritten on rerun. It does not change routes, DNS, or firewall settings. `--dry-run` performs host/configuration checks and prints intent without network downloads, token consumption, or writes.

Release assets do not exist until a `v*` tag is pushed and the GitHub Release workflow succeeds. A missing release makes installation fail before enrollment or service changes. After the release publishes, the panel gives the exact command with the configured real HTTPS endpoint and single-use token. The installer requires an interactive terminal for explicit terms acceptance and system-change confirmation (it reads `/dev/tty` when piped to `sudo sh`).

## Cloudflare Tunnel deployment

The app's single origin remains bound to `127.0.0.1:8080`; configure a Cloudflare published hostname to service `http://127.0.0.1:8080`. Set the same real public HTTPS origin in `IPV8_PUBLIC_URL` and `IPV8_API_URL`, set Secure cookies, restart the Go service, and verify local and public `/healthz` endpoints. `deploy/configure-cloudflare.sh` accepts `CLOUDFLARE_TUNNEL_TOKEN` only via environment, refuses to replace existing credentials/service units, writes `/etc/ipv8/cloudflared-token` mode 0600, and installs a systemd service that uses `--token-file` and systemd credentials. It requires cloudflared 2025.4.0+ and does not install cloudflared itself. The tunnel token never reaches the browser, the agent, or app API. See [DEPLOYMENT.md](DEPLOYMENT.md) for dashboard routes, token rotation, service management, and 502 diagnosis.

## Theme and UI

The panel uses a custom CSS variable theme with dark/light modes and an OS-following setting, stored in browser local storage. The mobile sidebar is a keyboard-dismissible drawer. Overview values are returned by real account/install APIs; the Admin Network Overview and node detail pages show actual accounts, registrations, platform/version and heartbeat timestamps. The UI labels unavailable metrics rather than displaying sample values. User location, password changes, notification history, and server-side session management are not implemented and are not requested by the UI.

## Local development and checks

```sh
cp .env.example .env # reference only, Go does not load this file
go run ./cmd/ipv8 serve
```

Open `http://127.0.0.1:8080`, register a user, then generate an address. For an administrator, export email/password just for `go run ./cmd/ipv8 bootstrap-admin`, then unset both. To use a custom data file or listen address, export `IPV8_DATA` or `IPV8_ADDR` before starting the process.

Run checks:

```sh
gofmt -w cmd internal web
go test ./...
go vet ./...
go build ./...
sh -n install.sh
sh -n deploy/configure-cloudflare.sh
node --check web/app.js
```

CI additionally runs `go test -race ./...` on GitHub-hosted Linux. Tests cover address canonicalization, reserved values, collision allocation/migration, packet validation/roundtrip, user/admin access, enrollment, invalid and valid heartbeats, status freshness, revocation, persistence, agent config permissions, redirect handling, URL configuration, and Cloudflare installer script syntax.

Contributions should keep documentation in sync, run these checks, and avoid committing `.env`, the local database, agent credentials, or Cloudflare tunnel credentials. See [CONTRIBUTING.md](CONTRIBUTING.md).
