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

func TestLoadSectionsHappyPath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "data")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, "workers: 8\nsections:\n  - name: Test\n    path: "+sub+"\n")

	cfg, err := LoadSections(cfgFile)
	if err != nil {
		t.Fatalf("LoadSections error: %v", err)
	}
	if len(cfg.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(cfg.Sections))
	}
	if cfg.Sections[0].Name != "Test" {
		t.Errorf("unexpected section name: %q", cfg.Sections[0].Name)
	}
	if cfg.Workers != 8 {
		t.Errorf("unexpected workers: %d", cfg.Workers)
	}
}

func TestLoadSectionsFromDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "data")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, SectionsFilename),
		"sections:\n  - name: A\n    path: "+sub+"\n")

	cfg, err := LoadSections(dir)
	if err != nil {
		t.Fatalf("LoadSections(dir) error: %v", err)
	}
	if len(cfg.Sections) != 1 || cfg.Sections[0].Name != "A" {
		t.Errorf("unexpected sections: %+v", cfg.Sections)
	}
	if cfg.Workers != DefaultWorkers {
		t.Errorf("workers should default to %d, got %d", DefaultWorkers, cfg.Workers)
	}
}

func TestLoadSectionsFiltersNonExistent(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile,
		"sections:\n  - name: Exists\n    path: "+existing+"\n  - name: Ghost\n    path: /nonexistent/path/xyz\n")

	cfg, err := LoadSections(cfgFile)
	if err != nil {
		t.Fatalf("LoadSections error: %v", err)
	}
	if len(cfg.Sections) != 1 || cfg.Sections[0].Name != "Exists" {
		t.Errorf("expected only Exists survived: %+v", cfg.Sections)
	}
}

func TestLoadSectionsAllMissing(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, "sections:\n  - name: Ghost\n    path: /nonexistent/xyz\n")
	if _, err := LoadSections(cfgFile); err == nil {
		t.Fatal("expected error when all paths are missing")
	}
}

func TestLoadSectionsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, "sections: []\n")
	if _, err := LoadSections(cfgFile); err == nil {
		t.Fatal("expected error for empty sections")
	}
}

func TestLoadSectionsBadYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, SectionsFilename)
	writeFile(t, cfgFile, ":\t: invalid\n")
	if _, err := LoadSections(cfgFile); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadSectionsMissingFile(t *testing.T) {
	if _, err := LoadSections("/nonexistent/path/to/config.yaml"); err == nil {
		t.Fatal("expected error for missing config file")
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
