// Package collector reports CephFS directory usage from the recursive
// statistics CephFS keeps for every directory (ceph.dir.rbytes, rfiles,
// rctime) plus quotas (ceph.quota). It never walks files.
//
// Every ceph.* xattr read is one MDS getattr round trip (measured: no client
// caching between reads), so reads are kept to the minimum: 4 per directory
// (rbytes, rfiles, rctime, quota; 3 with skip_quotas), plus ceph.dir.subdirs
// and ceph.dir.entries only for directories that will be listed.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

// xattrReads counts ceph.* xattr reads (each one MDS round trip).
var xattrReads atomic.Int64

// Swappable for tests (a plain filesystem has no ceph.* xattrs).
var (
	getxattr    = syscall.Getxattr
	lookupOwner = getentOwner
	statUID     = func(path string) (uint32, error) {
		var st syscall.Stat_t
		if err := syscall.Stat(path, &st); err != nil {
			return 0, err
		}
		return st.Uid, nil
	}
)

// Scan reports every configured path and its subdirectories down to the
// configured depth. Directories reachable from several paths are reported
// once. A path that fails is returned with Err set; it never aborts the scan.
func Scan(cfg config.SpaceConfig) model.Scan {
	start := time.Now()
	reads0 := xattrReads.Load()
	seen := map[string]bool{}
	scan := model.Scan{Started: start}
	owners := newOwnerCache()
	for _, p := range cfg.Paths {
		dirs, root := scanPath(p, cfg, seen, owners)
		scan.Dirs = append(scan.Dirs, dirs...)
		scan.Roots = append(scan.Roots, root)
	}
	sort.Slice(scan.Dirs, func(i, j int) bool { return scan.Dirs[i].Path < scan.Dirs[j].Path })
	scan.Duration = time.Since(start)
	scan.XattrReads = xattrReads.Load() - reads0
	return scan
}

func scanPath(p config.Path, cfg config.SpaceConfig, seen map[string]bool, owners *ownerCache) ([]model.Dir, model.RootResult) {
	root := model.RootResult{Path: p.Path}
	top, err := readDir(p.Path, p.Depth > 0, cfg.SkipQuotas)
	if err != nil {
		root.Err = err
		return nil, root
	}
	if p.Owner {
		top.Owner = owners.get(p.Path)
	}

	// Dirs counts everything under this path; out holds only directories not
	// already reported under another configured path.
	var out []model.Dir
	add := func(d model.Dir) bool {
		if root.Dirs >= cfg.MaxDirs {
			root.Truncated = true
			return false
		}
		root.Dirs++
		if !seen[d.Path] {
			seen[d.Path] = true
			out = append(out, d)
		}
		return true
	}
	add(top)

	level := []model.Dir{top}
	for depth := 1; depth <= p.Depth && len(level) > 0; depth++ {
		// Find the subdirectories of every directory on this level.
		var children []string
		for _, d := range level {
			if d.Subdirs == 0 {
				continue
			}
			if d.Entries > int64(cfg.MaxEntries) {
				root.Truncated = true
				continue
			}
			names, err := listSubdirs(d.Path)
			if err != nil {
				root.FailedDirs++
				continue
			}
			children = append(children, names...)
		}
		// Read their stats in parallel.
		listNext := depth < p.Depth
		stats := parallel(children, cfg.Workers, func(path string) (model.Dir, error) {
			d, err := readDir(path, listNext, cfg.SkipQuotas)
			if err == nil && p.Owner {
				d.Owner = owners.get(path)
			}
			return d, err
		})
		// When this level would exceed max_dirs, keep its largest directories.
		if root.Dirs+len(stats) > cfg.MaxDirs {
			sort.SliceStable(stats, func(i, j int) bool { return stats[i].dir.Bytes > stats[j].dir.Bytes })
		}
		level = level[:0]
		for _, r := range stats {
			if r.err != nil {
				root.FailedDirs++
				continue
			}
			if !add(r.dir) {
				break
			}
			level = append(level, r.dir)
		}
	}
	return out, root
}

