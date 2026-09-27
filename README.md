# Cephilis

[![CI](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml/badge.svg)](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A minimal, dependency-free CephFS space and health exporter for HPC nodes.
Produces Prometheus textfile metrics consumed by `node_exporter`.

## Goals

- Single static binary
- Safe CephFS client health checks for Slurm: no `/home` stat/list/probe
- O(1) CephFS xattr queries — no filesystem traversal for space reporting
- Prometheus textfile output for `node_exporter`

## Install

Download a pre-built binary from [Releases](https://github.com/HudoGriz/cephilis/releases), or build from source:

```bash
git clone https://github.com/HudoGriz/cephilis.git
cd cephilis
make build
sudo make install
```

## Usage

```bash
cephilis version
cephilis space --format table --config ./config/sections.yaml
cephilis health --mount /home --mode slurm
cephilis health --mount /home --mode prom
cephilis health --mount /home --mode json
cephilis all   --format prom --config-dir ./config
```

Run any sub-command with `--help` for full flag documentation.

## Config

Default config directory search order:

1. `/etc/cephilis`
2. `config`

`mounts.yaml` declares safe health checks. Required mounts are retained even
when the path is missing, so Slurm wrappers can fail closed. Health checks read
the local mount table and Ceph kernel debugfs state; they do not stat/list/read
or write the mount path:

```yaml
mounts:
  - name: home
    path: /home
    required: true
    expected_fstype: ceph
```

`sections.yaml` declares CephFS xattr space scans:

```yaml
workers: 16
sections:
  - name: Home Users
    path: /home
```

`cephilis health --mode slurm` exits nonzero when the selected mount is
unhealthy. Slurm prolog and HealthCheckProgram wrappers should call it and keep
Slurm actions such as drain/resume in shell.

`--mode prom` prints Prometheus metrics and exits successfully when the
collector ran, even if the health metric is `0`. `--mode json` is intended for
debugging and tests.

For safe non-invasive tests, the health checker can be pointed at fake local
state:

```bash
CEPHILIS_MOUNTINFO_PATH=/tmp/fake-mountinfo \
CEPHILIS_CEPH_DEBUGFS_GLOB='/tmp/fake-ceph-debug/*/mds_sessions' \
cephilis health --mount /home --mode slurm
```

## Systemd deployment

Install binary:

```bash
sudo install -D -m 0755 bin/cephilis /usr/local/bin/cephilis
```

Install units:

```bash
sudo install -D -m 0644 systemd/cephilis-metrics.service /etc/systemd/system/cephilis-metrics.service
sudo install -D -m 0644 systemd/cephilis-metrics.timer   /etc/systemd/system/cephilis-metrics.timer
sudo systemctl daemon-reload
sudo systemctl enable --now cephilis-metrics.timer
```

Optional env file: `/etc/cephilis/metrics.env`

| Variable                      | Default                                        | Description                                   |
|-------------------------------|------------------------------------------------|-----------------------------------------------|
| `CEPHILIS_BIN`                | `/usr/local/bin/cephilis`                      | Path to the binary                            |
| `CEPHILIS_ARGS`               | `all --format prom`                            | Arguments passed to the binary                |
| `CEPHILIS_OUTPUT_DIR`         | `/var/lib/node_exporter/textfile_collector`    | Directory for `.prom` output                  |
| `CEPHILIS_OUTPUT_FILE`        | `cephilis.prom`                                | Output filename                               |
| `CEPHILIS_COMPAT_MOUNT_METRICS` | (unset)                                      | Set to `1` for legacy `mount_home_*` metrics  |

## Development

```bash
make test      # run tests with race detector
make cover     # generate HTML coverage report
make lint      # run golangci-lint (requires golangci-lint installed)
make vet       # go vet
make fmt       # gofmt
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
