// Package config provides configuration loading, defaults, and environment
// variable helpers for cephilis.
//
// Configuration is split across two YAML files inside a configuration
// directory:
//
//   - mounts.yaml    — list of mounts to health-check (path, probe, timeout)
//   - sections.yaml  — list of directories to measure for size, plus
//     per-file options like the worker pool size
//
// The configuration directory is resolved in this order:
//
//  1. explicit path passed to ResolveConfigDir (e.g. from a --config-dir flag)
//  2. $CEPHILIS_CONFIG_DIR
//  3. /etc/cephilis
//  4. ./config (repo-local fallback)
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultWorkers             = 16
	DefaultMountTimeoutSeconds = 3
	MetricsPrefix              = "cephilis"

	MountsFilename   = "mounts.yaml"
	SectionsFilename = "sections.yaml"
)

// DefaultDUTimeout bounds the du(1) fallback when CephFS xattrs are missing.
var DefaultDUTimeout = 300 * time.Second

// DefaultConfigDirs is the ordered list of directories searched when the user
// does not pass an explicit --config-dir.
var DefaultConfigDirs = []string{
	"/etc/cephilis",
	"config",
}

// Section is one directory tree to measure for size.
type Section struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Mount is one mountpoint to health-check.  Probe is the sentinel file stat'd
// to measure responsiveness; Timeout bounds both the mountpoint(1), stat(1),
// and filesystem type probes.  Required mounts are retained even when their
// path is missing so health checks can fail loudly.
type Mount struct {
	Name           string `yaml:"name"`
	Path           string `yaml:"path"`
	Probe          string `yaml:"probe"`
	Timeout        int    `yaml:"timeout,omitempty"`
	Required       bool   `yaml:"required,omitempty"`
	ExpectedFSType string `yaml:"expected_fstype,omitempty"`
	Legacy         bool   `yaml:"legacy,omitempty"`
}

// SectionsConfig is the parsed sections.yaml.
type SectionsConfig struct {
	Workers  int       `yaml:"workers,omitempty"`
	Sections []Section `yaml:"sections"`
}

// MountsConfig is the parsed mounts.yaml.
type MountsConfig struct {
	Mounts []Mount `yaml:"mounts"`
}

// ResolveConfigDir picks the config directory.  When cliDir is non-empty it is
// required to exist.  Otherwise $CEPHILIS_CONFIG_DIR, then DefaultConfigDirs
// are tried in order.
func ResolveConfigDir(cliDir string) (string, error) {
	if strings.TrimSpace(cliDir) != "" {
		if st, err := os.Stat(cliDir); err != nil || !st.IsDir() {
			return "", fmt.Errorf("config dir not found: %s", cliDir)
		}
		return cliDir, nil
	}
	if env := strings.TrimSpace(os.Getenv("CEPHILIS_CONFIG_DIR")); env != "" {
		if st, err := os.Stat(env); err == nil && st.IsDir() {
			return env, nil
		}
	}
	for _, d := range DefaultConfigDirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d, nil
		}
	}
	return "", fmt.Errorf("no config directory found; searched: $CEPHILIS_CONFIG_DIR, %s",
		strings.Join(DefaultConfigDirs, ", "))
}

// LoadMounts reads mounts.yaml from path.  path may be the YAML file itself or
// the enclosing config directory.  Optional mounts whose Path does not exist on
// this host are skipped with a warning; required mounts are retained so the
// health check can report a hard failure.
func LoadMounts(path string) (MountsConfig, error) {
	file, err := resolveFile(path, MountsFilename)
	if err != nil {
		return MountsConfig{}, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return MountsConfig{}, fmt.Errorf("read mounts %s: %w", file, err)
	}
	var cfg MountsConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return MountsConfig{}, fmt.Errorf("parse mounts %s: %w", file, err)
	}
	if len(cfg.Mounts) == 0 {
		return MountsConfig{}, errors.New("mounts.yaml has no mounts")
	}

	for i := range cfg.Mounts {
		m := &cfg.Mounts[i]
		if m.Name == "" {
			return MountsConfig{}, fmt.Errorf("mount #%d is missing 'name'", i+1)
		}
		if m.Path == "" {
			return MountsConfig{}, fmt.Errorf("mount %q is missing 'path'", m.Name)
		}
		if m.Probe == "" {
			return MountsConfig{}, fmt.Errorf("mount %q is missing 'probe'", m.Name)
		}
		m.Path = filepath.Clean(m.Path)
		m.Probe = filepath.Clean(m.Probe)
		m.ExpectedFSType = strings.TrimSpace(m.ExpectedFSType)
		if m.Timeout <= 0 {
			m.Timeout = DefaultMountTimeoutSeconds
		}
	}

	filtered := make([]Mount, 0, len(cfg.Mounts))
	for _, m := range cfg.Mounts {
		if _, statErr := os.Stat(m.Path); statErr == nil {
			filtered = append(filtered, m)
		} else if m.Required {
			filtered = append(filtered, m)
		} else {
			fmt.Fprintf(os.Stderr, "warning: skipping mount %q: path %s does not exist\n", m.Name, m.Path)
		}
	}
	if len(filtered) == 0 {
		return MountsConfig{}, errors.New("no configured mount paths exist on this host")
	}
	return MountsConfig{Mounts: filtered}, nil
}

// LoadSections reads sections.yaml from path (file or enclosing directory).
// Sections whose Path does not exist are skipped with a warning to stderr.
func LoadSections(path string) (SectionsConfig, error) {
	file, err := resolveFile(path, SectionsFilename)
	if err != nil {
		return SectionsConfig{}, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return SectionsConfig{}, fmt.Errorf("read sections %s: %w", file, err)
	}
	var cfg SectionsConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return SectionsConfig{}, fmt.Errorf("parse sections %s: %w", file, err)
	}
	if len(cfg.Sections) == 0 {
		return SectionsConfig{}, errors.New("sections.yaml has no sections")
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}

	for i := range cfg.Sections {
		cfg.Sections[i].Path = filepath.Clean(cfg.Sections[i].Path)
	}

	filtered := make([]Section, 0, len(cfg.Sections))
	for _, s := range cfg.Sections {
		if _, statErr := os.Stat(s.Path); statErr == nil {
			filtered = append(filtered, s)
		} else {
			fmt.Fprintf(os.Stderr, "warning: skipping section %q: path %s does not exist\n", s.Name, s.Path)
		}
	}
	if len(filtered) == 0 {
		return SectionsConfig{}, errors.New("no configured section paths exist on this host")
	}
	cfg.Sections = filtered
	return cfg, nil
}

// resolveFile returns path itself when it points to a regular file, or
// filepath.Join(path, defaultName) when it points to a directory.  When path
// is empty, ResolveConfigDir is used to locate a default directory.
func resolveFile(path, defaultName string) (string, error) {
	if strings.TrimSpace(path) == "" {
		dir, err := ResolveConfigDir("")
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, defaultName), nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("config path not found: %s", path)
	}
	if st.IsDir() {
		return filepath.Join(path, defaultName), nil
	}
	return path, nil
}

// IntEnv reads an integer from the named env var.  Returns fallback when
// absent, empty, non-numeric, or <= 0.
func IntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// BoolEnv reads a boolean from the named env var.  Truthy: 1,true,yes,on.
// Falsy: 0,false,no,off.  Anything else returns fallback.
func BoolEnv(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
