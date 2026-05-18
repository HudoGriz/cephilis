// Package format renders collector and mount results into various output
// formats: plain text table, CSV, TSV, JSON, and Prometheus textfile.
package format

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
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

// Space formats the collector results using the named format.
// Valid formatName values: "table", "csv", "tsv", "json", "prom".
func Space(formatName string, results []model.SectionResult) (string, error) {
	switch formatName {
	case "table":
		return spaceTable(results), nil
	case "csv":
		return spaceCSV(results), nil
	case "tsv":
		return spaceTSV(results), nil
	case "json":
		return spaceJSON(results)
	case "prom":
		return spaceProm(results), nil
	default:
		return "", fmt.Errorf("unknown format: %s", formatName)
	}
}

func spaceProm(results []model.SectionResult) string {
	var b strings.Builder
	sizeMetric := config.MetricsPrefix + "_dir_size_bytes"
	filesMetric := config.MetricsPrefix + "_dir_files_total"

	b.WriteString(fmt.Sprintf("# HELP %s Recursive directory size in bytes via CephFS xattr\n", sizeMetric))
	b.WriteString(fmt.Sprintf("# TYPE %s gauge\n", sizeMetric))
	for _, section := range results {
		sec := sanitizePromLabel(strings.ToLower(strings.ReplaceAll(section.Name, " ", "_")))
		parent := sanitizePromLabel(section.ParentPath)
		for _, e := range section.Entries {
			b.WriteString(fmt.Sprintf(
				"%s{section=\"%s\",parent=\"%s\",name=\"%s\"} %d\n",
				sizeMetric, sec, parent, sanitizePromLabel(e.Name), e.RBytes,
			))
		}
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# HELP %s Recursive file count via CephFS xattr\n", filesMetric))
	b.WriteString(fmt.Sprintf("# TYPE %s gauge\n", filesMetric))
	for _, section := range results {
		sec := sanitizePromLabel(strings.ToLower(strings.ReplaceAll(section.Name, " ", "_")))
		parent := sanitizePromLabel(section.ParentPath)
		for _, e := range section.Entries {
			b.WriteString(fmt.Sprintf(
				"%s{section=\"%s\",parent=\"%s\",name=\"%s\"} %d\n",
				filesMetric, sec, parent, sanitizePromLabel(e.Name), e.RFiles,
			))
		}
	}
	b.WriteString("\n")
	return b.String()
}

func spaceCSV(results []model.SectionResult) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"section", "parent", "name", "size_bytes", "files", "percent"})
	for _, section := range results {
		parent := int64(0)
		if section.Parent != nil {
			parent = section.Parent.RBytes
		}
		for _, e := range section.Entries {
			_ = w.Write([]string{
				section.Name,
				section.ParentPath,
				e.Name,
				fmt.Sprintf("%d", e.RBytes),
				fmt.Sprintf("%d", e.RFiles),
				fmt.Sprintf("%.2f", percentOfParent(e.RBytes, parent)),
			})
		}
	}
	w.Flush()
	return b.String()
}

func spaceTSV(results []model.SectionResult) string {
	var b strings.Builder
	b.WriteString("section\tparent\tname\tsize_bytes\tfiles\tpercent\n")
	for _, section := range results {
		parent := int64(0)
		if section.Parent != nil {
			parent = section.Parent.RBytes
		}
		for _, e := range section.Entries {
			b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%.2f\n",
				section.Name, section.ParentPath, e.Name, e.RBytes, e.RFiles, percentOfParent(e.RBytes, parent)))
		}
	}
	return b.String()
}

