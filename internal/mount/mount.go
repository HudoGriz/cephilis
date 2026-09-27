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
		Name:           m.Name,
		Hostname:       hostname,
		MountPath:      m.Path,
		ProbeFile:      m.Probe,
		Required:       m.Required,
		ExpectedFSType: m.ExpectedFSType,
		FSTypeMatches:  m.ExpectedFSType == "",
	}
	if strings.TrimSpace(base.ProbeFile) == "" {
		base.Error = "probe is not configured for legacy mount check"
		return base
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
	base.Mounted = true

	fstype, fsErr := checkFSType(m.Path, timeoutSec)
	if fsErr != nil {
		base.Error = fsErr.Error()
		return base
	}
	base.ActualFSType = fstype
	if m.ExpectedFSType != "" {
		base.FSTypeMatches = strings.EqualFold(fstype, m.ExpectedFSType)
		if !base.FSTypeMatches {
			base.Error = fmt.Sprintf("mount %s fstype mismatch: got %s, want %s", m.Path, fstype, m.ExpectedFSType)
			return base
		}
	}

	start := time.Now()
	responsive, probeErr := checkProbe(m.Probe, timeoutSec)
	if probeErr != nil {
		base.Error = probeErr.Error()
		return base
	}
	if !responsive {
		base.Error = "probe check failed"
		return base
	}

	latency := time.Since(start).Seconds()
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

// checkFSType invokes stat(1) with a deadline and returns the filesystem type
// token, for example "ceph".
func checkFSType(path string, timeoutSec int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "stat", "-f", "-c", "%T", path)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("fstype check timed out after %ds: %s", timeoutSec, path)
		}
		return "", fmt.Errorf("fstype check failed for %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Healthy reports whether r satisfies the health contract for its mount.
func Healthy(r model.MountResult) bool {
	return r.Mounted && r.Responsive && r.FSTypeMatches && r.Error == ""
}

// RequiredHealthy reports whether all required mount results are healthy.
func RequiredHealthy(results []model.MountResult) bool {
	for _, r := range results {
		if r.Required && !Healthy(r) {
			return false
		}
	}
	return true
}
