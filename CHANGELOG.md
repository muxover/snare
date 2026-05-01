# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/muxover/snare/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/muxover/snare/releases/tag/v1.2.0
[1.1.0]: https://github.com/muxover/snare/releases/tag/v1.1.0
