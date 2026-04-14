# Cephilis

[![CI](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml/badge.svg)](https://github.com/HudoGriz/cephilis/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A minimal, dependency-free CephFS space and mount exporter for HPC nodes.
Produces Prometheus textfile metrics consumed by `node_exporter`.

## Goals

- Single static binary — no runtime dependencies beyond common Linux tools (`mountpoint`, `stat`, `du`)
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
cephilis space --format table --config ./config/paths.yaml
cephilis mount --format prom
cephilis all   --format prom --config ./config/paths.yaml
```

Run any sub-command with `--help` for full flag documentation.

## Config

Default search order:

1. `/etc/cephilis/paths.yaml`
2. `config/paths.yaml`

Example file:

```yaml
sections:
  - name: Home Users
    path: /home
  - name: Data Folders
    path: /home/Data
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
