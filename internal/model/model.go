// Package model defines the shared data structures used across the cephilis
// collector, mount checker, and output formatters.
package model

// DirStats holds the CephFS space and file-count statistics for a single
// directory, obtained either from CephFS extended attributes or via du(1).
type DirStats struct {
	// Path is the absolute filesystem path of the directory.
	Path string
	// Name is the base name of the directory (used in output).
	Name string
	// RBytes is the recursive byte usage reported by CephFS or du.
	RBytes int64
	// RFiles is the recursive file count (0 when obtained via du fallback).
	RFiles int64
	// RSubdirs is the recursive sub-directory count (0 when obtained via du fallback).
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
}

// MountResult holds the outcome of a CephFS mount health check.
type MountResult struct {
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
	// LatencySeconds is the probe round-trip time in seconds; nil when the probe was not attempted.
	LatencySeconds *float64
	// Error contains a diagnostic message when the check failed, empty otherwise.
	Error string
}

// HasError reports whether the mount check ended in an error state.
func (m MountResult) HasError() bool {
	return m.Error != ""
}
