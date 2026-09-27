package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"

	"github.com/HudoGriz/cephilis/internal/config"
)

// fakeCeph builds a real directory tree (so readdir works) and serves ceph.*
// xattrs from a map, counting readdir-worthy reads per path.
type fakeCeph struct {
	mu    sync.Mutex
	attrs map[string]map[string]string
}

func (f *fakeCeph) getxattr(path, attr string, dest []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.attrs[path]
	if !ok {
		return 0, syscall.ENOTSUP
	}
	v, ok := a[attr]
	if !ok {
		return 0, syscall.ENODATA
	}
	return copy(dest, v), nil
}

// dir registers a directory with recursive bytes and its immediate subdir count.
func (f *fakeCeph) dir(t *testing.T, path string, bytes int64, subdirs int, extra map[string]string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	a := map[string]string{
		"ceph.dir.rbytes":  strconv.FormatInt(bytes, 10),
		"ceph.dir.rfiles":  "7",
		"ceph.dir.rctime":  "1790503574.693164732",
		"ceph.dir.subdirs": strconv.Itoa(subdirs),
		"ceph.dir.entries": strconv.Itoa(subdirs),
	}
	for k, v := range extra {
		a[k] = v
	}
	f.mu.Lock()
	f.attrs[path] = a
	f.mu.Unlock()
}

func setup(t *testing.T) (string, *fakeCeph) {
	t.Helper()
	f := &fakeCeph{attrs: map[string]map[string]string{}}
	orig := getxattr
	getxattr = f.getxattr
	t.Cleanup(func() { getxattr = orig })
	root := t.TempDir()
	f.dir(t, root, 1000, 2, nil)
	f.dir(t, filepath.Join(root, "a"), 600, 2, map[string]string{"ceph.quota": "max_bytes=1200 max_files=0"})
	f.dir(t, filepath.Join(root, "a", "x"), 400, 0, nil)
	f.dir(t, filepath.Join(root, "a", "y"), 200, 0, nil)
	f.dir(t, filepath.Join(root, "b"), 400, 0, nil)
	return root, f
}

func cfg(paths ...config.Path) config.SpaceConfig {
	return config.SpaceConfig{Workers: 4, MaxDirs: 100, MaxEntries: 100, Paths: paths}
}

func paths(s []string) map[string]bool {
	m := map[string]bool{}
	for _, p := range s {
		m[p] = true
	}
	return m
}

func TestScanDepth(t *testing.T) {
	root, _ := setup(t)
	for depth, want := range map[int]int{1: 3, 2: 5} {
		s := Scan(cfg(config.Path{Path: root, Depth: depth}))
		if len(s.Dirs) != want {
			t.Errorf("depth %d: got %d dirs, want %d", depth, len(s.Dirs), want)
		}
		if s.Roots[0].Err != nil || s.Roots[0].Dirs != want {
			t.Errorf("depth %d: root result %+v", depth, s.Roots[0])
		}
	}
}

func TestScanReadsStatsAndQuota(t *testing.T) {
	root, _ := setup(t)
	s := Scan(cfg(config.Path{Path: root, Depth: 1}))
	var got []string
	for _, d := range s.Dirs {
		got = append(got, d.Path)
		if d.Path == filepath.Join(root, "a") {
			if d.Bytes != 600 || d.Files != 7 || d.QuotaBytes != 1200 || d.QuotaFiles != 0 || d.Parent != root || d.RCTime < 1.79e9 {
				t.Errorf("unexpected stats for a: %+v", d)
			}
		}
	}
	if !paths(got)[root] {
		t.Errorf("root itself not reported: %v", got)
	}
}

func TestScanDedupesOverlappingPaths(t *testing.T) {
	root, _ := setup(t)
	s := Scan(cfg(config.Path{Path: root, Depth: 1}, config.Path{Path: filepath.Join(root, "a"), Depth: 1}))
	seen := map[string]int{}
	for _, d := range s.Dirs {
		seen[d.Path]++
	}
	for p, n := range seen {
		if n != 1 {
			t.Errorf("%s reported %d times", p, n)
		}
	}
	if len(seen) != 5 {
		t.Errorf("got %d unique dirs, want 5", len(seen))
	}
	if s.Roots[1].Dirs != 3 {
		t.Errorf("second path counts %d dirs, want 3 (a, x, y) even though a was reported already", s.Roots[1].Dirs)
	}
}

func TestScanMaxDirsTruncates(t *testing.T) {
	root, _ := setup(t)
	c := cfg(config.Path{Path: root, Depth: 2})
	c.MaxDirs = 2
	s := Scan(c)
	if len(s.Dirs) != 2 || !s.Roots[0].Truncated {
		t.Errorf("got %d dirs truncated=%v, want 2 and truncated", len(s.Dirs), s.Roots[0].Truncated)
	}
}

