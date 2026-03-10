# Contributing

## Getting started

Clone the repo and build:

```bash
git clone https://github.com/muxover/snare.git
cd snare
go build .
```

## Running tests

```bash
go test ./...
```

## Code style

- Use `gofmt` (or your editor’s Go formatter).
- Keep changes focused; one logical change per PR.

## Submitting changes

1. Open an issue to discuss if the change is large.
2. Branch from `main` (e.g. `feat/your-feature` or `fix/your-fix`).
3. Make your changes and ensure the project builds: `go build .`
4. Open a pull request with a clear description and reference any issue.

## Reporting issues

Include:

- Snare version (`snare version` or tag)
- OS and version
- Steps to reproduce
- Expected vs actual behavior
- Any error output or logs