func spaceJSON(results []model.SectionResult) (string, error) {
	type entry struct {
		Name      string  `json:"name"`
		Path      string  `json:"path"`
		SizeBytes int64   `json:"size_bytes"`
		Files     int64   `json:"files"`
		Percent   float64 `json:"percent"`
	}
	type section struct {
		Name            string  `json:"name"`
		ParentPath      string  `json:"parent_path"`
		ParentSizeBytes int64   `json:"parent_size_bytes"`
		ParentFiles     int64   `json:"parent_files"`
		Entries         []entry `json:"entries"`
	}
	payload := struct {
		Timestamp string    `json:"timestamp"`
		Sections  []section `json:"sections"`
	}{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Sections:  make([]section, 0, len(results)),
	}

	for _, s := range results {
		parentSize := int64(0)
		parentFiles := int64(0)
		if s.Parent != nil {
			parentSize = s.Parent.RBytes
			parentFiles = s.Parent.RFiles
		}
		items := make([]entry, 0, len(s.Entries))
		for _, e := range s.Entries {
			items = append(items, entry{
				Name:      e.Name,
				Path:      e.Path,
				SizeBytes: e.RBytes,
				Files:     e.RFiles,
				Percent:   percentOfParent(e.RBytes, parentSize),
			})
		}
		payload.Sections = append(payload.Sections, section{
			Name:            s.Name,
			ParentPath:      s.ParentPath,
			ParentSizeBytes: parentSize,
			ParentFiles:     parentFiles,
			Entries:         items,
		})
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func spaceTable(results []model.SectionResult) string {
	var b strings.Builder
	now := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	b.WriteString("\n")
	b.WriteString("== Cephilis Space Usage Report - " + now + " ==\n\n")
	for _, section := range results {
		parent := int64(0)
		if section.Parent != nil {
			parent = section.Parent.RBytes
		}
		colName, colSize, colFiles := len("NAME"), len("SIZE"), len("FILES")
		for _, e := range section.Entries {
			if n := len(e.Name); n > colName {
				colName = n
			}
			if n := len(humanReadable(e.RBytes)); n > colSize {
				colSize = n
			}
			if n := len(fmt.Sprintf("%d", e.RFiles)); n > colFiles {
				colFiles = n
			}
		}
		b.WriteString(section.Name + " (" + section.ParentPath + ")\n")
		b.WriteString(fmt.Sprintf("%-*s  %-*s  %-*s  %s\n", colName, "NAME", colSize, "SIZE", colFiles, "FILES", "PERCENT"))
		for _, e := range section.Entries {
			pct := percentOfParent(e.RBytes, parent)
			b.WriteString(fmt.Sprintf("%-*s  %-*s  %-*d  %s %.1f%%\n",
				colName, e.Name, colSize, humanReadable(e.RBytes), colFiles, e.RFiles, barChart(pct, 18), pct,
			))
		}
		b.WriteString("\n")
	}
	b.WriteString("O(1) CephFS xattr queries - no filesystem traversal\n")
	return b.String()
}

// Mount formats a MountResult using the named format.
// Valid formatName values: "table", "json", "prom".
// When compat is true and m.MountPath is "/home", legacy
// mount_home_ok / mount_home_responsive metrics are appended to prom output.
func Mount(formatName string, m model.MountResult, compat bool) (string, error) {
	switch formatName {
	case "table":
		return mountTable(m), nil
	case "json":
		return mountJSON(m)
	case "prom":
		return mountProm(m, compat), nil
	default:
		return "", fmt.Errorf("unsupported mount format: %s", formatName)
	}
}

func mountTable(m model.MountResult) string {
	lat := "N/A"
	if m.LatencySeconds != nil {
		lat = fmt.Sprintf("%.1f ms", *m.LatencySeconds*1000)
	}
	status := "DEGRADED"
	if m.Mounted && m.Responsive && m.FSTypeMatches {
		status = "OK"
	}
	var b strings.Builder
	b.WriteString("Mount Health\n")
	b.WriteString("- status: " + status + "\n")
	b.WriteString("- hostname: " + m.Hostname + "\n")
	b.WriteString("- mount: " + m.MountPath + "\n")
	b.WriteString(fmt.Sprintf("- mounted: %t\n", m.Mounted))
	if m.ExpectedFSType != "" {
		b.WriteString("- expected_fstype: " + m.ExpectedFSType + "\n")
		b.WriteString("- actual_fstype: " + m.ActualFSType + "\n")
		b.WriteString(fmt.Sprintf("- fstype_matches: %t\n", m.FSTypeMatches))
	}
	b.WriteString("- probe: " + m.ProbeFile + "\n")
	b.WriteString(fmt.Sprintf("- responsive: %t\n", m.Responsive))
	b.WriteString("- latency: " + lat + "\n")
	if m.HasError() {
		b.WriteString("- error: " + m.Error + "\n")
	}
	return b.String()
}

func mountJSON(m model.MountResult) (string, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func mountProm(m model.MountResult, compat bool) string {
	prefix := config.MetricsPrefix
	mount := sanitizePromLabel(m.MountPath)
	host := sanitizePromLabel(m.Hostname)
	probe := sanitizePromLabel(m.ProbeFile)
	name := sanitizePromLabel(m.Name)
	lat := 0.0
	if m.LatencySeconds != nil {
		lat = *m.LatencySeconds
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# HELP %s_mount_up Mount point is currently mounted.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_up gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_mount_up{name=\"%s\",mount=\"%s\",hostname=\"%s\",required=\"%t\"} %d\n\n", prefix, name, mount, host, m.Required, btoi(m.Mounted)))
	b.WriteString(fmt.Sprintf("# HELP %s_mount_responsive Probe file stat succeeded within timeout.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_responsive gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_mount_responsive{name=\"%s\",mount=\"%s\",probe=\"%s\",hostname=\"%s\",required=\"%t\"} %d\n\n", prefix, name, mount, probe, host, m.Required, btoi(m.Responsive)))
	b.WriteString(fmt.Sprintf("# HELP %s_mount_fstype_match Filesystem type matches expected_fstype when configured.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_fstype_match gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_mount_fstype_match{name=\"%s\",mount=\"%s\",hostname=\"%s\",expected=\"%s\",actual=\"%s\",required=\"%t\"} %d\n\n",
		prefix, name, mount, host, sanitizePromLabel(m.ExpectedFSType), sanitizePromLabel(m.ActualFSType), m.Required, btoi(m.FSTypeMatches)))
	b.WriteString(fmt.Sprintf("# HELP %s_mount_probe_latency_seconds Probe stat latency in seconds.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_probe_latency_seconds gauge\n", prefix))
	b.WriteString(fmt.Sprintf("%s_mount_probe_latency_seconds{name=\"%s\",mount=\"%s\",probe=\"%s\",hostname=\"%s\"} %.6f\n", prefix, name, mount, probe, host, lat))

	if compat && m.MountPath == "/home" {
		b.WriteString("\n# HELP mount_home_ok Ceph /home mount status (1=mounted, 0=not mounted)\n")
		b.WriteString("# TYPE mount_home_ok gauge\n")
		b.WriteString(fmt.Sprintf("mount_home_ok{mount=\"%s\",hostname=\"%s\"} %d\n\n", mount, host, btoi(m.Mounted)))
		b.WriteString("# HELP mount_home_responsive Ceph /home responds to stat within timeout (1=ok, 0=hung/unmounted)\n")
		b.WriteString("# TYPE mount_home_responsive gauge\n")
		b.WriteString(fmt.Sprintf("mount_home_responsive{mount=\"%s\",hostname=\"%s\"} %d\n", mount, host, btoi(m.Responsive)))
	}

	if m.HasError() {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("# HELP %s_mount_error Mount check finished with an error.\n", prefix))
		b.WriteString(fmt.Sprintf("# TYPE %s_mount_error gauge\n", prefix))
		b.WriteString(fmt.Sprintf("%s_mount_error{mount=\"%s\",hostname=\"%s\",error=\"%s\"} 1\n", prefix, mount, host, sanitizePromLabel(m.Error)))
	}

	return b.String()
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}
