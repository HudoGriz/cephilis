// Package model defines the shared data structures used across the cephilis
// collector, health checker, and output formatters.
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
