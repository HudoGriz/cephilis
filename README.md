# cephilis

[![CI](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml/badge.svg)](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Per-directory CephFS usage for Prometheus and Grafana — without walking the tree.**

CephFS already tracks the recursive size and file count of every directory
(`ceph.dir.rbytes`, `ceph.dir.rfiles`). cephilis reads those extended
attributes for the folders you care about and exports them as Prometheus
metrics, so you get per-project and per-user usage, growth trends and a
drill-down folder tree in Grafana.

- **Fast and safe:** one `getxattr` per directory. A scan of ~170 folders over
  660 TiB / 34 M files takes ~50 ms. `du`, `ncdu` or Robinhood would walk every
  file for hours.
- **Folder tree dashboard:** click a folder to see its children, their size,
  file count, trend and 7-day growth.
- **D-safe client health (optional):** `cephilis health` reports whether the
  local CephFS kernel client is mounted and has a healthy MDS session, reading
  only `/proc/self/mountinfo` and the ceph debugfs — it never touches the
  mount, so it keeps answering while CephFS is hung. Suitable for Slurm
  `HealthCheckProgram` / `Prolog`.
- Single static binary, no dependencies; RPM/DEB packages and systemd units.

## Install

Download a package or tarball from
[Releases](https://github.com/HudoGriz/cephilis/releases) (`linux/amd64`,
`linux/arm64`), or build from source:

```bash
git clone https://github.com/HudoGriz/cephilis.git && cd cephilis
make build && sudo make install
```

cephilis writes node_exporter
[textfile collector](https://github.com/prometheus/node_exporter#textfile-collector)
files to `/var/lib/node_exporter/textfile_collector`; point node_exporter at
that directory (`--collector.textfile.directory`).

## Usage

```bash
cephilis space --format table            # human-readable report
cephilis space --format prom             # Prometheus text format (also csv, tsv, json)
cephilis health --mode prom              # client health metrics
cephilis health --mount /home --mode slurm   # exit 1 if the client is unhealthy
cephilis init                            # write config templates
```

Run any sub-command with `--help` for all flags.

## Configuration

Config files are read from `/etc/cephilis` (or `./config`, or `--config-dir`).

`sections.yaml` — folders to report. Each section exports its own recursive
totals plus one series per immediate subdirectory. Nest sections to get
deeper levels in the dashboard tree:

```yaml
workers: 16
sections:
  - name: Home
    path: /cephfs/home
  - name: Projects
    path: /cephfs/projects
  - name: Project archive
    path: /cephfs/projects/archive
```

`mounts.yaml` — mounts checked by `cephilis health`:

```yaml
mounts:
  - name: home
    path: /home
    required: true
    expected_fstype: ceph
```

## Deploy with systemd

```bash
sudo install -m 0644 systemd/cephilis-*.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cephilis-space.timer    # hourly, on ONE node
sudo systemctl enable --now cephilis-health.timer   # every minute, on every client
```

Run the space scan on a single node that mounts the filesystem: the numbers are
cluster-wide, and if CephFS hangs the scan can block in D state — the oneshot
unit is then simply not relaunched and `CephilisSpaceStale` fires.

## Metrics

| Metric | Labels | Source |
|---|---|---|
| `cephilis_dir_size_bytes` | `section`, `parent`, `name` | `ceph.dir.rbytes` of each subdirectory |
| `cephilis_dir_files_total` | `section`, `parent`, `name` | `ceph.dir.rfiles` of each subdirectory |
| `cephilis_section_size_bytes` | `section`, `parent` | `ceph.dir.rbytes` of the section root |
| `cephilis_section_files_total` | `section`, `parent` | `ceph.dir.rfiles` of the section root |
| `cephilis_section_scan_ok` | `section`, `parent` | 1 if the section root was read |
| `cephilis_section_failed_dirs` | `section`, `parent` | subdirectories skipped (unreadable xattrs) |
| `cephilis_health_ok` | `name`, `mount`, `hostname`, `expected`, `actual` | overall client health |
| `cephilis_mount_up`, `cephilis_mount_fstype_match` | same | mount table |
| `cephilis_cephfs_mds_session_open`, `cephilis_cephfs_session_unhealthy` | same | ceph debugfs `mds_sessions` |

## Grafana and alerts

- Dashboard: [`monitoring/grafana/dashboards/cephilis.json`](monitoring/grafana/dashboards/cephilis.json)
  — import it and pick your Prometheus datasource. Folder tree, per-folder
  drill-down, growth, and client health.
- Alert rules: [`monitoring/prometheus/cephilis-rules.yml`](monitoring/prometheus/cephilis-rules.yml)
  — unhealthy client, stale health metrics (hung client), stale or failed
  space scan.

## How it compares

| Tool | Per-folder usage | History / Grafana | Cost on a large CephFS |
|---|---|---|---|
| cephilis | yes | yes | one xattr per folder |
| Ceph Dashboard directory browser | quotas / snapshots | no | — |
| `du`, `ncdu`, `gdu`, `duc` | yes | no | walks every file |
| Robinhood | yes (+ owner, age, type) | own DB/UI | full scans + database |
| mgr `prometheus` module, `ceph_exporter` | no (cluster/pool level) | yes | — |

## Development

```bash
make test   # tests with race detector
make lint   # golangci-lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [ROADMAP.md](ROADMAP.md).

## License

[MIT](LICENSE)
