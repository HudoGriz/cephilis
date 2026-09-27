// Package format renders space scans into text formats: a human-readable
// table, CSV, TSV, JSON, and Prometheus text exposition.
package format

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

var valid = map[string]struct{}{
	"table": {},
	"csv":   {},
	"tsv":   {},
	"json":  {},
	"prom":  {},
}

// IsValid reports whether name is a recognised space format identifier.
func IsValid(name string) bool {
	_, ok := valid[name]
	return ok
}

// Names returns the sorted list of valid space format identifiers.
func Names() []string {
	keys := make([]string, 0, len(valid))
	for k := range valid {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Space formats a scan using the named format.
func Space(formatName string, s model.Scan) (string, error) {
	switch formatName {
	case "table":
		return spaceTable(s), nil
	case "csv":
		return spaceDelimited(s, ','), nil
	case "tsv":
		return spaceDelimited(s, '\t'), nil
	case "json":
		return spaceJSON(s)
	case "prom":
		return spaceProm(s), nil
	default:
		return "", fmt.Errorf("unknown format: %s", formatName)
	}
}

func dirLabels(d model.Dir) string {
	l := fmt.Sprintf(`path="%s",parent="%s"`, sanitizePromLabel(d.Path), sanitizePromLabel(d.Parent))
	if d.Owner != "" {
		l += fmt.Sprintf(`,owner="%s"`, sanitizePromLabel(d.Owner))
	}
	return l
}

func spaceProm(s model.Scan) string {
	var b strings.Builder
	p := config.MetricsPrefix
	family := func(name, help string, value func(model.Dir) (string, bool)) {
		fmt.Fprintf(&b, "# HELP %s_%s %s\n# TYPE %s_%s gauge\n", p, name, help, p, name)
		for _, d := range s.Dirs {
			if v, ok := value(d); ok {
				fmt.Fprintf(&b, "%s_%s{%s} %s\n", p, name, dirLabels(d), v)
			}
		}
	}
	i64 := func(v int64) string { return strconv.FormatInt(v, 10) }
	family("dir_bytes", "Recursive size of the directory in bytes (ceph.dir.rbytes).",
		func(d model.Dir) (string, bool) { return i64(d.Bytes), true })
	family("dir_files", "Recursive file count of the directory (ceph.dir.rfiles).",
		func(d model.Dir) (string, bool) { return i64(d.Files), true })
	family("dir_rctime_seconds", "Latest change time anywhere below the directory, Unix seconds (ceph.dir.rctime).",
		func(d model.Dir) (string, bool) {
			return strconv.FormatFloat(d.RCTime, 'f', 3, 64), d.RCTime > 0
		})
	family("dir_quota_bytes", "Byte quota of the directory (ceph.quota.max_bytes); only directories with a quota.",
		func(d model.Dir) (string, bool) { return i64(d.QuotaBytes), d.QuotaBytes > 0 })
	family("dir_quota_files", "File quota of the directory (ceph.quota.max_files); only directories with a quota.",
		func(d model.Dir) (string, bool) { return i64(d.QuotaFiles), d.QuotaFiles > 0 })

	root := func(name, help string, value func(model.RootResult) int64) {
		fmt.Fprintf(&b, "# HELP %s_%s %s\n# TYPE %s_%s gauge\n", p, name, help, p, name)
		for _, r := range s.Roots {
			fmt.Fprintf(&b, "%s_%s{root=\"%s\"} %d\n", p, name, sanitizePromLabel(r.Path), value(r))
		}
	}
	b2i := func(v bool) int64 {
		if v {
			return 1
		}
		return 0
	}
	root("root_scan_ok", "Configured path could be read, 1 yes, 0 no.", func(r model.RootResult) int64 { return b2i(r.Err == nil) })
	root("root_dirs", "Directories reported for the configured path.", func(r model.RootResult) int64 { return int64(r.Dirs) })
	root("root_truncated", "Tree below the path is incomplete (max_dirs or max_entries hit), 1 yes, 0 no.", func(r model.RootResult) int64 { return b2i(r.Truncated) })
	root("root_failed_dirs", "Directories whose CephFS xattrs could not be read.", func(r model.RootResult) int64 { return int64(r.FailedDirs) })

	fmt.Fprintf(&b, "# HELP %s_scan_duration_seconds Duration of the space scan.\n# TYPE %s_scan_duration_seconds gauge\n%s_scan_duration_seconds %.3f\n", p, p, p, s.Duration.Seconds())
	fmt.Fprintf(&b, "# HELP %s_scan_xattr_reads CephFS xattr reads in the scan; each is one MDS request.\n# TYPE %s_scan_xattr_reads gauge\n%s_scan_xattr_reads %d\n", p, p, p, s.XattrReads)
	fmt.Fprintf(&b, "# HELP %s_scan_timestamp_seconds Start time of the space scan, Unix seconds.\n# TYPE %s_scan_timestamp_seconds gauge\n%s_scan_timestamp_seconds %d\n", p, p, p, s.Started.Unix())
	return b.String()
}

func rctime(d model.Dir) string {
	if d.RCTime <= 0 {
		return ""
	}
	return time.Unix(int64(d.RCTime), 0).UTC().Format("2006-01-02")
}

func quotaPercent(d model.Dir) string {
	if d.QuotaBytes <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f", 100*float64(d.Bytes)/float64(d.QuotaBytes))
}

func spaceDelimited(s model.Scan, sep rune) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = sep
	_ = w.Write([]string{"path", "parent", "owner", "bytes", "files", "rctime", "quota_bytes", "quota_files"})
	for _, d := range s.Dirs {
		_ = w.Write([]string{d.Path, d.Parent, d.Owner,
			strconv.FormatInt(d.Bytes, 10), strconv.FormatInt(d.Files, 10), rctime(d),
			strconv.FormatInt(d.QuotaBytes, 10), strconv.FormatInt(d.QuotaFiles, 10)})
	}
	w.Flush()
	return b.String()
}

func spaceJSON(s model.Scan) (string, error) {
	type dir struct {
		Path       string  `json:"path"`
		Parent     string  `json:"parent"`
		Owner      string  `json:"owner,omitempty"`
		Bytes      int64   `json:"bytes"`
		Files      int64   `json:"files"`
		RCTime     float64 `json:"rctime,omitempty"`
		QuotaBytes int64   `json:"quota_bytes,omitempty"`
		QuotaFiles int64   `json:"quota_files,omitempty"`
	}
	type root struct {
		Path       string `json:"path"`
		Dirs       int    `json:"dirs"`
		Truncated  bool   `json:"truncated"`
		FailedDirs int    `json:"failed_dirs"`
		Error      string `json:"error,omitempty"`
	}
	payload := struct {
		Timestamp       string  `json:"timestamp"`
		DurationSeconds float64 `json:"duration_seconds"`
		Roots           []root  `json:"roots"`
		Dirs            []dir   `json:"dirs"`
	}{Timestamp: s.Started.UTC().Format(time.RFC3339), DurationSeconds: s.Duration.Seconds()}
	for _, r := range s.Roots {
		e := ""
		if r.Err != nil {
			e = r.Err.Error()
		}
		payload.Roots = append(payload.Roots, root{r.Path, r.Dirs, r.Truncated, r.FailedDirs, e})
	}
	for _, d := range s.Dirs {
		payload.Dirs = append(payload.Dirs, dir{d.Path, d.Parent, d.Owner, d.Bytes, d.Files, d.RCTime, d.QuotaBytes, d.QuotaFiles})
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// spaceTable prints one block per directory that has reported children:
// the children with size, share of the parent, files, last change and quota.
func spaceTable(s model.Scan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n== cephilis space report - %s ==\n\n", s.Started.UTC().Format("2006-01-02 15:04:05 UTC"))
	byPath := map[string]model.Dir{}
	children := map[string][]model.Dir{}
	for _, d := range s.Dirs {
		byPath[d.Path] = d
		children[d.Parent] = append(children[d.Parent], d)
	}
	parents := make([]string, 0)
	for p := range children {
		if _, ok := byPath[p]; ok {
			parents = append(parents, p)
		}
	}
	sort.Strings(parents)
	for _, p := range parents {
		parent := byPath[p]
		kids := children[p]
		sort.Slice(kids, func(i, j int) bool { return kids[i].Bytes > kids[j].Bytes })
		fmt.Fprintf(&b, "%s  %s, %d files\n", p, humanReadable(parent.Bytes), parent.Files)
		for _, d := range kids {
			pct := percentOfParent(d.Bytes, parent.Bytes)
			quota := ""
			if q := quotaPercent(d); q != "" {
				quota = "  quota " + q + "%"
			}
			fmt.Fprintf(&b, "  %-40s %10s  %s %5.1f%%  %12d files  changed %s%s\n",
				truncate(d.Path[len(p):], 40), humanReadable(d.Bytes), barChart(pct, 12), pct, d.Files, rctime(d), quota)
		}
		b.WriteString("\n")
	}
	for _, r := range s.Roots {
		switch {
		case r.Err != nil:
			fmt.Fprintf(&b, "! %s: %v\n", r.Path, r.Err)
		case r.Truncated:
			fmt.Fprintf(&b, "! %s: tree truncated (max_dirs/max_entries)\n", r.Path)
		}
	}
	fmt.Fprintf(&b, "%d directories in %.2fs via CephFS xattrs - no tree walk\n", len(s.Dirs), s.Duration.Seconds())
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}
