# Roadmap

Goal: become the standard way to see per-directory CephFS usage in
Prometheus/Grafana. Nothing else fills this gap: the mgr `prometheus` module
and `ceph_exporter` stop at cluster/pool level, and file-walking tools (`du`,
Robinhood) are too slow on large filesystems.

## 0.3 — make it worth adopting

- [ ] **Quota vs usage:** export `ceph.quota.max_bytes` / `max_files` per
      directory; usage-percent panel and alert at 90 %. No exporter shows this.
- [ ] **Recursive change time:** export `ceph.dir.rctime` per directory to find
      cold data (archive candidates).
- [ ] **Depth-based discovery:** `paths: [{path: /cephfs, depth: 2}]` instead of
      listing every nested section; labels `path`, `parent`, `depth`.
- [ ] **Cardinality cap:** `max_dirs` per path so a wide tree cannot flood
      Prometheus.
- [ ] **Owner label:** map the top-level directory uid to a user name for
      per-user reports on home directories.
- [ ] Settle metric names before 1.0 (e.g. `cephilis_dir_bytes{path,parent,depth}`),
      with a documented migration from 0.2 names.

## 0.4 — run anywhere

- [ ] `cephilis serve --listen :9xxx`: scan on an interval, serve the last good
      result over HTTP (no node_exporter needed; Kubernetes/Rook friendly).
- [ ] Scan in a child process with a watchdog; keep serving the last good
      result and export its age when a scan hangs.
- [ ] Container image and a Kubernetes/Rook example.

## Adoption

- [ ] Publish the dashboard on grafana.com (import by ID).
- [ ] README screenshots and an Ansible role example.
- [ ] Announce on ceph-users, r/ceph and HPC admin channels; propose a Ceph
      Days lightning talk.
- [ ] Ask to be listed in Ceph's monitoring/ecosystem docs.
- [ ] Get feedback from 2–3 external sites before 1.0.
