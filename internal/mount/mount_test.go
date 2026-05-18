package mount

import (
	"testing"

	"github.com/HudoGriz/cephilis/internal/config"
)

func TestCheckNonExistentMountPath(t *testing.T) {
	result := Check(config.Mount{
		Name:    "ghost",
		Path:    "/nonexistent/path/xyz",
		Probe:   "/nonexistent/probe",
		Timeout: 1,
	})
	if result.Mounted {
		t.Error("expected Mounted=false for non-existent path")
	}
	if result.Responsive {
		t.Error("expected Responsive=false for non-existent path")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error")
	}
	if result.Hostname == "" {
		t.Error("expected non-empty Hostname")
	}
}

func TestCheckDefaultsTimeout(t *testing.T) {
	result := Check(config.Mount{Name: "x", Path: "/nonexistent/xyz", Probe: "/nonexistent/probe"})
	if result.MountPath == "" {
		t.Error("expected MountPath to be set")
	}
	if result.ProbeFile == "" {
		t.Error("expected ProbeFile to be set")
	}
}

func TestRequiredMissingMountIsUnhealthy(t *testing.T) {
	result := Check(config.Mount{Name: "home", Path: "/nonexistent/xyz", Probe: "/nonexistent/probe", Required: true, Timeout: 1})
	if !result.Required {
		t.Fatal("expected required flag in result")
	}
	if Healthy(result) {
		t.Fatal("expected missing required mount to be unhealthy")
	}
}

func TestCheckMountedNonExistentPath(t *testing.T) {
	mounted, err := checkMounted("/nonexistent/path/xyz", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mounted {
		t.Error("expected not mounted")
	}
}

func TestCheckProbeNonExistentPath(t *testing.T) {
	responsive, err := checkProbe("/nonexistent/probe/file", 2)
	if err == nil {
		t.Fatal("expected error for non-existent probe path")
	}
	if responsive {
		t.Error("expected not responsive")
	}
}

func TestHasError(t *testing.T) {
	result := Check(config.Mount{Path: "/nonexistent/xyz", Probe: "/nonexistent/probe", Timeout: 1})
	if !result.HasError() {
		t.Error("HasError() should return true when Error is set")
	}
}

func TestCheckAll(t *testing.T) {
	results := CheckAll([]config.Mount{
		{Name: "a", Path: "/nonexistent/a", Probe: "/nonexistent/a/.probe", Timeout: 1},
		{Name: "b", Path: "/nonexistent/b", Probe: "/nonexistent/b/.probe", Timeout: 1},
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}
