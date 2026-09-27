package mount

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

const (
	defaultCephDebugGlob = "/sys/kernel/debug/ceph/*.client*/mds_sessions"
	badSessionStates     = "stale|blocklist|reconnect|closing|closed|evict|rejected|denied"
)

// CheckHealth runs a non-blocking CephFS client health check. It reads only the
// local mount table and Ceph kernel debugfs state; it deliberately does not
// stat, list, read, or write the checked mount.
func CheckHealth(m config.Mount) model.HealthResult {
	hostname, _ := os.Hostname()
	if idx := strings.IndexByte(hostname, '.'); idx > 0 {
		hostname = hostname[:idx]
	}

	expected := strings.TrimSpace(m.ExpectedFSType)
	if expected == "" {
		expected = "ceph"
	}

	result := model.HealthResult{
		Name:           m.Name,
		Hostname:       hostname,
		MountPath:      filepath.Clean(m.Path),
		ExpectedFSType: expected,
		FSTypeMatches:  false,
	}

	fstype, mounted, err := mountFSType(result.MountPath, expected)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Mounted = mounted
	result.ActualFSType = fstype
	result.FSTypeMatches = strings.EqualFold(fstype, expected)
	if !mounted {
		result.Error = fmt.Sprintf("mount path is not mounted: %s", result.MountPath)
		return result
	}
	if !result.FSTypeMatches {
		result.Error = fmt.Sprintf("mount %s fstype mismatch: got %s, want %s", result.MountPath, fstype, expected)
		return result
	}

	sessions, files, err := readMDSSessions(debugGlob())
	result.SessionFiles = files
	result.Sessions = sessions
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(sessions) == 0 {
		result.Error = "no CephFS client mds_sessions found in debugfs"
		return result
	}

	for _, line := range sessions {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "mds.") && fields[1] == "open" {
			result.MDSSessionOpen = true
		}
		if hasBadSessionState(line) {
			result.UnhealthySession = true
		}
	}
	if !result.MDSSessionOpen {
		result.Error = "no open CephFS MDS session found"
		return result
	}
	if result.UnhealthySession {
		result.Error = "CephFS client session unhealthy"
		return result
	}

	result.Healthy = true
	return result
}

// CheckHealthAll runs CheckHealth against every mount sequentially.
func CheckHealthAll(mounts []config.Mount) []model.HealthResult {
	out := make([]model.HealthResult, 0, len(mounts))
	for _, m := range mounts {
		out = append(out, CheckHealth(m))
	}
	return out
}

func mountFSType(path, preferredFSType string) (string, bool, error) {
	mountInfoPath := strings.TrimSpace(os.Getenv("CEPHILIS_MOUNTINFO_PATH"))
	if mountInfoPath == "" {
		mountInfoPath = "/proc/self/mountinfo"
	}
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return "", false, fmt.Errorf("read mountinfo %s: %w", mountInfoPath, err)
	}
	defer f.Close()
	return mountFSTypeFromScanner(bufio.NewScanner(f), path, preferredFSType)
}

func mountFSTypeFromScanner(scanner *bufio.Scanner, path, preferredFSType string) (string, bool, error) {
	cleanPath := filepath.Clean(path)
	firstFSType := ""
	for scanner.Scan() {
		line := scanner.Text()
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		leftFields := strings.Fields(left)
		rightFields := strings.Fields(right)
		if len(leftFields) < 5 || len(rightFields) < 1 {
			continue
		}
		mountPoint := unescapeMountInfo(leftFields[4])
		if filepath.Clean(mountPoint) == cleanPath {
			fstype := rightFields[0]
			if firstFSType == "" {
				firstFSType = fstype
			}
			if strings.EqualFold(fstype, preferredFSType) {
				return fstype, true, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("scan mountinfo: %w", err)
	}
	if firstFSType != "" {
		return firstFSType, true, nil
	}
	return "", false, nil
}

func readMDSSessions(glob string) ([]string, []string, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid debugfs glob: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, nil, nil
	}

	var lines []string
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return lines, files, fmt.Errorf("read %s: %w", file, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines, files, nil
}

func debugGlob() string {
	if glob := strings.TrimSpace(os.Getenv("CEPHILIS_CEPH_DEBUGFS_GLOB")); glob != "" {
		return glob
	}
	return defaultCephDebugGlob
}

func hasBadSessionState(line string) bool {
	lower := strings.ToLower(line)
	for _, bad := range strings.Split(badSessionStates, "|") {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

func unescapeMountInfo(s string) string {
	for _, repl := range [][2]string{
		{`\\`, `\`},
		{`\040`, ` `},
		{`\011`, "\t"},
		{`\012`, "\n"},
	} {
		s = strings.ReplaceAll(s, repl[0], repl[1])
	}
	return s
}
