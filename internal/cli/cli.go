// Package cli implements the command-line interface for cephilis.
// It wires together the collector, mount, and format packages, and is the
// primary entry point called from main.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/HudoGriz/cephilis/internal/collector"
	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/format"
	"github.com/HudoGriz/cephilis/internal/model"
	"github.com/HudoGriz/cephilis/internal/monitor"
	"github.com/HudoGriz/cephilis/internal/mount"
)

// Version is set at build time via -ldflags "-X github.com/HudoGriz/cephilis/internal/cli.Version=<tag>".
var Version = "dev"

// Run is the top-level entry point.  It dispatches to the appropriate
// sub-command based on args[0] and writes all output to stdout/stderr.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printRootHelp(stdout)
		return nil
	}

	switch args[0] {
	case "space":
		return runSpace(args[1:], stdout, stderr)
	case "mount":
		return runMount(args[1:], stdout, stderr)
	case "health":
		return runHealth(args[1:], stdout, stderr)
	case "all":
		return runAll(args[1:], stdout, stderr)
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, "cephilis "+Version)
		return nil
	case "help", "-h", "--help":
		printRootHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printRootHelp(out io.Writer) {
	fmt.Fprintln(out, "cephilis - HPC storage monitor")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  cephilis <command> [options]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init     Write config templates")
	fmt.Fprintln(out, "  space    Report directory space usage via CephFS xattrs")
	fmt.Fprintln(out, "  mount    Check CephFS mount health and export metrics")
	fmt.Fprintln(out, "  health   Safe CephFS client health for Slurm, Prometheus, or JSON")
	fmt.Fprintln(out, "  all      Run both space and mount checks")
	fmt.Fprintln(out, "  version  Print version and exit")
}

func runHealth(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis health [--config-dir DIR] [--config FILE] [--mount NAME_OR_PATH] [--mode slurm|prom|json]")
		return nil
	}
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configDir := fs.String("config-dir", "", "Config directory")
	configFile := fs.String("config", "", "Path to mounts.yaml")
	mountName := fs.String("mount", "", "Only this mount")
	mode := fs.String("mode", "slurm", "Output mode: slurm, prom, json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mounts := []config.Mount{}
	mountArg := strings.TrimSpace(*mountName)
	if strings.HasPrefix(mountArg, "/") {
		mounts = []config.Mount{{
			Name:           strings.Trim(filepath.Base(filepath.Clean(mountArg)), "/"),
			Path:           mountArg,
			Required:       true,
			ExpectedFSType: "ceph",
		}}
		if mounts[0].Name == "" {
			mounts[0].Name = "mount"
		}
	} else {
		mcfg, err := loadMounts(*configFile, *configDir)
		if err != nil {
			return err
		}
		mounts = mcfg.Mounts
		if mountArg != "" {
			mounts = filterMount(mounts, mountArg)
			if len(mounts) == 0 {
				return fmt.Errorf("mount not found: %s", mountArg)
			}
		}
	}

	results := mount.CheckHealthAll(mounts)
	output, err := renderHealth(*mode, results)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, output)

	for _, r := range results {
		if !r.Healthy {
			if *mode == "prom" {
				return nil
			}
			return fmt.Errorf("required mount health check failed")
		}
	}
	return nil
}

