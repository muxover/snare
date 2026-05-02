# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.6.0] - 2026-05-02

### Added

- `snare tui` — interactive terminal UI (Bubble Tea). Live capture list that polls every 2 s; keyboard navigation (↑↓/jk); full request/response detail view; inline replay; `/` filter by URL or method.
- `snare pipe` — stream captures as newline-delimited JSON (NDJSON). Supports `--follow` to tail new arrivals, and `--method`, `--status`, `--url` filters. Designed for `jq` and shell pipelines.
- `snare assert` — assert conditions on captures and exit 1 if unmet. Flags: `--url`, `--method`, `--status`, `--body`, `--min` (default 1), `--max`. For CI smoke tests and contract checks.
- `snare serve --on-capture <cmd>` — run a shell command for every new capture; the full capture JSON is written to its stdin.
- Release artifacts now include `linux-arm64` in addition to the existing targets (`linux-amd64`, `darwin-amd64`, `darwin-arm64`, `windows-amd64`).

## [1.5.0] - 2026-05-02

### Added

- `snare serve --intercept <pattern>` — pause requests matching a URL pattern before forwarding (use `*` for all).
- `snare intercept list` — list requests currently held by the proxy.
- `snare intercept forward [id]` — release a held request to the origin as-is.
- `snare intercept edit [id]` — open a held request in `$EDITOR`, modify method/headers/body, then forward.
- `snare intercept drop [id]` — return 502 to the client and discard the request.
- `snare serve --intercept-timeout` — auto-drop intercepted requests after this duration (default: 5m).
- `SNARE_INTERCEPT` env var for the intercept queue directory.
- Interception works across plain HTTP, MITM HTTP/1.1, and MITM HTTP/2.

## [1.4.0] - 2026-05-02

### Added

- `snare mock add` — add a mock rule (method, URL substring, status, body, content-type, headers, name).
- `snare mock from [id]` — generate a mock rule from an existing capture's response.
- `snare mock list` — list all active mock rules.
- `snare mock remove [id]` — remove a rule by ID or prefix.
- `snare mock clear` — remove all mock rules.
- `snare serve --mock-file` — load mock rules at startup from a JSON file.
- Mock rules are matched in order; first match wins; matched requests are not forwarded to the origin.
- `SNARE_MOCKS` env var to set the default mock file path.

## [1.3.0] - 2026-05-02

### Added

- `snare serve`: `--upstream-proxy` to chain outbound traffic through another proxy.
- `snare serve`: `--rewrite-host` for outbound host rewrites and `--add-header` / `--remove-header` for outbound header mutation.
- `snare replay`: `--match` to replay all captures whose URL matches a substring.
- `snare import [file.har]`: import HAR entries into the capture store.

### Changed

- Outbound rewrites/header mutation now apply across plain HTTP, MITM HTTP/1.1, and MITM HTTP/2 paths.

### Dependencies

- `golang.org/x/net` bumped from `v0.51.0` to `v0.53.0`.
- `github.com/andybalholm/brotli` bumped from `v1.2.0` to `v1.2.1`.

## [1.2.0] - 2026-05-02

### Added

- `snare watch` — print new captures as they are written (optional `--interval`).
- `snare list` — duration column; filters `--since`, `--until`, `--body` (request/response substring).
- `snare diff` — compare two captures (request line, status, headers, bodies).
- `(*capture.Store).AllFromDisk` — load all capture files (used when list filters need a full scan).

## [1.1.0] - 2026-03-11

First release.

### Added

- `snare serve` — HTTP/HTTPS proxy with optional MITM (HTTP/1.1 and HTTP/2).
- `snare list` — List captures with optional filters (method, status, url, host).
- `snare show` / `snare replay` — Inspect and re-send captured requests.
- `snare save` / `snare export` — Save or export captures to JSON or HAR.
- `snare clear` / `snare delete` — Clear all or delete one capture.
- `snare ca generate` / `snare ca install` — CA certificate for HTTPS MITM.
- Body decompression (gzip, deflate, brotli) for readable captures.
- Config via `SNARE_STORE`, `SNARE_CA` and serve flags (port, bind, max-captures).

[Unreleased]: https://github.com/muxover/snare/compare/v1.6.0...HEAD
[1.6.0]: https://github.com/muxover/snare/releases/tag/v1.6.0
[1.5.0]: https://github.com/muxover/snare/releases/tag/v1.5.0
[1.4.0]: https://github.com/muxover/snare/releases/tag/v1.4.0
[1.3.0]: https://github.com/muxover/snare/releases/tag/v1.3.0
[1.2.0]: https://github.com/muxover/snare/releases/tag/v1.2.0
[1.1.0]: https://github.com/muxover/snare/releases/tag/v1.1.0
