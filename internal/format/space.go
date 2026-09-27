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

	// Section level: scan status, skipped directories, and the section root's
	// own recursive totals (includes files directly in the root).
	sectionMetric := func(name, help string, value func(model.SectionResult) (int64, bool)) {
		metric := config.MetricsPrefix + name
		b.WriteString(fmt.Sprintf("# HELP %s %s\n", metric, help))
		b.WriteString(fmt.Sprintf("# TYPE %s gauge\n", metric))
		for _, section := range results {
			v, ok := value(section)
			if !ok {
				continue
			}
			sec := sanitizePromLabel(strings.ToLower(strings.ReplaceAll(section.Name, " ", "_")))
			b.WriteString(fmt.Sprintf("%s{section=\"%s\",parent=\"%s\"} %d\n", metric, sec, sanitizePromLabel(section.ParentPath), v))
		}
		b.WriteString("\n")
	}
	sectionMetric("_section_scan_ok", "Section root was scanned, 1 yes, 0 no.", func(s model.SectionResult) (int64, bool) {
		if s.Err != nil {
			return 0, true
		}
		return 1, true
	})
	sectionMetric("_section_failed_dirs", "Subdirectories skipped because their CephFS xattrs were unreadable.", func(s model.SectionResult) (int64, bool) {
		return int64(s.FailedDirs), s.Err == nil
	})
	sectionMetric("_section_size_bytes", "Recursive size of the section root in bytes via CephFS xattr.", func(s model.SectionResult) (int64, bool) {
		if s.Parent == nil {
			return 0, false
		}
		return s.Parent.RBytes, true
	})
	sectionMetric("_section_files_total", "Recursive file count of the section root via CephFS xattr.", func(s model.SectionResult) (int64, bool) {
		if s.Parent == nil {
			return 0, false
		}
		return s.Parent.RFiles, true
	})
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
