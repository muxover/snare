# Snare

<div align="center">

[![CI](https://github.com/muxover/snare/actions/workflows/ci.yml/badge.svg)](https://github.com/muxover/snare/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/muxover/snare/v2.svg)](https://pkg.go.dev/github.com/muxover/snare/v2)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/muxover/snare)](https://github.com/muxover/snare/releases/latest)

**Local HTTP/HTTPS proxy — capture, inspect, replay, and mock traffic.**

</div>

---

Snare runs as a local proxy and records every HTTP and HTTPS request that passes through it. Each capture is a plain JSON file you can list, filter, search, diff, mock, intercept, and replay with a single command. Forward and reverse proxy in one binary.

---

## Installation

```bash
go install github.com/muxover/snare/v2@latest
```

Build from source:

```bash
git clone https://github.com/muxover/snare.git
cd snare
go build -o snare .
```

---

## Quick Start

```bash
snare serve

export HTTP_PROXY=http://127.0.0.1:8888
export HTTPS_PROXY=http://127.0.0.1:8888

curl https://httpbin.org/get

snare list
snare show <id>
snare replay <id>
```

---

## HTTPS MITM

```bash
snare ca generate   # writes ~/.snare/ca.pem
snare ca install    # installs CA into system trust store
snare serve
```

`snare ca install` runs the right command per platform: `certutil -addstore Root` on Windows, `security add-trusted-cert` on macOS, `update-ca-certificates` on Linux.

---

## Reverse Proxy

```bash
snare serve --mode reverse --target http://localhost:3000
```

No proxy env vars needed. All traffic to `127.0.0.1:8888` is forwarded to the target and captured.

---

## Commands

**Captures**

| Command | Description |
|---------|-------------|
| `snare list` | List captures with filters and colorized output |
| `snare watch` | Tail new captures as they arrive |
| `snare show <id>` | Full request/response detail; WebSocket frames, SSE frames, GraphQL fields, and decoded gRPC when present |
| `snare diff <a> <b>` | Diff two captures |
| `snare grep <pattern>` | Regex search across all capture bodies |
| `snare clear` | Delete captures (all, or filtered by method/status/url/host) |
| `snare delete <id>` | Delete a single capture |

**Replay**

| Command | Description |
|---------|-------------|
| `snare replay <id>` | Re-send a captured request through the snare proxy (captured by default) |
| `snare replay --match <str>` | Re-send all captures whose URL contains this string |
| `snare replay --edit` | Open capture in `$EDITOR` before sending |

**Mock**

| Command | Description |
|---------|-------------|
| `snare mock add` | Add a stub rule |
| `snare mock from <id>` | Generate a stub from a capture |
| `snare mock list` | List all stubs |
| `snare mock remove <id>` | Remove a stub |
| `snare mock clear` | Remove all stubs |

**Intercept**

| Command | Description |
|---------|-------------|
| `snare intercept list` | List requests paused by the proxy |
| `snare intercept forward <id>` | Release a paused request |
| `snare intercept edit <id>` | Edit then forward a paused request |
| `snare intercept drop <id>` | Drop a paused request (client receives 502) |

**Import / Export**

| Command | Description |
|---------|-------------|
| `snare import <file.har>` | Import a HAR file |
| `snare save <id>` | Save a capture to a file |
| `snare export` | Export captures to JSON, HAR, Postman collection, or OpenAPI spec |
| `snare curl <id>` | Print a capture as a `curl` command |

**OpenAPI**

| Command | Description |
|---------|-------------|
| `snare openapi` | Generate an OpenAPI 3.0 spec from captured traffic |

**Sessions**

| Command | Description |
|---------|-------------|
| `snare session start <name>` | Mark the start of a named capture session |
| `snare session end <name>` | Mark the end of a named capture session |
| `snare session list` | List all recorded sessions |
| `snare session delete <name>` | Delete a named session |
| `snare session diff <a> <b>` | Compare two sessions' capture sequences |

**Record / Playback**

| Command | Description |
|---------|-------------|
| `snare record` | Record traffic to a cassette file for offline playback |
| `snare playback <cassette>` | Replay a cassette file as an HTTP server |

**Automation**

| Command | Description |
|---------|-------------|
| `snare pipe` | Stream captures as NDJSON; `--follow` to tail |
| `snare assert` | Assert conditions on captures; exits 1 on failure |
| `snare tui` | Interactive terminal UI — 4 tabs: Captures, Mocks, Intercept, Sessions |

**CA**

| Command | Description |
|---------|-------------|
| `snare ca generate` | Generate CA certificate |
| `snare ca install` | Install CA into system trust store |
| `snare ca install --device android` | Push CA to Android device via ADB |
| `snare ca install --device ios` | Serve CA for Safari download on iOS |

---

## serve Flags

```
-p, --port              Port (default: 8888)
-b, --bind              Bind address (default: 127.0.0.1)
    --mode              forward (default) or reverse
    --target            Reverse proxy target URL
    --no-mitm           Tunnel CONNECT without MITM
    --max-captures      In-memory cap, oldest pruned (default: 1000)
    --no-store          Memory only, nothing written to disk
    --max-body-size     Truncate bodies at N bytes (0 = no limit)
    --store-dir         Override capture directory
    --upstream-proxy    Chain through another proxy
    --rewrite-host      Rewrite outbound host: from=to (repeatable)
    --add-header        Add or override outbound header: Key: Value (repeatable)
    --remove-header     Remove outbound header by name (repeatable)
    --ignore            Skip URLs containing this substring (repeatable)
    --map-remote        Redirect host: host=http://target (repeatable)
    --rewrite-body      Rewrite response bodies: regex=replacement (repeatable)
    --mock-file         Load mock rules from a file
    --intercept         Pause requests matching this URL pattern (* for all)
    --intercept-timeout Auto-drop paused requests after this duration (default: 5m)
    --on-capture        Shell command run per capture; full JSON piped to stdin
    --delay             Inject artificial latency before each response (e.g. 200ms)
    --chaos             Drop this percentage of requests randomly (e.g. 10)
    --browser           Auto-launch Chrome/Edge with proxy configured
    --shadow            Mirror traffic to a second URL silently (repeatable)
    --plugin            Run plugin command per capture; JSON on stdin (repeatable)
    --web               Start web dashboard
    --web-port          Port for web dashboard (default: 8080)
    --no-config         Ignore ~/.snare/config.yaml
    --proto             Protobuf definition file for gRPC decoding (repeatable)
    --no-h3             Disable HTTP/3 QUIC server in reverse proxy mode
    --hook              JS hook file; onRequest/onResponse/onCapture called per request; reloaded from disk on every request (repeatable)
-v, --verbose           Debug logging
```

---

## JS Hooks

`snare serve --hook hook.js` loads a JavaScript file and calls its exported functions for every request that passes through the proxy. The file is reloaded from disk on every request — edit it and the next request picks up your changes without restarting.

**Available functions:**

```js
// Called before the request is forwarded. Mutate r to change what goes out.
// Return an object to short-circuit — the origin is never contacted.
function onRequest(r) {
  r.method            // string — change the HTTP method
  r.url               // string — change the target URL
  r.headers["X-Foo"] = "bar"   // add/override a header
  delete r.headers["X-Remove"] // remove a header
  r.body              // string — change the request body

  // Short-circuit: answer without hitting the origin
  return { status: 200, headers: { "Content-Type": "application/json" }, body: '{"mocked":true}' }
}

// Called after the response is received. Mutate res to change what the client sees.
function onResponse(r, res) {
  res.status           // number — change the status code
  res.headers["X-Injected"] = "yes"
  res.body = res.body.replace("staging", "production")
}

// Called after the capture is saved. Read-only view of the full capture JSON.
function onCapture(c) {
  if (c.response && c.response.status_code >= 500) {
    console.log("5xx:", c.request.method, c.request.url)
  }
}
```

`console.log()` writes to snare's verbose log (`-v`). Hook errors are logged and skipped — a broken hook never crashes the proxy. Pass `--hook` multiple times to load multiple files; all run in order.

---

## Config file

Snare reads `~/.snare/config.yaml` on startup. All `snare serve` flags are supported:

```yaml
port: "8888"
bind: "127.0.0.1"
web: true
web_port: "8080"
ignore:
  - /healthz
  - /metrics
shadow:
  - http://staging.internal
plugins:
  - "jq . >> ~/snare-log.ndjson"
```

---

## list / watch Flags

```
--method      HTTP method
--status      Response status code
--url         URL substring
--host        Host
--body        Substring in request or response body
--operation   GraphQL operation name
--since       Start timestamp (RFC3339)
--until       End timestamp (RFC3339)
-n, --last    Max results (list only)
--interval    Poll interval (watch only, default: 500ms)
```

---

## replay Flags

```
-n, --repeat  Send N times
-u, --url     Override URL
-H, --header  Add or override header (repeatable)
    --match   Replay all captures matching this URL substring
    --edit    Open capture in $EDITOR before sending
    --proxy   Proxy URL to route replay through (default: http://127.0.0.1:8888; set to empty to bypass)
```

---

## clear Flags

```
--method  Delete only captures with this method
--status  Delete only captures with this status code
--url     Delete only captures whose URL contains this substring
--host    Delete only captures for this host
```

---

## grep Flags

```
<pattern>    Regular expression matched against request and response bodies
-v, --invert Print captures that do NOT match
--method     Limit to this HTTP method
--host       Limit to this host
```

---

## export Flags

```
-f, --format  Output format: json (default), har, postman
-n, --last    Number of captures to export (default: 50)
```

---

## openapi Flags

```
-o, --out     Output file (default: openapi.json)
    --title   API title (default: "snare captured API")
    --server  Override server URL (default: inferred from captures)
```

---

## record Flags

```
-o, --out     Cassette output file (default: cassette.json)
-p, --port    Port to listen on (default: 8888)
-b, --bind    Bind address (default: 127.0.0.1)
    --mode    forward (default) or reverse
    --target  Reverse proxy target URL (required for --mode reverse)
    --no-mitm Disable HTTPS MITM
-v, --verbose Debug logging
```

---

## playback Flags

```
-p, --port  Port to listen on (default: 8888)
-b, --bind  Bind address (default: 127.0.0.1)
```

---

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `SNARE_STORE` | `~/.snare/captures` | Capture directory |
| `SNARE_CA` | `~/.snare` | CA certificate directory |
| `SNARE_MOCKS` | `~/.snare/mocks.json` | Mock rules file |
| `SNARE_INTERCEPT` | `~/.snare/intercept` | Intercept queue directory |

---

## Examples

```bash
# Filter captures
snare list --method POST --status 500

# Search bodies
snare grep '"error"'
snare grep --invert '"success"'

# Watch live traffic for one host
snare watch --host api.example.com

# Mock an endpoint
snare mock add --url /api/payment --status 200 --body '{"ok":true}'

# Intercept and edit a request before it goes out
snare serve --intercept '*'
snare intercept list
snare intercept edit <id>
snare intercept forward <id>

# Stream to jq
snare pipe --follow | jq '.request.url'

# CI smoke test
snare assert --url /api/health --status 200 --min 1

# Ignore health checks
snare serve --ignore /healthz --ignore /metrics

# Redirect a host to a local server
snare serve --map-remote api.example.com=http://localhost:4000

# Reverse proxy with body rewrite
snare serve --mode reverse --target http://localhost:3000 \
  --rewrite-body 'staging.internal=production.example.com'

# Simulate latency and random failures
snare serve --delay 200ms --chaos 15

# Launch browser with proxy pre-configured
snare serve --browser

# Print a capture as curl
snare curl <id>

# Export as Postman collection
snare export --format postman

# Generate OpenAPI spec from captured traffic
snare openapi --out api.json

# Record then replay offline
snare record --out cassette.json
snare playback cassette.json

# Install CA on mobile
snare ca install --device android
snare ca install --device ios

# Shadow traffic to a staging server
snare serve --shadow http://staging.internal

# Plugin: log all captures as NDJSON
snare serve --plugin "jq -c . >> ~/captures.ndjson"

# Web dashboard
snare serve --web

# Decode gRPC traffic with a .proto file
snare serve --proto path/to/service.proto

# Filter captures by GraphQL operation name
snare list --operation GetUser
snare watch --operation CreateOrder

# Reverse proxy with HTTP/3 disabled
snare serve --mode reverse --target http://localhost:3000 --no-h3

# JS hook: inject a header and log all 5xx
snare serve --hook hook.js

# Diff two test runs
snare session start baseline
# ... run tests ...
snare session end baseline
snare session start after-deploy
# ... run tests ...
snare session end after-deploy
snare session diff baseline after-deploy
```

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

Licensed under the [MIT](LICENSE) license.

---

## Links

- Repository: https://github.com/muxover/snare
- Issues: https://github.com/muxover/snare/issues
- Changelog: [CHANGELOG.md](CHANGELOG.md)
- Go Reference: https://pkg.go.dev/github.com/muxover/snare/v2

---

<p align="center">Made with ❤️ by Jax (@muxover)</p>