func TestScanMaxEntriesSkipsListing(t *testing.T) {
	root, f := setup(t)
	f.attrs[filepath.Join(root, "a")]["ceph.dir.entries"] = "500000"
	s := Scan(cfg(config.Path{Path: root, Depth: 2}))
	for _, d := range s.Dirs {
		if filepath.Dir(d.Path) == filepath.Join(root, "a") {
			t.Errorf("child of a huge directory was listed: %s", d.Path)
		}
	}
	if !s.Roots[0].Truncated {
		t.Error("expected truncated when a directory is too large to list")
	}
}

func TestScanSkipsListingLeafDirs(t *testing.T) {
	root, _ := setup(t)
	// A subdirectory the xattrs do not know about: never read because
	// ceph.dir.subdirs of b is 0, so b is not listed.
	if err := os.Mkdir(filepath.Join(root, "b", "hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Scan(cfg(config.Path{Path: root, Depth: 3}))
	if s.Roots[0].FailedDirs != 0 {
		t.Errorf("leaf dir was listed: failed_dirs=%d", s.Roots[0].FailedDirs)
	}
}

func TestScanCountsUnreadableDirs(t *testing.T) {
	root, f := setup(t)
	delete(f.attrs, filepath.Join(root, "b"))
	s := Scan(cfg(config.Path{Path: root, Depth: 1}))
	if s.Roots[0].FailedDirs != 1 || len(s.Dirs) != 2 {
		t.Errorf("failed=%d dirs=%d, want 1 and 2", s.Roots[0].FailedDirs, len(s.Dirs))
	}
}

func TestScanKeepsGoingPastFailedPaths(t *testing.T) {
	root, _ := setup(t)
	s := Scan(cfg(config.Path{Path: "/nonexistent/xyz", Depth: 1}, config.Path{Path: root, Depth: 1}))
	if len(s.Roots) != 2 || s.Roots[0].Err == nil || s.Roots[1].Err != nil || len(s.Dirs) != 3 {
		t.Errorf("roots=%+v dirs=%d", s.Roots, len(s.Dirs))
	}
}

func TestScanOwner(t *testing.T) {
	root, _ := setup(t)
	origStat, origLookup := statUID, lookupOwner
	t.Cleanup(func() { statUID, lookupOwner = origStat, origLookup })
	lookups := 0
	statUID = func(string) (uint32, error) { return 1234, nil }
	lookupOwner = func(uid uint32) string { lookups++; return "alice" }
	s := Scan(cfg(config.Path{Path: root, Depth: 2, Owner: true}))
	for _, d := range s.Dirs {
		if d.Owner != "alice" {
			t.Errorf("%s owner=%q", d.Path, d.Owner)
		}
	}
	if lookups != 1 {
		t.Errorf("uid resolved %d times, want 1 (cached)", lookups)
	}
}

func TestListSubdirsOnlyDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listSubdirs(dir)
	if err != nil || len(got) != 2 || filepath.Base(got[0]) != "alpha" {
		t.Errorf("listSubdirs = %v, %v", got, err)
	}
}

// Each ceph.* xattr read is an MDS round trip; leaves must not read the
// listing attributes, and skip_quotas must not read ceph.quota.
func TestScanXattrReadsPerDirectory(t *testing.T) {
	root, f := setup(t)
	var mu sync.Mutex
	reads := map[string]int{}
	inner := f.getxattr
	getxattr = func(path, attr string, dest []byte) (int, error) {
		mu.Lock()
		reads[attr]++
		mu.Unlock()
		return inner(path, attr, dest)
	}
	c := cfg(config.Path{Path: root, Depth: 1})
	c.SkipQuotas = true
	scan := Scan(c)
	if scan.XattrReads != int64(reads["ceph.dir.rbytes"]+reads["ceph.dir.rfiles"]+reads["ceph.dir.rctime"]+reads["ceph.dir.subdirs"]+reads["ceph.dir.entries"]) {
		t.Errorf("XattrReads=%d does not match %v", scan.XattrReads, reads)
	}
	// root is listed (subdirs+entries), its 2 children are leaves.
	if reads["ceph.dir.subdirs"] != 1 || reads["ceph.dir.entries"] != 1 || reads["ceph.quota"] != 0 || reads["ceph.dir.rbytes"] != 3 {
		t.Errorf("xattr reads = %v", reads)
	}
}

func TestScanKeepsLargestWhenTruncating(t *testing.T) {
	root, _ := setup(t)
	c := cfg(config.Path{Path: root, Depth: 1})
	c.MaxDirs = 2 // root + one child
	s := Scan(c)
	if len(s.Dirs) != 2 || s.Dirs[1].Path != filepath.Join(root, "a") {
		t.Errorf("kept %v, want root and the larger child a", s.Dirs)
	}
}

func TestParseQuota(t *testing.T) {
	b, f := parseQuota("max_bytes=1099511627776 max_files=500")
	if b != 1099511627776 || f != 500 {
		t.Errorf("parseQuota = %d, %d", b, f)
	}
}
