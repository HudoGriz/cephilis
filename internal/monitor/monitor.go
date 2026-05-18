// Package monitor collects Linux/Ceph/Slurm signals that complement the core
// mount and space checks.
package monitor

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

const commandTimeout = 2 * time.Second

// Collect gathers best-effort monitoring signals. Individual collector
// failures are encoded in the returned metrics instead of aborting the command.
func Collect() model.MonitorResult {
	return model.MonitorResult{
		Caps:   collectCaps(),
		Kernel: collectKernel(),
		Ceph:   collectCephHealth(),
		Slurm:  collectSlurmJobs(),
	}
}

func collectCaps() model.CapsMetrics {
	start := time.Now()
	m := model.CapsMetrics{Collector: "caps", Success: false}
	out, err := runShell(commandTimeout, "timeout 2 head -5 /sys/kernel/debug/ceph/*/caps 2>/dev/null")
	m.DurationSeconds = time.Since(start).Seconds()
	if err != nil {
		m.Error = err.Error()
		return m
	}
	total, used, avail, parseErr := ParseCaps(out)
	if parseErr != nil {
		m.Error = parseErr.Error()
		return m
	}
	m.Total = total
	m.Used = used
	m.Available = avail
	m.Success = true
	return m
}

// ParseCaps parses the leading lines from /sys/kernel/debug/ceph/*/caps.
func ParseCaps(input string) (total, used, available int64, err error) {
	seen := map[string]bool{}
	for _, line := range strings.Split(input, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "total":
			total = v
			seen["total"] = true
		case "used":
			used = v
			seen["used"] = true
		case "avail":
			available = v
			seen["avail"] = true
		}
	}
	for _, key := range []string{"total", "used", "avail"} {
		if !seen[key] {
			return 0, 0, 0, fmt.Errorf("caps output missing %s", key)
		}
	}
	return total, used, available, nil
}

func collectKernel() model.KernelEventMetrics {
	start := time.Now()
	m := model.KernelEventMetrics{Collector: "kernel_events", Success: false}
	out, err := runShell(commandTimeout, "dmesg 2>/dev/null || journalctl -k -n 2000 --no-pager 2>/dev/null")
	m.DurationSeconds = time.Since(start).Seconds()
	if err != nil {
		m.Error = err.Error()
		return m
	}
	m.Events = ParseKernelEvents(out)
	m.Success = true
	return m
}

// ParseKernelEvents counts the event families that matter for CephFS cap
// pressure and SELinux invalid-context failures.
func ParseKernelEvents(input string) map[string]int64 {
	events := map[string]int64{
		"caps_stale":              0,
		"cap_renewal":             0,
		"blocklist":               0,
		"reconnect":               0,
		"selinux_invalid_context": 0,
	}
	for _, line := range strings.Split(strings.ToLower(input), "\n") {
		switch {
		case strings.Contains(line, "caps stale"):
			events["caps_stale"]++
		case strings.Contains(line, "renew") && strings.Contains(line, "cap"):
			events["cap_renewal"]++
		case strings.Contains(line, "blocklist") || strings.Contains(line, "blacklist"):
			events["blocklist"]++
		case strings.Contains(line, "reconnect") && strings.Contains(line, "ceph"):
			events["reconnect"]++
		case strings.Contains(line, "invalid context") && strings.Contains(line, "ceph"):
			events["selinux_invalid_context"]++
		}
	}
	return events
}

func collectCephHealth() model.CephHealthMetrics {
	start := time.Now()
	m := model.CephHealthMetrics{Collector: "ceph_health", Success: false, Status: "unknown"}
	if _, err := exec.LookPath("ceph"); err != nil {
		m.Error = "ceph command not found"
		m.DurationSeconds = time.Since(start).Seconds()
		return m
	}
	out, err := runCommand(commandTimeout, "ceph", "health")
	m.DurationSeconds = time.Since(start).Seconds()
	if err != nil {
		m.Error = err.Error()
		return m
	}
	m.Status = ParseCephHealth(out)
	m.Success = true
	return m
}

// ParseCephHealth extracts the first health token from ceph health output.
func ParseCephHealth(input string) string {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[0]
}

