package format

import (
	"fmt"
	"strings"
)

func sanitizePromLabel(value string) string {
	v := strings.ReplaceAll(value, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func humanReadable(size int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	if size == 0 {
		return "0 B"
	}
	v := float64(size)
	for _, unit := range units {
		if v < 1024.0 {
			if unit == "B" {
				return fmt.Sprintf("%d %s", int64(v), unit)
			}
			return fmt.Sprintf("%.1f %s", v, unit)
		}
		v /= 1024.0
	}
	return fmt.Sprintf("%.1f %s", v, units[len(units)-1])
}

func percentOfParent(size, parent int64) float64 {
	if parent <= 0 {
		return 0
	}
	return float64(size) / float64(parent) * 100.0
}

func barChart(percent float64, width int) string {
	if width <= 0 {
		width = 18
	}
	full := int(percent / 100.0 * float64(width))
	remainder := (percent / 100.0 * float64(width)) - float64(full)
	bar := strings.Repeat("█", full)
	if remainder >= 0.5 && full < width {
		bar += "▌"
	} else if remainder >= 0.125 && full < width {
		bar += "▏"
	}
	if bar == "" && percent > 0 {
		bar = "▏"
	}
	return bar
}
