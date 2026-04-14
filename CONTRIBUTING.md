# Contributing to cephilis

Thank you for taking the time to contribute!

## Development setup

```bash
git clone https://github.com/HudoGriz/cephilis.git
cd cephilis
go mod download
```

Run tests:

```bash
make test
```

Build:

```bash
make build
./bin/cephilis version
```

## Pull requests

1. Fork the repository and create a feature branch from `main`.
2. Add or update tests for every code change.
3. Ensure `make test` and `make vet` pass locally.
4. If `golangci-lint` is installed, run `make lint` before opening the PR.
5. Update `CHANGELOG.md` under `[Unreleased]`.
6. Open a pull request and fill in the PR template.

## Coding style

- Follow standard Go conventions (`gofmt`, `goimports`).
- All exported symbols must have godoc comments.
- Errors must be wrapped with `fmt.Errorf("context: %w", err)` so callers can use `errors.Is`/`errors.As`.
- Never use `_` to discard errors from functions that can fail in production paths.
- Keep packages small and focused; prefer adding to `internal/` over creating new top-level packages.

## Reporting bugs

Please open a GitHub issue using the bug-report template and include:
- `cephilis version` output
- OS / kernel version
- Steps to reproduce

## Security issues

See [SECURITY.md](SECURITY.md).
