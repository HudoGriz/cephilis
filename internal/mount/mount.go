// Package mount provides CephFS mount health checks: it verifies that a
// configured path is mounted and that a probe file can be stat'd within a
// deadline, measuring the round-trip latency.
package mount

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

// Check runs a full mount health check for the given Mount.  It never returns
// an error; failure information is encoded in MountResult.Error.  All
// parameters (path, probe, timeout) come from the config.Mount entry.
func Check(m config.Mount) model.MountResult {
	timeoutSec := m.Timeout
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultMountTimeoutSeconds
	}

	hostname, _ := os.Hostname()
	if idx := strings.IndexByte(hostname, '.'); idx > 0 {
		hostname = hostname[:idx]
	}

	base := model.MountResult{
		Hostname:  hostname,
		MountPath: m.Path,
		ProbeFile: m.Probe,
	}

	mounted, mountErr := checkMounted(m.Path, timeoutSec)
	if mountErr != nil {
		base.Error = mountErr.Error()
		return base
	}
	if !mounted {
		base.Error = fmt.Sprintf("mount path is not mounted: %s", m.Path)
		return base
	}

	start := time.Now()
	responsive, probeErr := checkProbe(m.Probe, timeoutSec)
	if probeErr != nil {
		base.Mounted = true
		base.Error = probeErr.Error()
		return base
	}
	if !responsive {
		base.Mounted = true
		base.Error = "probe check failed"
		return base
	}

	latency := time.Since(start).Seconds()
	base.Mounted = true
	base.Responsive = true
	base.LatencySeconds = &latency
	return base
}

// CheckAll runs Check against every mount sequentially.
func CheckAll(mounts []config.Mount) []model.MountResult {
	out := make([]model.MountResult, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, Check(m))
	}
	return out
}

// checkMounted invokes mountpoint(1) with a deadline.
func checkMounted(path string, timeoutSec int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mountpoint", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return false, fmt.Errorf("mountpoint check timed out after %ds: %s", timeoutSec, path)
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
		return false, nil
	}
	return false, err
}

// checkProbe invokes stat(1) on path with a deadline.
func checkProbe(path string, timeoutSec int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "stat", path)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("probe timed out after %ds: %s", timeoutSec, path)
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return false, fmt.Errorf("probe stat failed: %s", path)
		}
		return false, err
	}
	return true, nil
}