func collectSlurmJobs() model.SlurmMetrics {
	start := time.Now()
	m := model.SlurmMetrics{Collector: "slurm_jobs", Success: false, States: map[string]int64{}}
	if _, err := exec.LookPath("squeue"); err != nil {
		m.Error = "squeue command not found"
		m.DurationSeconds = time.Since(start).Seconds()
		return m
	}
	out, err := runCommand(commandTimeout, "squeue", "-h", "-o", "%T")
	m.DurationSeconds = time.Since(start).Seconds()
	if err != nil {
		m.Error = err.Error()
		return m
	}
	m.States = ParseSlurmStates(out)
	m.Success = true
	return m
}

// ParseSlurmStates counts jobs by state from one-state-per-line squeue output.
func ParseSlurmStates(input string) map[string]int64 {
	states := map[string]int64{}
	for _, line := range strings.Split(input, "\n") {
		state := strings.ToLower(strings.TrimSpace(line))
		if state == "" {
			continue
		}
		states[state]++
	}
	return states
}

func runShell(timeout time.Duration, script string) (string, error) {
	return runCommand(timeout, "sh", "-c", script)
}

func runCommand(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%s timed out after %s", name, timeout)
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func collectorSample(prefix string, b *strings.Builder, collector string, success bool, duration float64, errMsg string) {
	name := sanitize(collector)
	b.WriteString(fmt.Sprintf("%s_collector_success{collector=\"%s\"} %d\n", prefix, name, boolInt(success)))
	b.WriteString(fmt.Sprintf("%s_collector_duration_seconds{collector=\"%s\"} %.6f\n", prefix, name, duration))
	if errMsg != "" {
		b.WriteString(fmt.Sprintf("%s_collector_error{collector=\"%s\",error=\"%s\"} 1\n", prefix, name, sanitize(errMsg)))
	}
}

func sanitize(value string) string {
	v := strings.ReplaceAll(value, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Prom renders monitor metrics using stable cephilis_* metric names.
func Prom(result model.MonitorResult) string {
	prefix := config.MetricsPrefix
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# HELP %s_caps_total CephFS client caps total from debugfs.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_caps_total gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_caps_total %d\n", prefix, result.Caps.Total))
	b.WriteString(fmt.Sprintf("# HELP %s_caps_used CephFS client caps used from debugfs.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_caps_used gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_caps_used %d\n", prefix, result.Caps.Used))
	b.WriteString(fmt.Sprintf("# HELP %s_caps_available CephFS client caps available from debugfs.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_caps_available gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_caps_available %d\n\n", prefix, result.Caps.Available))

	b.WriteString(fmt.Sprintf("# HELP %s_kernel_events_total Kernel event counters relevant to CephFS.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_kernel_events_total counter\n", prefix))
	for event, count := range result.Kernel.Events {
		b.WriteString(fmt.Sprintf("%s_kernel_events_total{event=\"%s\"} %d\n", prefix, sanitize(event), count))
	}
	b.WriteString("\n")

	cephOK := result.Ceph.Status == "HEALTH_OK"
	b.WriteString(fmt.Sprintf("# HELP %s_ceph_health_ok Ceph health status is HEALTH_OK.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_ceph_health_ok gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_ceph_health_ok{status=\"%s\"} %d\n\n", prefix, sanitize(result.Ceph.Status), boolInt(cephOK)))

	b.WriteString(fmt.Sprintf("# HELP %s_slurm_jobs Slurm jobs by state.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_slurm_jobs gauge\n", prefix))
	for state, count := range result.Slurm.States {
		b.WriteString(fmt.Sprintf("%s_slurm_jobs{state=\"%s\"} %d\n", prefix, sanitize(state), count))
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("# HELP %s_collector_success Collector completed successfully.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_collector_success gauge\n", prefix))
	b.WriteString(fmt.Sprintf("# HELP %s_collector_duration_seconds Collector runtime in seconds.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_collector_duration_seconds gauge\n", prefix))
	b.WriteString(fmt.Sprintf("# HELP %s_collector_error Collector error label for failed collectors.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_collector_error gauge\n", prefix))
	collectorSample(prefix, &b, result.Caps.Collector, result.Caps.Success, result.Caps.DurationSeconds, result.Caps.Error)
	collectorSample(prefix, &b, result.Kernel.Collector, result.Kernel.Success, result.Kernel.DurationSeconds, result.Kernel.Error)
	collectorSample(prefix, &b, result.Ceph.Collector, result.Ceph.Success, result.Ceph.DurationSeconds, result.Ceph.Error)
	collectorSample(prefix, &b, result.Slurm.Collector, result.Slurm.Success, result.Slurm.DurationSeconds, result.Slurm.Error)
	b.WriteString("\n")
	return b.String()
}