func runSpace(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis space [--config-dir DIR] [--config FILE] [--format table|csv|tsv|json|prom] [--section NAME] [--workers N]")
		return nil
	}

	fs := flag.NewFlagSet("space", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configDir := fs.String("config-dir", "", "Config directory (defaults: $CEPHILIS_CONFIG_DIR, /etc/cephilis, ./config)")
	configFile := fs.String("config", "", "Path to sections.yaml (overrides --config-dir for this file)")
	formatName := fs.String("format", "table", "Output format")
	sectionName := fs.String("section", "", "Only this section")
	workers := fs.Int("workers", 0, "Max parallel workers (0=from config)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if !format.IsValid(*formatName) {
		return fmt.Errorf("invalid --format %q (valid: %s)", *formatName, strings.Join(format.Names(), ", "))
	}

	if os.Geteuid() != 0 {
		slog.New(slog.NewTextHandler(stderr, nil)).Warn("not running as root — some directories may be unreadable")
	}

	cfg, err := loadSections(*configFile, *configDir)
	if err != nil {
		return err
	}

	sections := cfg.Sections
	if strings.TrimSpace(*sectionName) != "" {
		sections = filterSection(sections, *sectionName)
		if len(sections) == 0 {
			return fmt.Errorf("section not found: %s", *sectionName)
		}
	}

	w := *workers
	if w <= 0 {
		w = cfg.Workers
	}

	start := time.Now()
	results := collector.ScanAll(sections, w)
	if err := reportSpaceErrors(results, stderr); err != nil {
		return err
	}

	out, err := format.Space(*formatName, results)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	if *formatName == "table" {
		fmt.Fprintf(stdout, "Completed in %.1f seconds\n", time.Since(start).Seconds())
	}
	return nil
}

// reportSpaceErrors logs failed sections and skipped directories to stderr.
// It returns an error only when every section failed, so a partial scan still
// produces output (failures are visible as cephilis_section_scan_ok 0).
func reportSpaceErrors(results []model.SectionResult, stderr io.Writer) error {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			log.Error("section scan failed", "section", r.Name, "path", r.ParentPath, "err", r.Err)
		} else if r.FailedDirs > 0 {
			log.Warn("skipped unreadable directories", "section", r.Name, "count", r.FailedDirs)
		}
	}
	if len(results) > 0 && failed == len(results) {
		return fmt.Errorf("all %d sections failed", failed)
	}
	return nil
}

func runMount(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis mount [--config-dir DIR] [--config FILE] [--mount NAME] [--format table|json|prom]")
		return nil
	}

	fs := flag.NewFlagSet("mount", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configDir := fs.String("config-dir", "", "Config directory")
	configFile := fs.String("config", "", "Path to mounts.yaml")
	mountName := fs.String("mount", "", "Only this mount")
	formatName := fs.String("format", "table", "Output format")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *formatName != "table" && *formatName != "json" && *formatName != "prom" {
		return fmt.Errorf("invalid mount --format %q (valid: table|json|prom)", *formatName)
	}

	mcfg, err := loadMounts(*configFile, *configDir)
	if err != nil {
		return err
	}
	mounts := mcfg.Mounts
	if strings.TrimSpace(*mountName) != "" {
		mounts = filterMount(mounts, *mountName)
		if len(mounts) == 0 {
			return fmt.Errorf("mount not found: %s", *mountName)
		}
	}

	results := mount.CheckAll(mounts)
	compat := config.BoolEnv("CEPHILIS_COMPAT_MOUNT_METRICS", false)
	out, err := renderMounts(*formatName, results, compat)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	return nil
}

