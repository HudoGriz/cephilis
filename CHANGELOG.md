# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-09-27

### Added
- `paths:` config: report a directory plus `depth` levels of subdirectories;
  overlapping paths are reported once. `max_dirs` caps the directories per path
  (keeping the largest), `max_entries` refuses to list huge directories, and a
  directory is only listed when `ceph.dir.subdirs` says it has subdirectories.
- `cephilis_dir_rctime_seconds` (latest change below a directory) for finding
  cold data.
- CephFS quotas: `cephilis_dir_quota_bytes` / `cephilis_dir_quota_files`, read
  from the combined `ceph.quota` xattr; `skip_quotas` to save the read.
- `owner: true` per path adds the owner's user name (resolved through
  `getent`, so LDAP/SSSD users work in the static binary).
- `cephilis serve`: HTTP exporter on `:9966` with periodic scans in a child
  process; a scan blocked on CephFS is reported, never piled up.
  `cephilis-serve.service` unit.
- `cephilis_scan_xattr_reads` (= MDS requests), `cephilis_scan_duration_seconds`,
  `cephilis_scan_timestamp_seconds`, `cephilis_root_{scan_ok,dirs,truncated,failed_dirs}`.
- Dashboard: folder tree of every directory with reported subdirectories,
  idle-time, quota, by-owner and scan-cost panels.
- Alert rules for stale/stuck scans, failed paths, and quotas above 90 %.
- Container image `ghcr.io/hudogriz/cephilis`, Ansible and Kubernetes examples.

### Changed
- **Metric names** (breaking, pre-1.0): `cephilis_dir_size_bytes` →
  `cephilis_dir_bytes`, `cephilis_dir_files_total` → `cephilis_dir_files`,
  labels `section`/`parent`/`name` → `path`/`parent`; `cephilis_section_*` →
  `cephilis_root_*`. `sections:` configs still load as depth-1 paths.
- Fewer MDS round trips: measured one `getattr` per `ceph.*` xattr read, so
  leaves no longer read `subdirs`/`entries` and quotas use one combined read
  (7 → 4 reads per directory, 3 with `skip_quotas`).
- The space timer runs every 15 minutes (was hourly).
- Table output shows each directory's subdirectories with share, files, last
  change and quota usage.

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
