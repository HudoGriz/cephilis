package format

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HudoGriz/cephilis/internal/model"
)

var testResults = []model.SectionResult{
	{
		Name:       "Home Users",
		ParentPath: "/home",
		Parent:     &model.DirStats{RBytes: 1000, RFiles: 10},
		Entries: []model.DirStats{
			{Name: "user1", Path: "/home/user1", RBytes: 600, RFiles: 5},
			{Name: "user2", Path: "/home/user2", RBytes: 400, RFiles: 3},
		},
	},
}

func TestSpacePromContainsMetrics(t *testing.T) {
	out, err := Space("prom", testResults)
	if err != nil {
		t.Fatalf("Space(prom) error: %v", err)
	}
	for _, want := range []string{"cephilis_dir_size_bytes", "cephilis_dir_files_total", "user1", "user2"} {
		if !strings.Contains(out, want) {
			t.Errorf("prom output missing %q", want)
		}
	}
}

func TestSpacePromSectionMetrics(t *testing.T) {
	results := append([]model.SectionResult{}, testResults...)
	results = append(results, model.SectionResult{Name: "Gone", ParentPath: "/home/gone", Err: errors.New("enoent")})
	out, err := Space("prom", results)
	if err != nil {
		t.Fatalf("Space(prom) error: %v", err)
	}
	for _, want := range []string{
		`cephilis_section_scan_ok{section="gone",parent="/home/gone"} 0`,
		`cephilis_section_scan_ok{section="home_users",parent="/home"} 1`,
		`cephilis_section_size_bytes{section="home_users",parent="/home"}`,
		`cephilis_section_files_total{section="home_users",parent="/home"}`,
		`cephilis_section_failed_dirs{section="home_users",parent="/home"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prom output missing %q", want)
		}
	}
	if strings.Contains(out, `cephilis_section_size_bytes{section="gone"`) {
		t.Error("failed section must not report a size")
	}
}

func TestSpaceTable(t *testing.T) {
	out, err := Space("table", testResults)
	if err != nil {
		t.Fatalf("Space(table) error: %v", err)
	}
	if !strings.Contains(out, "Home Users") {
		t.Error("table missing section name")
	}
	if !strings.Contains(out, "user1") {
		t.Error("table missing entry name")
	}
}

func TestSpaceCSV(t *testing.T) {
	out, err := Space("csv", testResults)
	if err != nil {
		t.Fatalf("Space(csv) error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// header + 2 entries
	if len(lines) != 3 {
		t.Fatalf("expected 3 CSV lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "section,") {
		t.Errorf("unexpected CSV header: %q", lines[0])
	}
}

func TestSpaceTSV(t *testing.T) {
	out, err := Space("tsv", testResults)
	if err != nil {
		t.Fatalf("Space(tsv) error: %v", err)
	}
	if !strings.Contains(out, "\t") {
		t.Error("TSV output has no tabs")
	}
}

func TestSpaceJSON(t *testing.T) {
	out, err := Space("json", testResults)
	if err != nil {
		t.Fatalf("Space(json) error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := parsed["sections"]; !ok {
		t.Error("JSON missing 'sections' key")
	}
	if _, ok := parsed["timestamp"]; !ok {
		t.Error("JSON missing 'timestamp' key")
	}
}

func TestSpaceUnknownFormat(t *testing.T) {
	_, err := Space("xml", testResults)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestIsValid(t *testing.T) {
	for _, f := range []string{"table", "csv", "tsv", "json", "prom"} {
		if !IsValid(f) {
			t.Errorf("IsValid(%q) = false, want true", f)
		}
	}
	if IsValid("xml") {
		t.Error("IsValid('xml') = true, want false")
	}
}

func TestMountPromCompat(t *testing.T) {
	m := model.MountResult{
		Hostname:   "gpu1",
		MountPath:  "/home",
		ProbeFile:  "/home/.probe",
		Mounted:    true,
		Responsive: true,
	}
	out, err := Mount("prom", m, true)
	if err != nil {
		t.Fatalf("Mount(prom) error: %v", err)
	}
	for _, want := range []string{"mount_home_ok", "mount_home_responsive", "cephilis_mount_up"} {
		if !strings.Contains(out, want) {
			t.Errorf("prom mount output missing %q", want)
		}
	}
}

func TestMountPromNoCompat(t *testing.T) {
	m := model.MountResult{
		Hostname:   "gpu1",
		MountPath:  "/home",
		ProbeFile:  "/home/.probe",
		Mounted:    true,
		Responsive: true,
	}
	out, err := Mount("prom", m, false)
	if err != nil {
		t.Fatalf("Mount(prom) error: %v", err)
	}
	if strings.Contains(out, "mount_home_ok") {
		t.Error("compat metric should be absent when compat=false")
	}
}

func TestMountTable(t *testing.T) {
	m := model.MountResult{
		Hostname:      "node1",
		MountPath:     "/mnt/ceph",
		ProbeFile:     "/mnt/ceph/.probe",
		Mounted:       true,
		Responsive:    true,
		FSTypeMatches: true,
	}
	out, err := Mount("table", m, false)
	if err != nil {
		t.Fatalf("Mount(table) error: %v", err)
	}
	if !strings.Contains(out, "OK") {
		t.Error("table should show OK status")
	}
}

func TestMountTableDegraded(t *testing.T) {
	m := model.MountResult{
		Hostname:   "node1",
		MountPath:  "/mnt/ceph",
		ProbeFile:  "/mnt/ceph/.probe",
		Mounted:    false,
		Responsive: false,
		Error:      "mount path is not mounted",
	}
	out, err := Mount("table", m, false)
	if err != nil {
		t.Fatalf("Mount(table) error: %v", err)
	}
	if !strings.Contains(out, "DEGRADED") {
		t.Error("table should show DEGRADED status")
	}
}

func TestMountJSON(t *testing.T) {
	lat := 0.012
	m := model.MountResult{
		Hostname:       "node1",
		MountPath:      "/mnt/ceph",
		ProbeFile:      "/mnt/ceph/.probe",
		Mounted:        true,
		Responsive:     true,
		LatencySeconds: &lat,
	}
	out, err := Mount("json", m, false)
	if err != nil {
		t.Fatalf("Mount(json) error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestMountUnknownFormat(t *testing.T) {
	m := model.MountResult{}
	_, err := Mount("xml", m, false)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestHumanReadable(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := humanReadable(c.input); got != c.want {
			t.Errorf("humanReadable(%d) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestPercentOfParent(t *testing.T) {
	if got := percentOfParent(500, 1000); got != 50.0 {
		t.Errorf("expected 50.0, got %f", got)
	}
	if got := percentOfParent(100, 0); got != 0.0 {
		t.Errorf("expected 0.0 for zero parent, got %f", got)
	}
}
