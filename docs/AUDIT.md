# IPv8 Phase 0 Audit

Status: Phase 0 deliverable. This document records the measured state of the repository. It adds no capability, changes no behavior, and is the basis for the implementation plan that follows maintainer approval.

| Item | Value |
| --- | --- |
| Repository | `github.com/mryuvraj1/ipv8` |
| Branch | `main` |
| Commit audited | `114da74` — "Add DNS verification and IPv8 MOTD manager" (2026-10-19) |
| Audit date | 2026-11-07 |
| Go toolchain | `go1.24.5 linux/amd64` (matches the `go 1.24` directive in `go.mod`) |
| Module | `github.com/mryuvraj1/ipv8`, zero external dependencies |
| Tracked files | 33 (5,790 lines) |
| Source changes made | none — this commit adds this document and corrects two documentation defects |

## 1. Verdict

The repository is a well-engineered **single-process control plane** with strong hygiene around persistence, parsing, credential handling, and packaging. It is **not** a networking protocol.

A search for `net.Listen`, `ListenUDP`, `ListenTCP`, `net.Dial`, and `tls.` across all non-test Go files returns **zero matches**. The only traffic the project originates is authenticated HTTPS status heartbeats from the node agent to the panel. Every statement in the README and [SPECIFICATION.md](../SPECIFICATION.md) about the absence of transport, forwarding, and routing is accurate; the documentation understates capability rather than overstating it, which is rare and worth preserving as a project norm.

The existing foundation is suitable to build the protocol on. The blockers are design decisions, not defects, and are listed in §11.

## 2. Method and reproducibility

Every check below was executed against a clean clone at `114da74` on the toolchain above. Commands are the project's own, as defined in [CONTRIBUTING.md](../CONTRIBUTING.md).

| Command | Result |
| --- | --- |
| `gofmt -l cmd internal web` | pass — no files listed |
| `go vet ./...` | pass |
| `go build ./...` | pass |
| `go test -race ./...` | pass — `cmd/ipv8` 6.5 s, `internal/app` 30.4 s, `internal/core` 1.0 s, `internal/dnsrecord` 1.0 s |
| `go test -cover ./...` | `internal/core` 93.8%, `internal/dnsrecord` 92.1%, `internal/app` 76.4%, `cmd/ipv8` 48.1%, `web` no test files |
| `bash -n install.sh` | pass |
| `sh -n deploy/configure-cloudflare.sh` | pass |
| `node --check web/app.js` | pass |
| `git diff --check` | clean |
| `python3 tests/installer_test.py -v` | 10 tests; **flaky under load** — see below |
| `govulncheck ./...` | no vulnerabilities found |

Test inventory: 23 Go test functions (several table-driven, covering address, packet, DNS record, API, CLI, and password hashing) plus 10 PTY-driven installer tests. Go sources are 1,889 production lines against 1,017 test lines.

### 2.1 Baseline defect: installer test deadlines

`tests/installer_test.py` drives the interactive menu over a PTY and waits on a fixed interval. Under concurrent load, baseline runs produced intermittent failures of the form:

```
AssertionError: timed out waiting for 'Installation name [first-node]'
```

Two of the first runs failed (2 failures, then 1); three subsequent runs were clean at 8.2 s each. The deadline is a hard-coded 8 seconds in the `run_tty`/`until` helpers, not a product fault: the menu and prompt behavior is correct, the test simply has no headroom. This will intermittently turn CI red and should be fixed before more phases are added to the pipeline.

## 3. Source inventory

