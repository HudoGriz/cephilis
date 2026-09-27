package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSpacePaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "data")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, "workers: 8\nmax_dirs: 50\npaths:\n  - path: "+sub+"/\n    depth: 3\n    owner: true\n  - path: "+dir+"\n")

	cfg, err := LoadSpace(cfgFile)
	if err != nil {
		t.Fatalf("LoadSpace: %v", err)
	}
	if cfg.Workers != 8 || cfg.MaxDirs != 50 || cfg.MaxEntries != DefaultMaxEntries {
		t.Errorf("unexpected limits: %+v", cfg)
	}
	want := []Path{{Path: sub, Depth: 3, Owner: true}, {Path: dir, Depth: DefaultDepth}}
	if len(cfg.Paths) != 2 || cfg.Paths[0] != want[0] || cfg.Paths[1] != want[1] {
		t.Errorf("paths = %+v, want %+v", cfg.Paths, want)
	}
}

// Pre-0.3 configs list `sections:`; they keep working as depth-1 paths.
func TestLoadSpaceLegacySections(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, SectionsFilename), "sections:\n  - name: Home Users\n    path: "+dir+"\n")
	cfg, err := LoadSpace(dir)
	if err != nil {
		t.Fatalf("LoadSpace: %v", err)
	}
	if len(cfg.Paths) != 1 || cfg.Paths[0] != (Path{Path: dir, Depth: 1}) || cfg.Workers != DefaultWorkers {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoadSpaceSkipsMissingPaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, "paths:\n  - path: "+dir+"\n  - path: /nonexistent/xyz\n")
	cfg, err := LoadSpace(cfgFile)
	if err != nil || len(cfg.Paths) != 1 {
		t.Fatalf("LoadSpace = %+v, %v", cfg, err)
	}
}

func TestLoadSpaceErrors(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"all missing": "paths:\n  - path: /nonexistent/xyz\n",
		"empty":       "paths: []\n",
		"no path key": "paths:\n  - depth: 2\n",
		"bad yaml":    ":\t: invalid\n",
	} {
		f := filepath.Join(dir, "x.yaml")
		writeFile(t, f, content)
		if _, err := LoadSpace(f); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := LoadSpace("/nonexistent/config.yaml"); err == nil {
		t.Error("missing file: expected error")
	}
}

func TestLoadMountsHappyPath(t *testing.T) {
	dir := t.TempDir()
	mountPath := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(dir, MountsFilename)
	writeFile(t, cfgFile,
		"mounts:\n  - name: home\n    path: "+mountPath+"\n")

	cfg, err := LoadMounts(cfgFile)
	if err != nil {
		t.Fatalf("LoadMounts error: %v", err)
	}
	if len(cfg.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(cfg.Mounts))
	}
	m := cfg.Mounts[0]
	if m.Name != "home" || m.Path != mountPath {
		t.Errorf("unexpected mount: %+v", m)
	}
}

// Deployed mounts.yaml files still carry keys from the removed `mount`
// command. They must keep loading: Slurm HealthCheckProgram and Prolog drain
// the node if `cephilis health` fails.
func TestLoadMountsIgnoresLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, MountsFilename)
	writeFile(t, cfgFile, "mounts:\n  - name: home\n    path: /home\n    probe: /home/.probe\n    timeout: 3\n    required: true\n    expected_fstype: ceph\n    legacy: true\n")

	cfg, err := LoadMounts(cfgFile)
	if err != nil {
		t.Fatalf("LoadMounts with legacy keys: %v", err)
	}
	if len(cfg.Mounts) != 1 || !cfg.Mounts[0].Required || cfg.Mounts[0].ExpectedFSType != "ceph" {
		t.Fatalf("unexpected mount config: %+v", cfg.Mounts[0])
	}
}

func TestLoadMountsMissingIsRetainedWithoutStat(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, MountsFilename)
	writeFile(t, cfgFile, "mounts:\n  - name: home\n    path: /nonexistent/required\n    required: false\n    expected_fstype: ceph\n")

	cfg, err := LoadMounts(cfgFile)
	if err != nil {
		t.Fatalf("LoadMounts error: %v", err)
	}
	if len(cfg.Mounts) != 1 {
		t.Fatalf("expected missing mount to remain, got %d", len(cfg.Mounts))
	}
	if cfg.Mounts[0].Path != "/nonexistent/required" || cfg.Mounts[0].ExpectedFSType != "ceph" {
		t.Fatalf("unexpected mount config: %+v", cfg.Mounts[0])
	}
}

func TestLoadMountsFromDir(t *testing.T) {
	dir := t.TempDir()
	mountPath := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, MountsFilename),
		"mounts:\n  - name: home\n    path: "+mountPath+"\n    probe: "+mountPath+"/.probe\n")
	cfg, err := LoadMounts(dir)
	if err != nil {
		t.Fatalf("LoadMounts(dir) error: %v", err)
	}
	if len(cfg.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(cfg.Mounts))
	}
}

func TestResolveConfigDirExplicit(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveConfigDir(dir)
	if err != nil {
		t.Fatalf("ResolveConfigDir error: %v", err)
	}
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}

func TestResolveConfigDirMissing(t *testing.T) {
	if _, err := ResolveConfigDir("/nonexistent/xyz"); err == nil {
		t.Fatal("expected error for missing dir")
	}
}
