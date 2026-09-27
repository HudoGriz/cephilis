# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-09-27

Focus: cephilis is now a CephFS per-directory usage exporter with an optional
D-safe client health check.

### Added
- Site-neutral Grafana dashboard `monitoring/grafana/dashboards/cephilis.json`:
  folder tree with click-through drill-down, per-folder size/files/trend/
  7-day growth, share-of-folder donut, scan status, client health.
- `cephilis_section_scan_ok`, `cephilis_section_failed_dirs`,
  `cephilis_section_size_bytes` and `cephilis_section_files_total`.
- systemd units `cephilis-space.{service,timer}` (hourly) and
  `cephilis-health.{service,timer}` (every minute).
- Alert rules for unhealthy client, stale health metrics (hung client), and
  stale or failed space scans.
- RPM and DEB packages via GoReleaser (binary, units, example config,
  dashboard and rules under `/usr/share/cephilis`).

### Changed
- `space`: a failing section or unreadable subdirectory no longer aborts the
  whole scan; failures are logged and exported. Exit status is non-zero only
  when every section fails, so partial results still reach Prometheus.
- Unknown keys in `mounts.yaml` (`probe`, `timeout`, `legacy` from older
  releases) are ignored, so existing configs keep working.

### Removed
- `mount` sub-command: its probe-file `stat` on the mount blocks in D state
  when CephFS hangs. Use `health`.
- `all` sub-command and the `monitor` package (read the `caps` debugfs file,
  which can deadlock the ceph kernel module under cap pressure, and shelled out
  to `ceph`/`squeue`). Run `space` and `health` from their own units instead.
- `cephilis-metrics.{service,timer}` (ran `all`).
- `du -sb` fallback when CephFS xattrs are missing: walking a multi-PB tree
  takes hours and blocks in D state on a CephFS brownout. A missing xattr is
  now reported as an error.
- Unused `internal/interfaces` package and `IntEnv`/`BoolEnv` helpers.

## [0.1.0]

### Added
- `health --mount <path> --mode slurm|prom|json` for safe Slurm/CephFS mount
  health checks based on mountinfo and kernel Ceph debugfs session state.
- Health test hooks `CEPHILIS_MOUNTINFO_PATH` and
  `CEPHILIS_CEPH_DEBUGFS_GLOB` for non-invasive wrapper/drain testing.
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
- `all --format prom` now uses the safe health path for mount metrics instead
  of probe-file responsiveness checks.
- Mount config no longer requires probe files, per-mount timeouts, or legacy
  probe declarations for `/home`.
- `init` no longer creates probe files under monitored mounts.
- Module path updated from `cephilis` to `github.com/HudoGriz/cephilis`.
- `go.mod` bumped from Go 1.19 to Go 1.26.
- YAML config parsing replaced: hand-rolled regex parser removed in favour of `gopkg.in/yaml.v3`.
- Silent error drops in `runAll` (`cli.go`) replaced with proper error propagation.
- `errors.New` in CLI replaced with `fmt.Errorf` for consistent error wrapping.
- Flag-parse error output restored (previously sent to `io.Discard`).
- Root-not-running warning now uses `log/slog` structured logger.
- Skipped config sections (non-existent paths) now emit a warning to stderr instead of silently disappearing.
