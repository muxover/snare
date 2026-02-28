# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-02-28

### Added

- HTTP/HTTPS proxy with CONNECT support.
- HTTPS MITM with dynamic per-host certificates (CA generate/install).
- HTTP/1.1 and HTTP/2 support.
- Automatic capture of every request/response (method, URL, headers, body, status, duration).
- Persistence: each capture saved as JSON to configurable directory.
- CLI commands: serve, list, show, replay, save, export, clear, ca generate, ca install.
- Bind to all interfaces (`--bind 0.0.0.0`) for LAN/external clients.
- Graceful shutdown on SIGINT/SIGTERM.
- Export to HAR or JSON.
- Go module: `github.com/muxover/snare`; install with `go install github.com/muxover/snare@latest`.
- Default paths: `~/.snare/captures` (store), `~/.snare` (CA). Environment: `SNARE_STORE`, `SNARE_CA`.
- `snare --version` prints version.
- `serve --verbose` / `-v` for debug logging.
- Port validation for `serve` (1–65535).
- Graceful CLI exit: errors print to stderr and exit with code 1.
- Long descriptions for all commands (`snare <cmd> --help`).

### Security

- CA and per-host certs for HTTPS interception; user must trust CA for MITM.

[Unreleased]: https://github.com/muxover/snare/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/muxover/snare/releases/tag/v0.1.0
