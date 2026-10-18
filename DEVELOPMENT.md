# Development Guide

## Architecture and folders

This is a dependency-free Go module (`github.com/mryuvraj1/ipv8`, Go 1.24+). A single Go process serves the API, static panel, and health endpoint.

```text
cmd/ipv8/          CLI entry point: server, admin bootstrap, enrollment, agent
internal/app/      HTTP handlers, authentication, JSON persistence, rate limits
internal/core/     IPv8 v1 address and packet codecs
web/               HTML, CSS, and JavaScript embedded into the server binary
install.sh         Linux systemd agent installer
SPECIFICATION.md   Address and packet format, reserved values, and limits
```

`web/files.go` embeds the panel assets into the binary. The browser uses same-origin `fetch()` requests with JSON bodies and an HttpOnly session cookie. `GET /api/branding` is public; account, address, simulator, and installation endpoints require a session. `GET /healthz` returns a small readiness response.

## Storage and configuration

The server persists state as one JSON file at `IPV8_DATA` (default `./data/ipv8.json`). It stores users and password hashes, sessions as token hashes, addresses, one-time enrollment-token hashes, installation-secret hashes and timestamps, plus platform branding. Mutations are serialized under a process mutex, written to a mode-0600 temporary file, synced, and atomically renamed. Newly created parent directories use mode 0700.

The file is designed for one server process on one host. There is no cross-process locking, replication, encryption at rest, or database migration framework. Stop the server before copying the file for backup. The service reads `IPV8_ADDR` (default `127.0.0.1:8080`), `IPV8_DATA`, and `IPV8_COOKIE_SECURE` (default `false`). It does not read `.env`; shell or service configuration must export the values. See `.env.example` for placeholders.

## Authentication and permissions

Public registration always creates role `user`; request fields cannot select a role. There is no public admin registration. An administrator is created once using `IPV8_ADMIN_EMAIL` and `IPV8_ADMIN_PASSWORD` with `ipv8 bootstrap-admin`; passwords must be at least 14 characters for this command. Passwords are stored as PBKDF2-HMAC-SHA-256 hashes (600,000 iterations, 16-byte random salt, 32-byte result).

User and admin login have separate endpoints (`/api/login` and `/api/admin/login`) and verify both password and role. Login returns a random 256-bit opaque cookie; only its SHA-256 hash is stored. The cookie is HttpOnly, SameSite=Strict, expires after 12 hours, and gains the Secure attribute when `IPV8_COOKIE_SECURE=true`. Logout deletes the stored token hash. The handler rejects mismatched `Origin` hosts and does not enable CORS. Registration, login, enrollment, and heartbeat endpoints have per-process fixed-window rate limits.

Backend handlers enforce roles. Users can update only their own display name/avatar, get their one stored address, manage their installations, and run the own-address loopback simulator. Admin-only overview, user-directory, and branding endpoints require an admin session. Public responses omit password hashes. Image uploads are embedded PNG/JPEG data, limited to 128 KiB, and their image dimensions are bounded before storage.

## Address generation and parsing

`internal/core/address.go` generates 16 bytes with `crypto/rand`. The canonical text is `ipv8:1:` followed by 26 lowercase, unpadded RFC 4648 Base32 characters, exactly 33 ASCII bytes overall. Parsing checks the prefix, length, alphabet, reserved values, decoded length, and canonical re-encoding. All-zero and all-`0xff` values are reserved.

When a user first requests an address, `internal/app` checks generated values against every stored user address while holding the state mutex and persists the selected value. Each user gets one stable address. Uniqueness applies only to a single intact JSON database handled by one process; separately operated instances do not coordinate.

## Packet processing and simulator

`internal/core/packet.go` implements the 72-byte header and SHA-256 corruption check described in `SPECIFICATION.md`. The decoder validates magic, version, flags, source/destination identifiers, payload length, exact frame length, and checksum before returning a packet. Payloads are capped at 1200 bytes.

`POST /api/simulate` accepts a user's own generated address and a payload. The server encodes a packet from that address to itself, decodes and validates the encoded bytes, then returns the delivered payload and packet size. This is an in-process loopback demonstration; the code does not open a packet socket, route between users, authenticate packet senders, or encrypt payloads. The checksum is not a security signature.

## Installation and heartbeat behavior

`POST /api/installations/token` issues a random 256-bit token, stores only its hash, expires it after 15 minutes, and invalidates any older unused token for that account. `POST /api/installations/enroll` consumes the token once and returns a random installation ID and secret. The server stores only a hash of the secret; the client writes it to `/etc/ipv8/agent.json` with mode 0600.

The agent posts the ID and secret to `/api/agent/heartbeat` immediately and every 30 seconds over HTTPS (plain HTTP is accepted only for localhost prototypes). The panel calls an installation online when its last authenticated heartbeat is within 90 seconds. Removing an installation deletes its server credential, so the agent is rejected on its next heartbeat. No route, firewall, interface, or DNS configuration is changed.

The installer supports Debian, Ubuntu, Fedora, and Arch Linux with systemd on amd64 and arm64. It checks prerequisites, downloads the repository source archive over HTTPS, and builds the Go binary. It writes only its own `/usr/local/bin/ipv8`, `/etc/ipv8/agent.json`, and `ipv8-agent.service` paths; it refuses to overwrite an unrelated unit, preserves and validates an existing agent config, and is safe to rerun with that config. The installer requires Go 1.24+, `curl`, and `tar` to be installed already.

## Local commands and verification

```sh
cp .env.example .env # edit placeholders first; .env is not loaded automatically
set -a
. ./.env
set +a
go run ./cmd/ipv8 bootstrap-admin
unset IPV8_ADMIN_PASSWORD
go run ./cmd/ipv8 serve
```

To run automated checks:

```sh
gofmt -w cmd internal web
go vet ./...
go test -race ./...
go build -o ./ipv8 ./cmd/ipv8
sh -n install.sh
node --check web/app.js
```

The integration test exercises registration, user/admin login separation, admin-only APIs, profile changes, branding persistence after restart, address generation and uniqueness, packet simulation validation, one-time enrollment, heartbeat status, installation ownership, logout, and login after logout.

## Contributing

Run the checks above and add tests for behavior changes. Keep the specification and UI claims synchronized with what the server actually implements. Never add secrets or local database files to Git. See `CONTRIBUTING.md` for the short pull-request checklist.
