package collector

import (
	"os"
	"path/filepath"
	"testing"
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

func TestFallbackDU(t *testing.T) {
	dir := t.TempDir()
	// Write a small file so du has something to count.
	if err := os.WriteFile(filepath.Join(dir, "data"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	size, err := fallbackDU(dir)
	if err != nil {
		t.Fatalf("fallbackDU error: %v", err)
	}
	if size <= 0 {
		t.Errorf("expected size > 0, got %d", size)
	}
}

func TestFallbackDUNonExistent(t *testing.T) {
	_, err := fallbackDU("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}
