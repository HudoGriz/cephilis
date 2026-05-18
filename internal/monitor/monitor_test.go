package monitor

import (
	"strings"
	"testing"
)

func TestParseCaps(t *testing.T) {
	total, used, avail, err := ParseCaps("total 100000\nused 125\navail 99875\n")
	if err != nil {
		t.Fatalf("ParseCaps error: %v", err)
	}
	if total != 100000 || used != 125 || avail != 99875 {
		t.Fatalf("unexpected caps: total=%d used=%d avail=%d", total, used, avail)
	}
}

func TestParseKernelEvents(t *testing.T) {
	events := ParseKernelEvents(`
ceph: caps stale for inode
ceph: reconnect start
ceph: client blocklisted
SELinux: invalid context for cephfs
ceph: cap renew failed
`)
	if events["caps_stale"] != 1 || events["reconnect"] != 1 || events["blocklist"] != 1 ||
		events["selinux_invalid_context"] != 1 || events["cap_renewal"] != 1 {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestParseCephHealth(t *testing.T) {
	if got := ParseCephHealth("HEALTH_WARN 1 MDS behind"); got != "HEALTH_WARN" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSlurmStates(t *testing.T) {
	states := ParseSlurmStates("RUNNING\nPENDING\nRUNNING\n")
	if states["running"] != 2 || states["pending"] != 1 {
		t.Fatalf("unexpected states: %#v", states)
	}
}

func TestPromIncludesCollectorMetrics(t *testing.T) {
	out := Prom(Collect())
	for _, needle := range []string{"cephilis_caps_total", "cephilis_kernel_events_total", "cephilis_collector_success"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected %q in prom output:\n%s", needle, out)
		}
	}
}