| Area | Files (lines) | Role |
| --- | --- | --- |
| `internal/core/` | `address.go` (87), `packet.go` (65) | 256-bit address codec; version 2 packet envelope |
| `internal/app/` | `app.go` (1,105), `store.go` (165) | HTTP API, auth, sessions, rate limiting, DNS verification endpoint, JSON store |
| `internal/dnsrecord/` | `record.go` (136) | Strict TXT parsing and bounded resolution |
| `cmd/ipv8/` | `main.go` (323) | `serve`, `bootstrap-admin`, `enroll`, `agent`, `check-config`, `version` |
| `web/` | `index.html` (14), `app.js` (235), `app.css` (827), `files.go` (8) | Embedded vanilla-JS dashboard, no build step |
| `install.sh` | 945 | Twelve-action MOTD installer for Linux and Termux |
| `tests/` | `installer_test.py` (196) | PTY menu and cancellation tests |
| `deploy/` | `configure-cloudflare.sh` (68) | Token-file and tunnel unit helper |
| Go tests | 6 files (1,017) | — |
| Documentation | `README.md`, `SPECIFICATION.md`, `DEVELOPMENT.md`, `DEPLOYMENT.md`, `DNS.md`, `CONTRIBUTING.md` (423) | — |

`install.sh` is 16% of the repository by line count. That is disproportionate to the protocol work and is a maintenance risk: it must keep working through every later phase while being the hardest artifact to test.

## 4. Architecture as built

```
browser (app.js, same-origin fetch, HttpOnly session cookie)
   |
   v
cmd/ipv8 serve -> internal/app.App (net/http mux + security headers)
                    |-- accounts, sessions, admin roles
                    |-- in-memory rate limiter (path + RemoteAddr, one-minute window)
                    |-- internal/core (address generation/parsing, packet encode/decode)
                    |-- internal/dnsrecord (TXT lookup and validation)
                    +-- internal/store (in-memory database -> atomic JSON rewrite)
   ^
   |  HTTPS POST /api/agent/heartbeat (installation id + secret, every 30 s)
cmd/ipv8 agent -> agent.json (0600) -> systemd unit, DynamicUser, LoadCredential
```

There is no data plane. The only packet path is `POST /api/simulate`, which serializes a packet, immediately decodes it in the same goroutine, and returns the validated payload. It additionally rejects any destination other than the caller's own address, so it cannot express a sender and a receiver.

## 5. Capability matrix

| Subsystem | Classification | Basis |
| --- | --- | --- |
| 256-bit address codec | Working | 93.8% coverage; canonical round-trip, reserved values, bad grouping, non-canonical pad bits rejected |
| Packet encode/decode (v2) | Working | Round-trip, truncation, trailing bytes, checksum, version rejection tested |
| JSON persistence | Working; single process only | Temp file, `0600`, file and directory sync, atomic rename, symlink and permission refusal |
| Accounts, sessions, admin | Working | End-to-end API tests including suspension revocation and role separation |
| Enrollment and heartbeat agent | Working | Single-use 15-minute tokens, constant-time secret comparison, no credential forwarding across redirects |
| DNS TXT verification | Working; metadata only | Fail-closed on duplicates, malformed fields, unsupported versions, and expired records |
| Dashboard | Working | Unavailable metrics render as unavailable; no fabricated values found |
| Installer | Working; tests flaky | See §2.1 |
| Packet simulator | Experimental | Self-addressed loopback only |
| Cryptographic node identity | Missing | No keypair, no signature, no key storage |
| Transport (UDP/TCP) | Missing | No listen or dial call sites |
| Authenticated sessions / key agreement | Missing | — |
| Peer discovery | Missing | Agent heartbeat only |
| Routing, relay, forwarding | Missing | Correctly documented as absent |
| Name system with authentication | Missing | TXT records are unsigned metadata |
| Client SDK | Missing | No importable client package |
| Benchmarks, fuzz targets | Missing | No `func Benchmark` or `func Fuzz` |
| Packet type, sequence, hop limit, fragmentation | Missing | Flags byte must be zero; no such fields exist |

## 6. Security assessment

### 6.1 Strengths to preserve

* Atomic, fsynced persistence with `0600` permissions; symlinked and group-readable credential files are refused.
* PBKDF2-HMAC-SHA256 at 600,000 iterations with per-hash salts and constant-time comparison. The hand-rolled implementation is vector-tested and correct.
* Session, enrollment, and installation secrets are stored only as hashes.
* Enrollment and admin bootstrap secrets are read from stdin, never argv or files.
* CSP `default-src 'self'`, `frame-ancestors 'none'`, and an Origin/Host consistency check on mutating requests.
* `DisallowUnknownFields`, a 1 MiB body cap, `ReadHeaderTimeout`, and content sniffing on uploaded images rather than trusting a declared type.
* No known vulnerabilities reported by `govulncheck`.

