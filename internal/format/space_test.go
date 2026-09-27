package format

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HudoGriz/cephilis/internal/model"
)

var testScan = model.Scan{
	Started:  time.Unix(1790000000, 0),
	Duration: 50 * time.Millisecond,
	Dirs: []model.Dir{
		{Path: "/cephfs/home", Parent: "/cephfs", Bytes: 1000, Files: 10, RCTime: 1790503574.69},
		{Path: "/cephfs/home/alice", Parent: "/cephfs/home", Owner: "alice", Bytes: 600, Files: 5, QuotaBytes: 1200},
		{Path: "/cephfs/home/bob", Parent: "/cephfs/home", Owner: "bob", Bytes: 400, Files: 3},
	},
	Roots: []model.RootResult{
		{Path: "/cephfs/home", Dirs: 3},
		{Path: "/cephfs/gone", Err: errors.New("enoent")},
	},
}

func TestSpacePromMetrics(t *testing.T) {
	out, err := Space("prom", testScan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`cephilis_dir_bytes{path="/cephfs/home",parent="/cephfs"} 1000`,
		`cephilis_dir_files{path="/cephfs/home/alice",parent="/cephfs/home",owner="alice"} 5`,
		`cephilis_dir_rctime_seconds{path="/cephfs/home",parent="/cephfs"} 1790503574.690`,
		`cephilis_dir_quota_bytes{path="/cephfs/home/alice",parent="/cephfs/home",owner="alice"} 1200`,
		`cephilis_root_scan_ok{root="/cephfs/home"} 1`,
		`cephilis_root_scan_ok{root="/cephfs/gone"} 0`,
		`cephilis_root_dirs{root="/cephfs/home"} 3`,
		`cephilis_scan_duration_seconds 0.050`,
		`cephilis_scan_timestamp_seconds 1790000000`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prom output missing %q", want)
		}
	}
	// Quota and rctime series only exist where the attribute is set.
	if strings.Contains(out, `cephilis_dir_quota_bytes{path="/cephfs/home/bob"`) {
		t.Error("quota series for a directory without quota")
	}
	if strings.Contains(out, `cephilis_dir_rctime_seconds{path="/cephfs/home/bob"`) {
		t.Error("rctime series for a directory without rctime")
	}
}

func TestSpacePromLabelEscaping(t *testing.T) {
	s := model.Scan{Dirs: []model.Dir{{Path: `/x/we"ird\dir`, Parent: "/x"}}}
	out, _ := Space("prom", s)
	if !strings.Contains(out, `path="/x/we\"ird\\dir"`) {
		t.Errorf("label not escaped:\n%s", out)
	}
}

func TestSpaceTable(t *testing.T) {
	out, err := Space("table", testScan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/cephfs/home", "/alice", "quota 50.0%", "! /cephfs/gone"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestSpaceCSVAndTSV(t *testing.T) {
	for name, sep := range map[string]string{"csv": ",", "tsv": "\t"} {
		out, err := Space(name, testScan)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 4 || !strings.HasPrefix(lines[0], "path"+sep+"parent") {
			t.Errorf("%s: unexpected output:\n%s", name, out)
		}
	}
}

func TestSpaceJSON(t *testing.T) {
	out, err := Space("json", testScan)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Dirs  []map[string]any `json:"dirs"`
		Roots []map[string]any `json:"roots"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Dirs) != 3 || len(v.Roots) != 2 || v.Roots[1]["error"] != "enoent" {
		t.Errorf("unexpected json: %s", out)
	}
}

func TestSpaceUnknownFormat(t *testing.T) {
	if _, err := Space("xml", testScan); err == nil {
		t.Error("expected error for unknown format")
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
