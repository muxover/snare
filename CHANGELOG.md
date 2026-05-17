# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.0] - 2026-05-20

### Added

- `snare serve --web` — start a browser dashboard (default port 8080) alongside the proxy; the dashboard URL opens automatically in the default browser on startup.
- `snare serve --web-port <port>` — choose the dashboard port (default: 8080).
- `snare serve --browser` — when combined with `--web`, opens the proxy-configured browser at the dashboard URL instead of `about:blank`.
- `snare serve` auto-creates a named run session (`run-YYYY-MM-DDTHH:MM:SS`) on startup and closes it on shutdown; every proxy run is automatically its own session.
- `snare serve` pre-loads existing captures from disk into memory on startup; the web dashboard shows prior captures immediately without a full reload.
- Web dashboard Mocks tab: add rules, view active rules, remove rules, and generate a mock from any captured response.
- Web dashboard Intercept tab: view paused requests; forward, drop, or edit-and-forward each one from the browser.
- Web dashboard Sessions tab: start, end, delete, and compare sessions from the browser.
- Web dashboard capture diff: pin any capture and diff it against any other.
- Web dashboard export: JSON, HAR, Postman collection, and OpenAPI 3.0 spec from the Export dropdown.
- Web dashboard CA Cert button: download the CA certificate directly from the browser; shows per-device LAN URLs and installation instructions for iOS and Android.
- Web dashboard SSE clear and delete events: all open browser tabs stay in sync when captures are deleted or cleared from any tab.
- `snare tui` rebuilt with four full management tabs (press 1–4 to switch):
  - **Captures** — browse, filter (`/`), inspect, replay (`r`), edit & replay (`e`), mock from capture (`m`), delete (`d`), clear all (`C`), diff two captures (space + `D`).
  - **Mocks** — view rules, add (`a`), delete (`d`), clear all (`C`).
  - **Intercept** — view pending, forward (`f`), drop (`x`), edit & forward (`e`).
  - **Sessions** — start (`s`), end (`e`), delete (`x`), diff (space + `D`).
- `snare tui --proxy <url>` — route TUI replays through a proxy so they are captured (default: `http://127.0.0.1:8888`).
- `snare replay` now routes through the snare proxy by default (`--proxy http://127.0.0.1:8888`) so every replay is captured and inspectable; override with `--proxy ""` to bypass.
- `snare session delete <name>` — delete a named session.
- `snare serve --shadow <url>` — silently mirror every proxied request to a second target; the client response is unaffected. Repeatable.
- `snare serve --plugin <cmd>` — run a plugin command for every capture; the full capture JSON is written to its stdin. Repeatable. Distinct from `--on-capture`: multiple plugins can be registered independently.
- `snare session start <name>` — mark the start of a named capture session.
- `snare session end <name>` — mark the end of a named capture session.
- `snare session list` — list all recorded sessions.
- `snare session diff <a> <b>` — compare the capture sequences from two sessions: shows mismatched methods, paths, and status codes by position.
- gRPC capture: requests with `Content-Type: application/grpc*` are parsed into length-prefixed frames stored under `grpc.frames` in the capture JSON. The service method is recorded under `grpc.method`. Frames are shown in the TUI detail view, `snare show`, and the browser dashboard.
- `~/.snare/config.yaml` — config file; all `snare serve` flags can be set here. CLI flags override config values. Suppress with `--no-config`.

### Fixed

- Mock rules added or removed from the CLI or TUI now take effect immediately in the running proxy without a restart; the mock store reloads from disk on every request match.

## [1.9.0] - 2026-05-15

### Added

- `snare serve --delay <duration>` — inject artificial latency before every response; useful for simulating slow networks.
- `snare serve --chaos <pct>` — randomly drop a percentage of requests with 503; useful for resilience testing.
- `snare serve --browser` — auto-launch Chrome, Edge, or Chromium with `--proxy-server=` pointing at the running Snare proxy.
- `snare ca install --device android` — push CA certificate to a connected Android device via ADB and install it in the user trust store.
- `snare ca install --device ios` — serve CA certificate over a temporary local HTTP server so Safari can download and install it directly.
- `snare curl <id>` — print a capture as a ready-to-run `curl` command with all headers and body included.
- `snare export --format postman` — export captures as a Postman Collection v2.1.0 JSON file.
- `snare openapi` — analyse captures and generate an OpenAPI 3.0.3 JSON spec; numeric and UUID path segments are parameterised as `{id}` automatically.
- `snare record` — start the proxy and write all captures to a cassette file (NDJSON) for offline playback.
- `snare playback <cassette>` — replay a cassette file: serves an HTTP server that matches requests by method and path and returns the recorded responses.

