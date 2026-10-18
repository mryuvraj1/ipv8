# Contributing

IPv8 is experimental research software. Keep protocol claims aligned with the implementation and specification; do not describe the current prototype as Internet-routable.

Before opening a pull request:

1. Format Go sources with `gofmt -w cmd internal web`.
2. Run `go vet ./...`, `go test -race ./...`, `go build ./...`, `bash -n install.sh`, `node --check web/app.js`, and `python3 tests/installer_test.py -v`.
3. Add tests for address, packet, API, or UI behavior that changed.
4. Do not commit `.env`, database files, credentials, enrollment tokens, or agent configuration.

Keep changes focused and use the packet/address rules in `SPECIFICATION.md`. See `DEVELOPMENT.md` for the current architecture and security boundaries.