func runAll(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis all [--config-dir DIR] [--workers N] [--format table|json|prom]")
		return nil
	}

	fs := flag.NewFlagSet("all", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configDir := fs.String("config-dir", "", "Config directory")
	workers := fs.Int("workers", 0, "Max parallel workers (0=from config)")
	formatName := fs.String("format", "table", "Output format")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *formatName != "table" && *formatName != "json" && *formatName != "prom" {
		return fmt.Errorf("invalid all --format %q (valid: table|json|prom)", *formatName)
	}

	mcfg, err := loadMounts("", *configDir)
	if err != nil {
		return err
	}
	scfg, err := loadSections("", *configDir)
	if err != nil {
		return err
	}
	w := *workers
	if w <= 0 {
		w = scfg.Workers
	}

	health := mount.CheckHealthAll(mcfg.Mounts)
	space := collector.ScanAll(scfg.Sections, w)
	if err := reportSpaceErrors(space, stderr); err != nil {
		return err
	}

	switch *formatName {
	case "prom":
		mo, err := renderHealth("prom", health)
		if err != nil {
			return fmt.Errorf("format health: %w", err)
		}
		so, err := format.Space("prom", space)
		if err != nil {
			return fmt.Errorf("format space: %w", err)
		}
		fmt.Fprint(stdout, mo)
		fmt.Fprint(stdout, so)
		fmt.Fprint(stdout, monitor.Prom(monitor.Collect()))
		return nil
	case "json":
		so, err := format.Space("json", space)
		if err != nil {
			return fmt.Errorf("format space: %w", err)
		}
		payload := struct {
			Health []model.HealthResult `json:"health"`
			Space  json.RawMessage      `json:"space"`
		}{Health: health, Space: json.RawMessage(so)}
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		fmt.Fprintln(stdout, string(b))
		return nil
	default:
		mo, err := renderHealth("slurm", health)
		if err != nil {
			return fmt.Errorf("format health: %w", err)
		}
		so, err := format.Space("table", space)
		if err != nil {
			return fmt.Errorf("format space: %w", err)
		}
		fmt.Fprint(stdout, mo)
		fmt.Fprintln(stdout)
		fmt.Fprint(stdout, so)
		return nil
	}
}