### 6.2 Weaknesses

| # | Severity | Finding | Evidence |
| --- | --- | --- | --- |
| 1 | High | No cryptographic identity. The packet checksum is the only integrity mechanism and the source address is entirely self-asserted, so any sender can claim any address. Documented honestly, but it means transport work cannot ship before identity work without exposing a spoofing surface. | `internal/core/packet.go` |
| 2 | High | Rate limiting collapses to one global bucket behind the documented deployment. The key derives from `r.RemoteAddr` and never consults `X-Forwarded-For` or `CF-Connecting-IP`; behind Cloudflare Tunnel every request arrives from `127.0.0.1`. The heartbeat limit of 60/minute then applies to the whole fleet, so at a 30 s interval the panel saturates at roughly 30 nodes, and enrollment is capped at 12/minute globally. An availability cliff, not a theoretical one. | `internal/app/app.go` (`RemoteAddr` split, `limit(60, ...)`, `limit(12, ...)`) |
| 3 | Medium | Every authenticated write rewrites and fsyncs the entire database. The mutation path marshals the whole in-memory database, deep-copies it, writes a temp file, syncs file and directory, then replaces. Heartbeats are writes, so cost is O(database size) per heartbeat under a single mutex. | `internal/app/store.go` (`change`) |
| 4 | Medium | Node identity does not exist and addresses are account-scoped. Admin node views return the owning user's address for every installation, so all nodes of one account share one identifier. Separately, startup migration silently regenerates any invalid or duplicate stored address, so a randomly minted identifier can change with no audit trail. | `internal/app/app.go`, `internal/app/store.go` |
| 5 | Medium | The packet header has no type, sequence, hop limit, or fragmentation, and a version mismatch is a hard reject. A version 3 header therefore cannot be rolled out without an explicit coexistence rule. | `internal/core/packet.go` |
| 6 | Low | Expired sessions are only cleaned up when a new session is created. | `internal/app/app.go` |
| 7 | Low | The simulator accepts only self-addressed packets, so it cannot model two parties. Multi-node simulation is new work, not a tweak. | `internal/app/app.go` (`/api/simulate`) |
| 8 | Low | The dashboard reads `state.installExpires` without declaring it in the initial state object; it renders empty rather than failing. | `web/app.js` |
| 9 | Process | No `SECURITY.md` threat model, no `ARCHITECTURE.md`, no `ROADMAP.md`, and no documented trust boundaries, which the project's own rules require before transport claims are made. | repository root |

## 7. Technical debt

* `internal/app/app.go` is a 1,105-line file mixing routing, session logic, cryptographic primitives, validation, and DNS verification. The in-file PBKDF2 and HMAC implementations are correct but are avoidable cryptographic surface; the standard library provides `crypto/pbkdf2`.
* No fuzz targets for the two parsers that will face hostile input (`Unpacket`, `dnsrecord.Parse`). This is the highest-value missing test work in the repository.
* No benchmarks, so later performance claims have no baseline.
* CI does not run `govulncheck`, fuzz smoke tests, or any transport-level job.
* The installer test harness uses fixed deadlines with no retry (§2.1).
* Documentation drift, listed in §8.

## 8. Documentation defects

Two of these are corrected in this commit; the rest are recorded for a later documentation pass:

* **Fixed here** — `README.md` repeated the same paragraph twice, once after the local bootstrap and again after the systemd bootstrap.
* **Fixed here** — `SPECIFICATION.md` announced "version 1" while the document specifies packet format version 2 and address format version 1. The status line now distinguishes the document revision from the wire versions it defines.
* Open: no `SECURITY.md`, `ARCHITECTURE.md`, or `ROADMAP.md`.
* Open: `SPECIFICATION.md` describes the DNS record as unauthenticated metadata, but `DNS.md` does not repeat that limitation as prominently.
* Open: `DEVELOPMENT.md` does not state the rate-limiter and single-database scaling limits identified in §6.2.

