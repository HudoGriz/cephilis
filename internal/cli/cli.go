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
	fmt.Fprintln(out, "cephilis - HPC storage goblin")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  cephilis <command> [options]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init     Write config templates and create probe files")
	fmt.Fprintln(out, "  space    Report directory space usage via CephFS xattrs")
	fmt.Fprintln(out, "  mount    Check CephFS mount health and export metrics")
	fmt.Fprintln(out, "  all      Run both space and mount checks")
	fmt.Fprintln(out, "  version  Print version and exit")
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
	results, err := collector.ScanAll(sections, w)
	if err != nil {
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

	mounts := mount.CheckAll(mcfg.Mounts)
	space, err := collector.ScanAll(scfg.Sections, w)
	if err != nil {
		return err
	}

	compat := config.BoolEnv("CEPHILIS_COMPAT_MOUNT_METRICS", false)

	switch *formatName {
	case "prom":
		mo, err := renderMounts("prom", mounts, compat)
		if err != nil {
			return fmt.Errorf("format mount: %w", err)
		}
		so, err := format.Space("prom", space)
		if err != nil {
			return fmt.Errorf("format space: %w", err)
		}
		fmt.Fprint(stdout, mo)
		fmt.Fprint(stdout, so)
		return nil
	case "json":
		so, err := format.Space("json", space)
		if err != nil {
			return fmt.Errorf("format space: %w", err)
		}
		payload := struct {
			Mounts []model.MountResult `json:"mounts"`
			Space  json.RawMessage     `json:"space"`
		}{Mounts: mounts, Space: json.RawMessage(so)}
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json: %w", err)
		}
		fmt.Fprintln(stdout, string(b))
		return nil
	default:
		mo, err := renderMounts("table", mounts, compat)
		if err != nil {
			return fmt.Errorf("format mount: %w", err)
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
		fmt.Fprintln(stdout, "Usage: cephilis init [--config-dir DIR] [--force] [--create-probes]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Writes mounts.yaml and sections.yaml templates.")
		fmt.Fprintln(stdout, "With --create-probes, touches every probe file declared in mounts.yaml.")
		return nil
	}

	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configDir := fs.String("config-dir", "", "Config directory to initialise (default: /etc/cephilis if root, else ./config)")
	force := fs.Bool("force", false, "Overwrite existing config files")
	createProbes := fs.Bool("create-probes", false, "Touch probe files declared in mounts.yaml")
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

	if *createProbes {
		mcfg, err := config.LoadMounts(mountsPath)
		if err != nil {
			fmt.Fprintf(stderr, "warning: could not load mounts for probe creation: %v\n", err)
			return nil
		}
		for _, m := range mcfg.Mounts {
			if err := touchProbe(m.Probe); err != nil {
				fmt.Fprintf(stderr, "warning: touch probe %s: %v\n", m.Probe, err)
				continue
			}
			fmt.Fprintf(stdout, "touched probe %s\n", m.Probe)
		}
	} else {
		fmt.Fprintln(stdout, "(pass --create-probes to also create probe sentinel files)")
	}
	return nil
}

func writeTemplate(path, content string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (pass --force to overwrite)", path)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func touchProbe(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
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
		if strings.ToLower(m.Name) == needle {
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
# Each entry is a mountpoint to health-check.
# - path:    filesystem path expected to be a mountpoint
# - probe:   sentinel file stat'd to measure responsiveness (will be created by "cephilis init --create-probes")
# - timeout: seconds allowed for mountpoint(1) and stat(1); default 3
# - legacy:  emit legacy mount_home_* metrics (opt-in compat)
mounts:
  - name: home
    path: /home
    probe: /home/.probe
    timeout: 3
`

const sectionsTemplate = `# cephilis sections.yaml
# Each entry is a directory tree to measure for size (via CephFS xattrs).
workers: 16
sections:
  - name: Home Users
    path: /home
`
