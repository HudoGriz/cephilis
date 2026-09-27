// Package model defines the shared data structures used across the cephilis
// collector, health checker, and output formatters.
package model

import "time"

// Dir holds the CephFS recursive statistics of one directory, read from its
// ceph.dir.* and ceph.quota.* extended attributes.
type Dir struct {
	Path   string
	Parent string
	// Owner is the user name (or numeric uid) owning the directory; empty
	// unless owner reporting is enabled for its configured path.
	Owner string
	// Bytes and Files are recursive (ceph.dir.rbytes / ceph.dir.rfiles).
	Bytes int64
	Files int64
	// RCTime is the most recent change time anywhere below the directory
	// (ceph.dir.rctime), in Unix seconds.
	RCTime float64
	// QuotaBytes / QuotaFiles are ceph.quota.max_*; 0 means no quota.
	QuotaBytes int64
	QuotaFiles int64
	// Subdirs and Entries are immediate counts (ceph.dir.subdirs / entries),
	// used to decide whether listing the directory is worthwhile and safe.
	Subdirs int64
	Entries int64
}

// RootResult is the scan outcome for one configured path.
type RootResult struct {
	Path string
	// Dirs is the number of directories reported for this path.
	Dirs int
	// Truncated is set when max_dirs was hit or a directory was too large to
	// list (max_entries), so the tree below it is incomplete.
	Truncated bool
	// FailedDirs counts directories whose xattrs could not be read.
	FailedDirs int
	// Err is set when the configured path itself could not be read.
	Err error
}

// Scan is the result of one space scan over all configured paths.
type Scan struct {
	Dirs     []Dir
	Roots    []RootResult
	Started  time.Time
	Duration time.Duration
	// XattrReads counts ceph.* xattr reads; each is one MDS round trip.
	XattrReads int64
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
