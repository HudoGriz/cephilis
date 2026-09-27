# Roadmap

Goal: become the standard way to see per-directory CephFS usage in
Prometheus/Grafana. Nothing else fills this gap: the mgr `prometheus` module
and `ceph_exporter` stop at cluster/pool level, and file-walking tools (`du`,
Robinhood) are too slow on large filesystems.

## 0.3 — make it worth adopting (done in 0.3.0)

- [x] **Quota vs usage:** export `ceph.quota.max_bytes` / `max_files` per
      directory; usage-percent panel and alert at 90 %. No exporter shows this.
- [x] **Recursive change time:** export `ceph.dir.rctime` per directory to find
      cold data (archive candidates).
- [x] **Depth-based discovery:** `paths: [{path: /cephfs, depth: 2}]` instead of
      listing every nested section; labels `path`, `parent`.
- [x] **Cardinality cap:** `max_dirs` per path so a wide tree cannot flood
      Prometheus.
- [x] **Owner label:** map the top-level directory uid to a user name for
      per-user reports on home directories.
- [x] Settle metric names before 1.0 (`cephilis_dir_bytes{path,parent}`),
      with a documented migration from 0.2 names.
- [ ] Verify quota metrics on a cluster that uses quotas (tested with fakes
      only; the author's cluster has none).

## 0.4 — run anywhere (done in 0.3.0)

- [x] `cephilis serve --listen :9xxx`: scan on an interval, serve the last good
      result over HTTP (no node_exporter needed; Kubernetes/Rook friendly).
- [x] Scan in a child process with a watchdog; keep serving the last good
      result and export its age when a scan hangs.
- [x] Container image and a Kubernetes/Rook example.

## Next

- [ ] Multi-arch container image (arm64).
- [ ] Optional per-owner totals across the whole tree.

## Adoption

- [ ] Publish the dashboard on grafana.com (import by ID).
- [x] Ansible example.
- [ ] README screenshots.
- [ ] Announce on ceph-users, r/ceph and HPC admin channels; propose a Ceph
      Days lightning talk.
- [ ] Ask to be listed in Ceph's monitoring/ecosystem docs.
- [ ] Get feedback from 2–3 external sites before 1.0.
