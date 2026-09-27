# cephilis

[![CI](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml/badge.svg)](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Per-directory CephFS usage for Prometheus and Grafana — without walking the tree.**

CephFS already tracks the recursive size, file count and latest change time of
every directory (`ceph.dir.rbytes`, `rfiles`, `rctime`). cephilis reads those
extended attributes for the directory trees you choose and exports them as
Prometheus metrics: per-project and per-user usage, growth, idle (cold) data,
quota usage, and a drill-down folder tree in Grafana.

- **No tree walk.** About 3–4 MDS requests per reported directory, never per
  file. Measured on a 660 TiB / 34 M-file filesystem: 1,175 directories in
  0.22 s. `du`, `ncdu` or Robinhood walk every file for hours.
- **Folder tree dashboard:** click a folder to see its subfolders — size,
  files, trend, 7-day growth, time since last change, usage by owner, quotas.
- **Cold data:** `ceph.dir.rctime` shows how long nothing changed anywhere
  below a folder — archive candidates at a glance.
- **Quotas:** usage vs `ceph.quota` with an alert before writes start failing.
- **D-safe client health (optional):** `cephilis health` reports whether the
  local CephFS kernel client is mounted with a healthy MDS session, reading only
  `/proc/self/mountinfo` and the ceph debugfs. It never touches the mount, so
  it keeps answering while CephFS hangs — usable as a Slurm
  `HealthCheckProgram` / `Prolog`.
- Single static binary; RPM/DEB, container image, systemd units, Grafana
  dashboard and alert rules included.

## Install

From [Releases](https://github.com/HudoGriz/cephilis/releases) (`linux/amd64`,
`linux/arm64`):

```bash
sudo dnf install ./cephilis_<version>_linux_amd64.rpm     # or: apt install ./….deb
```

Container: `ghcr.io/hudogriz/cephilis:<version>`. From source:
`make build && sudo make install`.

## Configuration

Files are read from `/etc/cephilis` (or `$CEPHILIS_CONFIG_DIR`, `./config`,
`--config-dir`).

`sections.yaml` — the directory trees to report:

```yaml
workers: 16          # parallel xattr reads
max_dirs: 1000       # per path: cap on reported directories (keeps the largest)
max_entries: 10000   # never list a directory with more immediate entries
skip_quotas: false   # true saves one MDS request per directory if you use no quotas
paths:
  - path: /cephfs/home
    depth: 1         # report the path plus 1 level of subdirectories (default)
    owner: true      # add the owner's user name (NSS/getent, so LDAP/SSSD works)
  - path: /cephfs/projects
    depth: 3
```

Every path reports itself plus all subdirectories down to `depth`. Paths may
overlap; each directory is reported once. A directory is only listed (readdir)
when `ceph.dir.subdirs` says it has subdirectories and it has at most
`max_entries` entries, so folders full of files are never enumerated.

The pre-0.3 `sections:` list is still accepted (each entry becomes a depth-1
path).

`mounts.yaml` — mounts checked by `cephilis health`:

```yaml
mounts:
  - name: home
    path: /home
    required: true
    expected_fstype: ceph
```

## Running

Two ways to get the metrics into Prometheus:

**node_exporter textfile collector** (the units write to
`/var/lib/node_exporter/textfile_collector`):

```bash
sudo systemctl enable --now cephilis-space.timer    # every 15 min — on ONE node
sudo systemctl enable --now cephilis-health.timer   # every minute — on every client
```

**Built-in exporter** on `:9966`:

```bash
sudo systemctl enable --now cephilis-serve.service  # or: cephilis serve --listen :9966 --interval 15m
```

`serve` runs each scan in a child process and serves the last good result. If
CephFS hangs, the scan blocks in D state and cannot be killed; `serve` then
starts no further scans and exports `cephilis_serve_scan_running_seconds`
instead of piling up stuck processes.

Run the space scan on a single node: CephFS statistics are cluster-wide.

Ad hoc:

```bash
cephilis space                      # human-readable tree report
cephilis space --format json        # also csv, tsv, prom
cephilis health --mount /home --mode slurm   # exit 1 when the client is unhealthy
```

Examples: [Ansible](examples/ansible/cephilis.yml),
[Kubernetes](examples/kubernetes/cephilis.yaml).

## Metrics

| Metric | Labels | Meaning |
|---|---|---|
| `cephilis_dir_bytes` | `path`, `parent`, `owner`¹ | recursive size (`ceph.dir.rbytes`) |
| `cephilis_dir_files` | same | recursive file count (`ceph.dir.rfiles`) |
| `cephilis_dir_rctime_seconds` | same | latest change anywhere below (`ceph.dir.rctime`) |
| `cephilis_dir_quota_bytes`, `cephilis_dir_quota_files` | same | quota, only where one is set |
| `cephilis_root_scan_ok` | `root` | configured path readable |
| `cephilis_root_dirs` | `root` | directories reported under the path |
| `cephilis_root_truncated` | `root` | 1 if `max_dirs`/`max_entries` cut the tree |
| `cephilis_root_failed_dirs` | `root` | directories whose xattrs could not be read |
| `cephilis_scan_duration_seconds`, `cephilis_scan_timestamp_seconds` | | last scan |
| `cephilis_scan_xattr_reads` | | xattr reads in the last scan = MDS requests |
| `cephilis_serve_*` | | `serve` mode: last success, running time, failures |
| `cephilis_health_ok` | `name`, `mount`, `hostname`, … | client health (`cephilis health`) |
| `cephilis_mount_up`, `cephilis_mount_fstype_match`, `cephilis_cephfs_mds_session_open`, `cephilis_cephfs_session_unhealthy` | same | health details |

¹ only for paths with `owner: true`.

## Grafana and alerts

- [`monitoring/grafana/dashboards/cephilis.json`](monitoring/grafana/dashboards/cephilis.json):
  import and pick your Prometheus datasource. Folder tree with click-through
  drill-down; size, files, idle time, trend, 7-day growth and owners of the
  selected folder's subfolders; quota usage; scan cost; client health.
- [`monitoring/prometheus/cephilis-rules.yml`](monitoring/prometheus/cephilis-rules.yml):
  unhealthy or hung client, stale or stuck scan, failed path, quota above 90 %.

## Load on the MDS

Every `ceph.*` xattr read is one MDS `getattr` (the kernel client does not
cache them), so cephilis keeps reads to a minimum: `rbytes`, `rfiles`,
`rctime` and `ceph.quota` per directory, plus `subdirs`/`entries` only for
directories it will list. `cephilis_scan_xattr_reads` reports the exact count.
For scale: 1,000 directories every 15 minutes is about 4 requests per second
for 0.2 s, then nothing — far below a busy MDS's background load.

## How it compares

| Tool | Per-folder usage | History / Grafana | Cost on a large CephFS |
|---|---|---|---|
| cephilis | yes (+ idle time, quota, owner) | yes | ~4 MDS requests per folder |
| Ceph Dashboard directory browser | quotas / snapshots | no | — |
| `du`, `ncdu`, `gdu`, `duc` | yes | no | walks every file |
| Robinhood | yes (+ owner, age, type) | own DB/UI | full scans + database |
| mgr `prometheus` module, `ceph_exporter` | no (cluster/pool level) | yes | — |

## Upgrading from 0.2

Metric names changed (`cephilis_dir_size_bytes` → `cephilis_dir_bytes`,
`section`/`name` labels → `path`/`parent`; `cephilis_section_*` →
`cephilis_root_*`). Re-import the dashboard and rules. Old `sections.yaml`
files keep working.

## Development

```bash
make test   # tests (fake CephFS xattrs) with race detector
make lint   # golangci-lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [ROADMAP.md](ROADMAP.md).

## License

[MIT](LICENSE)