## [1.8.0] - 2026-05-14

### Added

- `snare serve --mode reverse --target <url>` — reverse proxy mode; Snare sits in front of a backend and captures all traffic without requiring `HTTP_PROXY` to be set.
- `snare serve --rewrite-body <regex=replacement>` — rewrite response bodies with a regular expression before capturing and forwarding.
- `snare serve --ignore <substring>` — skip capturing requests whose URL contains this substring; applied at the CONNECT (host) and per-request levels. Repeatable.
- `snare serve --map-remote <host=http://target>` — redirect outbound requests for a given host to a different base URL. Repeatable.
- `snare serve --no-store` — disable disk persistence; captures are held in memory only and not written to `SNARE_STORE`.
- `snare serve --max-body-size <bytes>` — truncate captured request and response bodies at this byte limit (0 = no limit).
- `snare grep <pattern>` — regex search across request and response bodies in all captures; supports `--invert`, `--method`, `--host`.
- `snare replay --edit` — open the capture JSON in `$EDITOR` before resending; edited method, URL, headers, and body are used for the request.
- `snare clear --method/--status/--url/--host` — selective delete; without filters clears all captures (previous behaviour).
- `snare watch --method/--status/--url/--host/--body` — same filters as `snare list`, applied per poll tick.
- `snare ca install` now executes the system trust-store command directly (`certutil` on Windows, `security` on macOS, `update-ca-certificates` on Linux) instead of just printing instructions.
- Protocol column in `snare list` and `snare watch` output: `h1`, `h2`, or `ws`, coloured with lipgloss.
- Colourised status codes in `snare list` and `snare watch`: 2xx green, 3xx yellow, 4xx red, 5xx magenta.
- WebSocket frame list in `snare tui` detail view — rendered after the response section with per-frame direction arrow, timestamp, opcode, and payload preview.

## [1.7.0] - 2026-05-13

### Added

- WebSocket capture for HTTPS MITM: the HTTP upgrade handshake (HTTP/1.1 `101` or HTTP/2 extended CONNECT with `200`) is stored like a normal capture; after the connection closes, per-frame records (`c2s` / `s2c`, opcode, payload, timestamp) are attached under `websocket.frames` in the capture JSON.
- MITM **HTTP/1.1** path: `101 Switching Protocols`, RFC6455 frames on the wire (masked client → origin).
- MITM **HTTP/2** path (RFC 8441): `CONNECT` with `:protocol` websocket; client stream uses unmasked RFC6455 payloads in HTTP/2 DATA; Snare completes the origin leg with an HTTP/1.1 WebSocket upgrade on a separate TLS connection, then relays frames both ways.
- **Cleartext proxy** (`serveHTTP`, absolute `http://` / `https://` URLs): WebSocket upgrades use a direct dial to the origin plus `Hijack` on the client connection so the tunnel is not returned to the transport pool before relaying.
- `snare show` prints WebSocket frames after the response section when present.
- HAR export adds a `_webSocketMessages` array on entries that have frames (Chrome-compatible shape: `type`, `time`, `opcode`, `data`).

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

[Unreleased]: https://github.com/muxover/snare/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/muxover/snare/releases/tag/v2.0.0
[1.9.0]: https://github.com/muxover/snare/releases/tag/v1.9.0
[1.8.0]: https://github.com/muxover/snare/releases/tag/v1.8.0
[1.7.0]: https://github.com/muxover/snare/releases/tag/v1.7.0
[1.6.0]: https://github.com/muxover/snare/releases/tag/v1.6.0
[1.5.0]: https://github.com/muxover/snare/releases/tag/v1.5.0
[1.4.0]: https://github.com/muxover/snare/releases/tag/v1.4.0
[1.3.0]: https://github.com/muxover/snare/releases/tag/v1.3.0
[1.2.0]: https://github.com/muxover/snare/releases/tag/v1.2.0
[1.1.0]: https://github.com/muxover/snare/releases/tag/v1.1.0
