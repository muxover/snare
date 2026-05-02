# Snare

<div align="center">

[![CI](https://github.com/muxover/snare/actions/workflows/ci.yml/badge.svg)](https://github.com/muxover/snare/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**HTTP/HTTPS proxy CLI that intercepts, captures, and replays traffic.**

</div>

---

Snare is a local HTTP/HTTPS proxy you run on your machine. Point `HTTP_PROXY` and `HTTPS_PROXY` at it to capture every request and response. Inspect, save, export (JSON or HAR), and replay requests from the CLI.

## Features

- HTTP and HTTPS (CONNECT) with optional MITM (HTTP/1.1 and HTTP/2)
- Every request/response saved to disk (JSON per capture)
- List, watch, diff, show, replay, import, save, export, clear, delete from the CLI
- Body decompression (gzip, deflate, brotli) for readable captures
- CA generate and install for trusting the proxy on your system
- Upstream proxy chaining, host rewrite rules, and outbound header rewrite/remove

## Installation

```bash
go install github.com/muxover/snare@latest
```

Or clone and build:

```bash
git clone https://github.com/muxover/snare.git && cd snare && go build -o snare .
```

## Quick Start

1. Start the proxy:

```bash
snare serve
```

2. Set your proxy and run traffic:

```bash
export HTTP_PROXY=http://127.0.0.1:8888 HTTPS_PROXY=http://127.0.0.1:8888
curl -x http://127.0.0.1:8888 http://example.com
```

3. List and inspect captures:

```bash
snare list
snare show <id>
snare replay <id>
```

## Commands

| Command | Description |
|---------|-------------|
| `snare serve` | Start the proxy (default port 8888; supports `--upstream-proxy`, `--rewrite-host`, `--add-header`, `--remove-header`) |
| `snare list` | List recent captures (filters: `--method`, `--status`, `--url`, `--host`, `--since`, `--until`, `--body`; shows duration) |
| `snare watch` | Print new captures as they arrive (`--interval`) |
| `snare show [id]` | Show full request/response for a capture |
| `snare diff [id-a] [id-b]` | Compare two captures |
| `snare replay [id]` | Re-send one capture by ID, or replay all URL matches with `--match` (optional `-u` URL, `-H` headers, `-n` repeat) |
| `snare import [file.har]` | Import HAR entries into the capture store |
| `snare save [id]` | Save one or more captures to a JSON file (`-o`, `--all`) |
| `snare export` | Export last N captures to JSON or HAR (`-f json|har`, `-n`) |
| `snare clear` | Delete all captures from the store |
| `snare delete [id]` | Delete one capture by ID or prefix |
| `snare ca generate` | Generate CA certificate if missing |
| `snare ca install` | Print instructions to install CA in system trust store |

## Configuration

| Env / flag | Description |
|------------|-------------|
| `SNARE_STORE` | Directory for capture files (default: `~/.snare/captures`) |
| `SNARE_CA` | Directory for CA certs (default: `~/.snare`) |
| `--port`, `-p` | Port (default: 8888) |
| `--bind`, `-b` | Bind address (default: 127.0.0.1; use 0.0.0.0 for all interfaces) |
| `--no-mitm` | Disable HTTPS MITM; CONNECT is tunneled only |
| `--max-captures` | Max captures to keep; oldest pruned (default: 1000) |
| `--upstream-proxy` | Forward outbound traffic through another proxy URL |
| `--rewrite-host` | Rewrite outbound host with `from=to` (repeatable) |
| `--add-header` | Add or override outbound header (`Key: Value`, repeatable) |
| `--remove-header` | Remove outbound header by name (repeatable) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Licensed under [MIT](LICENSE).

## Links

- Repository: https://github.com/muxover/snare
- Issues: https://github.com/muxover/snare/issues
- Changelog: [CHANGELOG.md](CHANGELOG.md)

---

<p align="center">Made with ❤️ by Jax (@muxover)</p>
