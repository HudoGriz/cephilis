// Package model defines the shared data structures used across the cephilis
// collector, mount checker, and output formatters.
package model

// DirStats holds the CephFS space and file-count statistics for a single
// directory, obtained from CephFS extended attributes.
type DirStats struct {
	// Path is the absolute filesystem path of the directory.
	Path string
	// Name is the base name of the directory (used in output).
	Name string
	// RBytes is the recursive byte usage (ceph.dir.rbytes).
	RBytes int64
	// RFiles is the recursive file count (ceph.dir.rfiles).
	RFiles int64
	// RSubdirs is the recursive sub-directory count (ceph.dir.rsubdirs).
	RSubdirs int64
}

// SectionResult holds the scan output for a single configured section.
type SectionResult struct {
	// Name is the human-readable section label from the config file.
	Name string
	// ParentPath is the root path of this section.
	ParentPath string
	// Parent holds the aggregate statistics for the section root itself.
	Parent *DirStats
	// Entries holds per-subdirectory statistics, sorted descending by RBytes.
	Entries []DirStats
	// FailedDirs counts subdirectories skipped because their stats were unreadable.
	FailedDirs int
	// Err is set when the section root itself could not be scanned.
	Err error
}

// MountResult holds the outcome of a CephFS mount health check.
type MountResult struct {
	// Name is the configured mount label.
	Name string
	// Hostname is the short hostname of the machine that ran the check.
	Hostname string
	// MountPath is the filesystem path that was checked for mount status.
	MountPath string
	// ProbeFile is the path used for the responsive-stat probe.
	ProbeFile string
	// Mounted is true when mountpoint(1) reports the path as mounted.
	Mounted bool
	// Responsive is true when a stat(1) probe on ProbeFile succeeded within the timeout.
	Responsive bool
	// Required is true when this mount must be healthy for cephilis health to pass.
	Required bool
	// ExpectedFSType is the filesystem type configured for this mount.
	ExpectedFSType string
	// ActualFSType is the filesystem type reported by stat -f -c %T.
	ActualFSType string
	// FSTypeMatches is true when ExpectedFSType is empty or ActualFSType matches it.
	FSTypeMatches bool
	// LatencySeconds is the probe round-trip time in seconds; nil when the probe was not attempted.
	LatencySeconds *float64
	// Error contains a diagnostic message when the check failed, empty otherwise.
	Error string
}

// HasError reports whether the mount check ended in an error state.
func (m MountResult) HasError() bool {
	return m.Error != ""
}

// HealthResult holds a non-blocking CephFS client health check. It is intended
// for Slurm and alerting paths that must not touch filesystem metadata under
// the checked mount.
type HealthResult struct {
	Name             string   `json:"name,omitempty"`
	Hostname         string   `json:"hostname"`
	MountPath        string   `json:"mount"`
	Mounted          bool     `json:"mounted"`
	ExpectedFSType   string   `json:"expected_fstype,omitempty"`
	ActualFSType     string   `json:"actual_fstype,omitempty"`
	FSTypeMatches    bool     `json:"fstype_matches"`
	MDSSessionOpen   bool     `json:"mds_session_open"`
	UnhealthySession bool     `json:"unhealthy_session"`
	Sessions         []string `json:"sessions,omitempty"`
	SessionFiles     []string `json:"session_files,omitempty"`
	Healthy          bool     `json:"healthy"`
	Error            string   `json:"error,omitempty"`
}

// CapsMetrics holds CephFS client cap counts from debugfs.
type CapsMetrics struct {
	Collector       string
	Success         bool
	DurationSeconds float64
	Error           string
	Total           int64
	Used            int64
	Available       int64
}

// KernelEventMetrics holds kernel message counters for CephFS incidents.
type KernelEventMetrics struct {
	Collector       string
	Success         bool
	DurationSeconds float64
	Error           string
	Events          map[string]int64
}

// CephHealthMetrics holds the optional ceph health status.
type CephHealthMetrics struct {
	Collector       string
	Success         bool
	DurationSeconds float64
	Error           string
	Status          string
}

// SlurmMetrics holds optional Slurm queue counts by state.
type SlurmMetrics struct {
	Collector       string
	Success         bool
	DurationSeconds float64
	Error           string
	States          map[string]int64
}

// MonitorResult groups best-effort non-space monitoring signals.
type MonitorResult struct {
	Caps   CapsMetrics
	Kernel KernelEventMetrics
	Ceph   CephHealthMetrics
	Slurm  SlurmMetrics
}
