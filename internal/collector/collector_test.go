package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HudoGriz/cephilis/internal/config"
)

func TestListSubdirsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := listSubdirs(dir)
	if err != nil {
		t.Fatalf("listSubdirs error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 subdirs, got %d", len(got))
	}
}

func TestListSubdirsOnlyDirs(t *testing.T) {
	dir := t.TempDir()
	// Create two subdirs and one file — only dirs should be returned.
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := listSubdirs(dir)
	if err != nil {
		t.Fatalf("listSubdirs error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 subdirs, got %d: %v", len(got), got)
	}
	// Must be sorted.
	if filepath.Base(got[0]) != "alpha" || filepath.Base(got[1]) != "beta" {
		t.Errorf("unexpected order: %v", got)
	}
}

func TestListSubdirsNonExistentPermission(t *testing.T) {
	// A path that doesn't exist should not produce a permission error, but
	// listSubdirs should return an error for non-permission failures.
	_, err := listSubdirs("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestScanAllKeepsGoingPastFailedSections(t *testing.T) {
	// A plain temp dir has no ceph.dir.* xattrs, so every section fails; the
	// scan must still return one result per section instead of aborting.
	sections := []config.Section{
		{Name: "missing", Path: "/nonexistent/path/xyz"},
		{Name: "not ceph", Path: t.TempDir()},
	}
	got := ScanAll(sections, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	for _, r := range got {
		if r.Err == nil {
			t.Errorf("section %q: expected Err, got nil", r.Name)
		}
	}
}
