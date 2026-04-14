# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `version` sub-command and `--version` flag; version string injected via `ldflags` at build time.
- `Collector` and `Prober` interfaces in `internal/interfaces` for decoupled testing.
- Godoc comments on all exported symbols; package-level documentation in each `internal/` package.
- Expanded test suite: config (YAML parsing, env helpers, error paths), collector (listSubdirs, du fallback), mount (Check with non-existent paths), format (table, CSV, TSV, JSON, Prometheus, compat mode).
- `Makefile` with `build`, `test`, `cover`, `lint`, `vet`, `fmt`, `tidy`, `install`, `clean` targets.
- `.golangci.yml` linting configuration (`errcheck`, `govet`, `staticcheck`, `revive`, `gosec`, …).
- GitHub Actions CI workflow: test matrix (Go 1.22/1.23), lint, build + smoke test.
- GitHub Actions release workflow using GoReleaser for tagged releases.
- `.goreleaser.yaml` producing `linux/amd64` and `linux/arm64` static binaries with checksums.
- `Dockerfile` (multi-stage scratch image) for binary distribution.
- `LICENSE` (MIT), `.gitignore`, `CONTRIBUTING.md`, `SECURITY.md`, issue and PR templates.

### Changed
- Module path updated from `cephilis` to `github.com/HudoGriz/cephilis`.
- `go.mod` bumped from Go 1.19 to Go 1.22.
- YAML config parsing replaced: hand-rolled regex parser removed in favour of `gopkg.in/yaml.v3`.
- Silent error drops in `runAll` (`cli.go`) replaced with proper error propagation.
- `errors.New` in CLI replaced with `fmt.Errorf` for consistent error wrapping.
- Flag-parse error output restored (previously sent to `io.Discard`).
- Root-not-running warning now uses `log/slog` structured logger.
- Skipped config sections (non-existent paths) now emit a warning to stderr instead of silently disappearing.