func runInit(args []string, stdout, stderr io.Writer) error {
	if hasHelpArg(args) {
		fmt.Fprintln(stdout, "Usage: cephilis init [--config-dir DIR] [--force]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Writes mounts.yaml and sections.yaml templates.")
		return nil
	}

	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configDir := fs.String("config-dir", "", "Config directory to initialise (default: /etc/cephilis if root, else ./config)")
	force := fs.Bool("force", false, "Overwrite existing config files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := strings.TrimSpace(*configDir)
	if dir == "" {
		if os.Geteuid() == 0 {
			dir = "/etc/cephilis"
		} else {
			dir = "config"
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	mountsPath := filepath.Join(dir, config.MountsFilename)
	sectionsPath := filepath.Join(dir, config.SectionsFilename)

	if err := writeTemplate(mountsPath, mountsTemplate, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", mountsPath)
	if err := writeTemplate(sectionsPath, sectionsTemplate, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", sectionsPath)

	return nil
}

func writeTemplate(path, content string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (pass --force to overwrite)", path)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// renderMounts renders a slice of MountResult in the requested format,
// concatenating per-mount output.  For json, wraps in a {"mounts":[...]} object.
func renderMounts(formatName string, results []model.MountResult, compat bool) (string, error) {
	if formatName == "json" {
		b, err := json.MarshalIndent(struct {
			Mounts []model.MountResult `json:"mounts"`
		}{Mounts: results}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	}
	var b strings.Builder
	for i, r := range results {
		out, err := format.Mount(formatName, r, compat)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
		if formatName == "table" && i < len(results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func renderHealth(mode string, results []model.HealthResult) (string, error) {
	switch mode {
	case "slurm":
		var b strings.Builder
		for _, r := range results {
			if r.Healthy {
				fmt.Fprintf(&b, "OK: %s CephFS client healthy\n", r.MountPath)
			} else {
				fmt.Fprintf(&b, "ERROR: %s CephFS client unhealthy: %s\n", r.MountPath, r.Error)
			}
		}
		return b.String(), nil
	case "json":
		b, err := json.MarshalIndent(struct {
			Health []model.HealthResult `json:"health"`
		}{Health: results}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	case "prom":
		return healthProm(results), nil
	default:
		return "", fmt.Errorf("invalid health --mode %q (valid: slurm|prom|json)", mode)
	}
}

func healthProm(results []model.HealthResult) string {
	prefix := config.MetricsPrefix
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# HELP %s_health_ok Safe CephFS client health check result.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_health_ok gauge\n", prefix))
	for _, r := range results {
		fmt.Fprintf(&b, "%s_health_ok{%s} %d\n", prefix, healthLabels(r), boolInt(r.Healthy))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# HELP %s_mount_up Mount point appears in local mount table.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_up gauge\n", prefix))
	for _, r := range results {
		fmt.Fprintf(&b, "%s_mount_up{%s} %d\n", prefix, healthLabels(r), boolInt(r.Mounted))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# HELP %s_mount_fstype_match Mount filesystem type matches expected_fstype.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_mount_fstype_match gauge\n", prefix))
	for _, r := range results {
		fmt.Fprintf(&b, "%s_mount_fstype_match{%s} %d\n", prefix, healthLabels(r), boolInt(r.FSTypeMatches))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# HELP %s_cephfs_mds_session_open CephFS kernel client has an open MDS session.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_cephfs_mds_session_open gauge\n", prefix))
	for _, r := range results {
		fmt.Fprintf(&b, "%s_cephfs_mds_session_open{%s} %d\n", prefix, healthLabels(r), boolInt(r.MDSSessionOpen))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("# HELP %s_cephfs_session_unhealthy CephFS kernel client session contains stale/blocklisted/reconnect/closed state.\n", prefix))
	b.WriteString(fmt.Sprintf("# TYPE %s_cephfs_session_unhealthy gauge\n", prefix))
	for _, r := range results {
		fmt.Fprintf(&b, "%s_cephfs_session_unhealthy{%s} %d\n", prefix, healthLabels(r), boolInt(r.UnhealthySession))
	}
	b.WriteString("\n")
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(&b, "%s_health_error{%s,error=\"%s\"} 1\n", prefix, healthLabels(r), promLabel(r.Error))
		}
	}
	return b.String()
}

func healthLabels(r model.HealthResult) string {
	return fmt.Sprintf("name=\"%s\",mount=\"%s\",hostname=\"%s\",expected=\"%s\",actual=\"%s\"",
		promLabel(r.Name), promLabel(r.MountPath), promLabel(r.Hostname), promLabel(r.ExpectedFSType), promLabel(r.ActualFSType))
}

func promLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func loadSections(configFile, configDir string) (config.SectionsConfig, error) {
	if strings.TrimSpace(configFile) != "" {
		return config.LoadSections(configFile)
	}
	if strings.TrimSpace(configDir) != "" {
		return config.LoadSections(configDir)
	}
	return config.LoadSections("")
}

func loadMounts(configFile, configDir string) (config.MountsConfig, error) {
	if strings.TrimSpace(configFile) != "" {
		return config.LoadMounts(configFile)
	}
	if strings.TrimSpace(configDir) != "" {
		return config.LoadMounts(configDir)
	}
	return config.LoadMounts("")
}

func filterSection(sections []config.Section, name string) []config.Section {
	needle := strings.ToLower(strings.TrimSpace(name))
	out := make([]config.Section, 0)
	for _, s := range sections {
		if strings.ToLower(s.Name) == needle {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func filterMount(mounts []config.Mount, name string) []config.Mount {
	needle := strings.ToLower(strings.TrimSpace(name))
	out := make([]config.Mount, 0)
	for _, m := range mounts {
		if strings.ToLower(m.Name) == needle || strings.ToLower(m.Path) == needle {
			out = append(out, m)
		}
	}
	return out
}

func hasHelpArg(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

const mountsTemplate = `# cephilis mounts.yaml
# Each entry is a mountpoint to health-check safely via mount table + Ceph debugfs.
# - path:    filesystem path expected to be a mountpoint
# - required: if true, missing/unhealthy mount makes cephilis health fail
# - expected_fstype: optional expected mount-table filesystem type, for example ceph
mounts:
  - name: home
    path: /home
    required: true
    expected_fstype: ceph
`

const sectionsTemplate = `# cephilis sections.yaml
# Each entry is a directory tree to measure for size (via CephFS xattrs).
workers: 16
sections:
  - name: Home Users
    path: /home
`