## 9. Gap analysis against the target protocol

The subsystems required for the target protocol map to the missing rows in §5. The dependency order is forced: identity before address binding, binding before packets on a wire, packets before transport, transport before sessions, sessions before discovery, and discovery before routing. Attempting transport first would ship unauthenticated packets with forgeable source addresses.

## 10. Recommended roadmap

Relative effort only; no calendar estimates are made. Each step is independently reviewable and testable.

| Step | Work | Effort | Rationale |
| --- | --- | --- | --- |
| 0a | Fix installer test deadlines, README duplication, specification version drift, undeclared dashboard state | S | Cheap; restores trustworthy CI (two items land with this document) |
| 0b | Add `SECURITY.md` threat model, `ARCHITECTURE.md`, `ROADMAP.md` | S | Required before transport claims |
| 1 | `internal/identity`: Ed25519 keypair, `0600` on-disk key handling, sign/verify, rotation and revocation records | M | Everything downstream depends on it |
| 2 | Key-bound node addresses in the existing 256-bit text format, with signed binding of legacy account addresses | M | Requires the decision in §11.1 |
| 3 | Packet version 3: type, sequence, hop limit, session identifier; retain the version 2 decoder and define coexistence | M | Must precede any wire use |
| 4 | `internal/transport`: UDP datagram listener plus TCP framed fallback, bounded queues, per-peer limits, timeouts, graceful shutdown | L | First real data plane; the simulator stays untouched |
| 5 | `internal/session`: TLS 1.3 mutual authentication, session binding, replay window, expiry | L | Zero new dependencies under the decision in §11.2 |
| 6 | `internal/discovery`: static bootstrap list, signed peer announcements, expiry and health | M | Needs steps 1 and 4 |
| 7 | Versioned signed DNS record alongside `v=ipv8-1`, which remains explicitly unauthenticated | M | Preserves current compatibility |
| 8 | Direct routing, then multi-hop with hop limit and loop detection, then optional relays | L | Only after 4 and 5 are stable |
| 9 | Node runtime as a new subcommand beside the existing agent, not a replacement | M | — |
| 10 | Dashboard sections backed only by real new data; multi-node simulator, clearly labeled | M | Last, matching the requested priority |
| 11 | Go SDK, benchmarks, fuzz targets, security and performance review | M | — |

## 11. Open decisions requiring maintainer approval

1. **Address-to-key binding.** Existing accounts hold randomly generated 256-bit addresses that cannot be proven to belong to a key. Recommended: derive node addresses as `SHA-256(Ed25519 public key)` inside the unchanged `ipv8:` Base32 text format, and keep existing account addresses as a distinct, explicitly non-routable identifier joined to the key by a signed binding record. The alternative — key-bound addresses for new nodes only, with no migration path — is simpler but leaves two permanent address classes.
2. **Handshake.** Recommended: TLS 1.3 through `crypto/tls`, with ephemeral self-signed certificates whose public-key hash equals the peer's IPv8 address. This yields mutual authentication, AEAD, replay protection, and key updates with no new dependency. A Noise-based handshake would require adding a dependency and a separate justification.
3. **Transport split.** Recommended: UDP for the datagram data plane, since packets are already capped at 1,304 bytes and fit within a typical MTU, with TCP framing as a fallback for large transfers.
4. **Compatibility.** The version 2 packet format has no extension mechanism. Any version 3 work must define explicit coexistence, and the existing dashboard, installer, and agent behavior must remain intact throughout.

## 12. Scope of this audit

No source file was modified to produce this audit. No branch, tag, or commit exists in the upstream repository beyond this documentation change, and nothing was pushed. The address format, README structure, installer behavior, simulator, and agent protocol are unchanged. Implementation of steps 1 and later waits on approval of the decisions in §11.
