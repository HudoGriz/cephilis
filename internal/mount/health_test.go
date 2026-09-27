package mount

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HudoGriz/cephilis/internal/config"
)

func TestHasBadSessionState(t *testing.T) {
	for _, line := range []string{
		"mds.0 stale",
		"client blocklisted",
		"ceph: reconnect start",
		"mds.0 closed",
	} {
		if !hasBadSessionState(line) {
			t.Fatalf("expected bad session state for %q", line)
		}
	}
	if hasBadSessionState("mds.0 open") {
		t.Fatal("open session should not be bad")
	}
}

func TestReadMDSSessions(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "mds_sessions")
	if err := os.WriteFile(file, []byte("global_id 1\nname \"hpc\"\nmds.0 open\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, files, err := readMDSSessions(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("readMDSSessions error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}
	if len(lines) != 3 || lines[2] != "mds.0 open" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestCheckHealthMissingMount(t *testing.T) {
	result := CheckHealth(config.Mount{Name: "ghost", Path: "/nonexistent/cephilis/ghost", ExpectedFSType: "ceph"})
	if result.Healthy {
		t.Fatal("expected missing mount to be unhealthy")
	}
	if result.Mounted {
		t.Fatal("expected missing mount to report Mounted=false")
	}
	if result.Error == "" {
		t.Fatal("expected error")
	}
}

func TestUnescapeMountInfo(t *testing.T) {
	got := unescapeMountInfo(`/mnt/ceph\040home`)
	if got != "/mnt/ceph home" {
		t.Fatalf("unexpected unescaped mountinfo path: %q", got)
	}
}

func TestMountFSTypePrefersExpectedFSType(t *testing.T) {
	input := strings.Join([]string{
		"1 0 0:1 / /home rw - autofs systemd-1 rw",
		"2 0 0:2 /home /home rw - ceph 10.0.0.1:/home rw",
	}, "\n")
	fstype, mounted, err := mountFSTypeFromScanner(bufio.NewScanner(strings.NewReader(input)), "/home", "ceph")
	if err != nil {
		t.Fatalf("mountFSTypeFromScanner error: %v", err)
	}
	if !mounted || fstype != "ceph" {
		t.Fatalf("expected mounted ceph, got mounted=%t fstype=%q", mounted, fstype)
	}
}

func TestCheckHealthWithFakeOpenSession(t *testing.T) {
	t.Setenv("CEPHILIS_MOUNTINFO_PATH", fakeMountInfo(t, "/home", "ceph"))
	t.Setenv("CEPHILIS_CEPH_DEBUGFS_GLOB", fakeSessionGlob(t, "global_id 1\nname \"hpc\"\nmds.0 open\n"))

	result := CheckHealth(config.Mount{Name: "home", Path: "/home", ExpectedFSType: "ceph"})
	if !result.Healthy {
		t.Fatalf("expected healthy result, got: %+v", result)
	}
	if !result.Mounted || !result.FSTypeMatches || !result.MDSSessionOpen || result.UnhealthySession {
		t.Fatalf("unexpected health fields: %+v", result)
	}
}

func TestCheckHealthWithFakeStaleSession(t *testing.T) {
	t.Setenv("CEPHILIS_MOUNTINFO_PATH", fakeMountInfo(t, "/home", "ceph"))
	t.Setenv("CEPHILIS_CEPH_DEBUGFS_GLOB", fakeSessionGlob(t, "global_id 1\nname \"hpc\"\nmds.0 open\nmds.0 stale\n"))

	result := CheckHealth(config.Mount{Name: "home", Path: "/home", ExpectedFSType: "ceph"})
	if result.Healthy {
		t.Fatalf("expected stale session to be unhealthy: %+v", result)
	}
	if !result.UnhealthySession {
		t.Fatalf("expected UnhealthySession=true: %+v", result)
	}
}

func TestCheckHealthWithFakeNoSessions(t *testing.T) {
	t.Setenv("CEPHILIS_MOUNTINFO_PATH", fakeMountInfo(t, "/home", "ceph"))
	dir := t.TempDir()
	t.Setenv("CEPHILIS_CEPH_DEBUGFS_GLOB", filepath.Join(dir, "*", "mds_sessions"))

	result := CheckHealth(config.Mount{Name: "home", Path: "/home", ExpectedFSType: "ceph"})
	if result.Healthy {
		t.Fatalf("expected no debugfs sessions to be unhealthy: %+v", result)
	}
	if result.Error != "no CephFS client mds_sessions found in debugfs" {
		t.Fatalf("unexpected error: %q", result.Error)
	}
}

func fakeMountInfo(t *testing.T, mountPath, fstype string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "mountinfo")
	content := "1 0 0:1 / " + mountPath + " rw - " + fstype + " source rw\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func fakeSessionGlob(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "client.test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mds_sessions"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(dir), "*", "mds_sessions")
}
