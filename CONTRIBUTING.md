# Contributing to Snare

Thank you for your interest in contributing!

---

## Table of Contents

- [Getting started](#getting-started)
- [Running tests](#running-tests)
- [Code style](#code-style)
- [Submitting changes](#submitting-changes)
- [Reporting issues](#reporting-issues)

---

## Getting started

**Requirements:**
- Go 1.22+

```bash
git clone https://github.com/muxover/snare.git
cd snare
go build .
```

## Running tests

```bash
go test -v -race ./...
```

## Code style

- Run `go fmt ./...` before committing.
- Run `go vet ./...` — CI will reject code with vet warnings.
- Prefer clear names and short functions; add comments for non-obvious behavior.
- Error handling: return errors from commands; use `RunE` in Cobra so the CLI exits with a non-zero code.

## Submitting changes

1. **Open an issue first** for significant changes.
2. Fork and branch from `main`.
3. Add/update tests.
4. Ensure CI checks pass.
5. Open a PR with a clear description.

One logical change per PR.

## Reporting issues

Open an issue at https://github.com/muxover/snare/issues. Include:
- Go version
- OS and architecture
- Minimal reproducer
- Full error message

## License

By contributing, you agree that your contributions will be licensed under the same [MIT License](LICENSE) that covers this project.