// readDir reads the CephFS statistics of one directory. listing asks for the
// immediate subdir/entry counts needed to decide whether to list it.
func readDir(path string, listing, skipQuotas bool) (model.Dir, error) {
	d := model.Dir{Path: path, Parent: filepath.Dir(path)}
	var err error
	if d.Bytes, err = xattrInt(path, "ceph.dir.rbytes"); err != nil {
		return model.Dir{}, fmt.Errorf("read ceph.dir.rbytes on %s (not CephFS?): %w", path, err)
	}
	if d.Files, err = xattrInt(path, "ceph.dir.rfiles"); err != nil {
		return model.Dir{}, fmt.Errorf("read ceph.dir.rfiles on %s: %w", path, err)
	}
	// Optional attributes: absent on old releases or when no quota is set.
	d.RCTime, _ = xattrFloat(path, "ceph.dir.rctime")
	if !skipQuotas {
		if q, err := xattrString(path, "ceph.quota"); err == nil {
			d.QuotaBytes, d.QuotaFiles = parseQuota(q)
		}
	}
	if listing {
		d.Subdirs, err = xattrInt(path, "ceph.dir.subdirs")
		if err != nil {
			d.Subdirs = -1 // unknown: list the directory to find out
		}
		if d.Subdirs != 0 {
			d.Entries, _ = xattrInt(path, "ceph.dir.entries")
		}
	}
	return d, nil
}

// parseQuota parses the combined ceph.quota xattr, e.g.
// "max_bytes=1099511627776 max_files=0" (one MDS round trip instead of two).
func parseQuota(s string) (maxBytes, maxFiles int64) {
	for _, f := range strings.Fields(s) {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "max_bytes":
			maxBytes = n
		case "max_files":
			maxFiles = n
		}
	}
	return maxBytes, maxFiles
}

func xattrString(path, attr string) (string, error) {
	buf := make([]byte, 64)
	xattrReads.Add(1)
	n, err := getxattr(path, attr, buf)
	if err != nil {
		return "", err
	}
	if n <= 0 {
		return "", errors.New("empty xattr")
	}
	return strings.TrimSpace(string(bytes.Trim(buf[:n], "\x00"))), nil
}

func xattrInt(path, attr string) (int64, error) {
	s, err := xattrString(path, attr)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s on %s: %w", attr, path, err)
	}
	return v, nil
}

// xattrFloat parses values like ceph.dir.rctime ("1790503574.693164732").
func xattrFloat(path, attr string) (float64, error) {
	s, err := xattrString(path, attr)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s, 64)
}

// listSubdirs returns the sorted immediate subdirectories of path.
func listSubdirs(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			paths = append(paths, filepath.Join(path, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

type result struct {
	dir model.Dir
	err error
}

// parallel runs fn over paths with up to workers goroutines, keeping order.
func parallel(paths []string, workers int, fn func(string) (model.Dir, error)) []result {
	out := make([]result, len(paths))
	if workers <= 0 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p string) {
			defer wg.Done()
			defer func() { <-sem }()
			d, err := fn(p)
			out[i] = result{dir: d, err: err}
		}(i, p)
	}
	wg.Wait()
	return out
}

// ownerCache maps directory uids to user names, resolving each uid once.
type ownerCache struct {
	mu    sync.Mutex
	names map[uint32]string
}

func newOwnerCache() *ownerCache { return &ownerCache{names: map[uint32]string{}} }

func (c *ownerCache) get(path string) string {
	uid, err := statUID(path)
	if err != nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if name, ok := c.names[uid]; ok {
		return name
	}
	name := lookupOwner(uid)
	c.names[uid] = name
	return name
}

// getentOwner resolves a uid through NSS (getent), so LDAP/SSSD users work in
// a static binary; it falls back to the numeric uid.
func getentOwner(uid uint32) string {
	id := strconv.FormatUint(uint64(uid), 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "getent", "passwd", id).Output()
	if err != nil {
		return id
	}
	if name, _, ok := strings.Cut(string(out), ":"); ok && name != "" {
		return name
	}
	return id
}
