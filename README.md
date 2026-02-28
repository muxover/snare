# Snare

<div align="center">

[![CI](https://github.com/muxover/snare/actions/workflows/ci.yml/badge.svg)](https://github.com/muxover/snare/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/muxover/snare)](https://goreportcard.com/report/github.com/muxover/snare)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**HTTP/HTTPS proxy CLI that intercepts, captures, and replays traffic.**

[Features](#-features) • [Installation](#-installation) • [Quick Start](#-quick-start) • [Commands](#-commands) • [Configuration](#️-configuration) • [HTTPS (MITM)](#-https-mitm) • [Troubleshooting](#-troubleshooting) • [Security](#-security) • [Contributing](#-contributing) • [License](#-license)

</div>

---

Snare is a single-binary CLI for engineers who need to **inspect and replay HTTP(S) traffic**: run a local proxy, point your tools at it, and every request/response is captured to disk. Supports HTTP, HTTPS (with optional MITM), HTTP/1.1, and HTTP/2.

## ✨ Features

- **Intercept & capture** — Every request and response (method, URL, headers, body, status, duration) is saved automatically to JSON files.
- **HTTP & HTTPS** — Plain HTTP is captured as-is; HTTPS can be decrypted via a local CA (MITM) so you see full bodies.
- **HTTP/1.1 & HTTP/2** — Both protocols supported; ALPN and `golang.org/x/net/http2` for H2.
- **Replay** — Re-send any captured request from the CLI (`snare replay <id>`), with optional URL override and repeat count.
- **Export** — Export captures to JSON or HAR for use in other tools (e.g. DevTools, Postman).
- **LAN / external** — Bind to `0.0.0.0` and use your machine's IP so other devices can use the proxy.
- **Production-friendly** — Graceful shutdown (SIGINT/SIGTERM), timeouts, structured logging (slog), port validation, `--verbose` for debug.

## 📦 Installation

**Requires Go 1.25+.**

```bash
git clone https://github.com/muxover/snare.git
cd snare
go build -o snare .
```

On Windows the binary is `snare.exe`. Move it to a directory on your `PATH`, or run from the project directory.

**Install to `$GOPATH/bin`:**

```bash
go install github.com/muxover/snare@latest
# Binary: ~/go/bin/snare (Windows: %USERPROFILE%\go\bin\snare.exe)
```

**Version:**

```bash
snare --version
# snare version 0.1.0
```

## 🚀 Quick Start

**1. Start Snare**

```bash
./snare serve
# Or: ./snare serve --bind 0.0.0.0 --port 8888
```

You'll see the proxy URL (e.g. `http://127.0.0.1:8888`) and where captures are saved (default: `~/.snare/captures`).

**2. Send traffic through it**

```bash
export HTTP_PROXY=http://127.0.0.1:8888
export HTTPS_PROXY=http://127.0.0.1:8888
curl -x http://127.0.0.1:8888 http://example.com
```

Or set your browser/app to use manual proxy `http://127.0.0.1:8888`.

**3. List and inspect**

```bash
./snare list
./snare show <id>      # id = full UUID or first 8 chars
./snare replay <id>
```

## 📋 Commands

| Command | Description |
|--------|-------------|
| `snare serve` | Start the proxy. Options: `--port`, `--bind`, `--store-dir`, `--no-mitm`, `--verbose`. |
| `snare list [-n 20]` | List last N captures (default 20). Reads from store dir. |
| `snare show <id>` | Print full request/response. `<id>` can be a prefix. |
| `snare replay <id> [-n 1] [-u URL]` | Re-send the request. `-n` repeat, `-u` override URL. |
| `snare save <id> [-o file]` | Save one capture to JSON. |
| `snare save --all [-n 10] [-o file]` | Save last N captures to one file. |
| `snare export [-f json\|har] [-n 50]` | Export to `export.json` or `export.har`. |
| `snare clear` | Delete all captures in the store directory. |
| `snare ca generate` | Create CA cert if missing. |
| `snare ca install` | Print instructions to install CA (for HTTPS MITM). |
| `snare --version` | Print version. |

### Examples

```bash
# Serve on all interfaces, verbose logs
./snare serve --bind 0.0.0.0 -v

# List last 50 captures
./snare list -n 50

# Show capture by prefix
./snare show a1b2c3d4

# Replay 3 times with a different URL
./snare replay a1b2c3d4 -n 3 -u https://api.example.com/v2/echo

# Save one capture to a custom file
./snare save a1b2c3d4 -o request.json

# Save last 20 captures to one file
./snare save --all -n 20 -o backup.json

# Export to HAR for browser DevTools
./snare export -f har -n 100
```

## ⚙️ Configuration

### Serve flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8888` | Port (1–65535). |
| `--bind` | `-b` | `127.0.0.1` | Bind address. Use `0.0.0.0` for all interfaces. |
| `--store-dir` | | (env) | Directory for capture files. |
| `--no-mitm` | | `false` | Tunnel HTTPS only; no decryption. |
| `--verbose` | `-v` | `false` | Debug logging (e.g. per-connection). |

### Environment variables

| Variable | Description |
|----------|-------------|
| `SNARE_STORE` | Capture directory. Default: `~/.snare/captures` (Windows: `%USERPROFILE%\.snare\captures`). |
| `SNARE_CA` | CA cert directory. Default: `~/.snare`. |

### Logging

- **Info** (default): request method/host/path, captured request (method, URL, status).
- **Debug** (`--verbose`): per-connection logs.

## 🔐 HTTPS (MITM)

To decrypt HTTPS and capture bodies:

1. **Generate and install the Snare CA** (once per machine):

   ```bash
   ./snare ca generate
   ./snare ca install
   ```

2. Follow the printed steps to add the CA to your system or browser (e.g. Windows: install `ca.pem` as Trusted Root CA).

3. Start Snare (MITM is on by default). HTTPS through the proxy is decrypted, captured, and re-encrypted to the origin.

To avoid trusting the CA, use `--no-mitm`: HTTPS is tunneled without capture.

## 🌐 Using the proxy from other machines

1. Start with:

   ```bash
   ./snare serve --bind 0.0.0.0 --port 8888
   ```

2. Snare prints URLs like `http://192.168.1.7:8888`. On the other device, set the proxy to that URL.

3. Allow the proxy port through the firewall on the host.

**Security:** `0.0.0.0` makes the proxy reachable from the network. Use only in trusted environments.

## 🔧 Troubleshooting

**No captures when running `snare list`**

- Captures are written only when **traffic goes through the proxy**. If you never see `request` / `captured` in the serve terminal, the client is not using this proxy.
- **Check:** From the same machine run `curl -x http://127.0.0.1:8888 http://example.com`. You should see `request` and `captured` in the Snare output. Then run `snare list`.
- **Store path:** `list` reads from the same directory as `serve`. If you use `--store-dir` with `serve`, set `SNARE_STORE` to the same path so both use the same directory.

**HTTPS sites show certificate errors**

- For MITM you must install and trust the Snare CA (`snare ca install`). Until then, browsers will show untrusted certificate. Use `--no-mitm` to tunnel HTTPS without decrypting.

**Port already in use**

- Use another port: `./snare serve -p 9999`. Port must be 1–65535.

## 🔒 Security

- **CA and keys** are stored under `~/.snare` (or `SNARE_CA`). Protect this directory; anyone with the CA key can issue certs for your MITM.
- **Binding to `0.0.0.0`** exposes the proxy on the network. Use only in trusted LANs or with access controls.
- **Captured data** (headers, bodies) may contain secrets. Restrict access to the store directory and don't commit it to version control.

## 🏗️ Project layout

```
.
├── main.go            # Entrypoint; exits with 1 on error
├── cmd/               # CLI: serve, list, show, replay, save, export, clear, ca
├── proxy/             # Server, handler, MITM (H1/H2), transport, certs
├── capture/           # Capture types and store (memory + disk)
├── config/            # Store dir and CA dir (env + defaults)
├── release-notes/     # Per-version release notes
├── go.mod
├── README.md
├── LICENSE
├── CHANGELOG.md
└── CONTRIBUTING.md
```

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Open an [issue](https://github.com/muxover/snare/issues) or [pull request](https://github.com/muxover/snare/pulls) on GitHub.

## 📄 License

Licensed under the MIT License ([LICENSE](LICENSE)).

## 🔗 Links

- **Repository**: https://github.com/muxover/snare
- **Issues**: https://github.com/muxover/snare/issues
- **Changelog**: [CHANGELOG.md](CHANGELOG.md)

---

<div align="center">

Made with ❤️ by Jax (@muxover)

</div>
