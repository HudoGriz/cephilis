// Package config provides configuration loading, defaults, and environment
// variable helpers for cephilis.
//
// Configuration is split across two YAML files inside a configuration
// directory:
//
//   - mounts.yaml    — list of mounts to health-check (path, expected_fstype)
//   - sections.yaml  — directory trees to report usage for (paths, depth,
//     limits); the older `sections:` list is still accepted
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
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultWorkers    = 16
	DefaultDepth      = 1
	DefaultMaxDirs    = 1000
	DefaultMaxEntries = 10000
	MetricsPrefix     = "cephilis"

	MountsFilename   = "mounts.yaml"
	SectionsFilename = "sections.yaml"
)

// DefaultConfigDirs is the ordered list of directories searched when the user
// does not pass an explicit --config-dir.
var DefaultConfigDirs = []string{
	"/etc/cephilis",
	"config",
}

// Path is one directory tree to report: the root itself plus every
// subdirectory down to Depth levels below it.
type Path struct {
	Path  string `yaml:"path"`
	Depth int    `yaml:"depth,omitempty"`
	// Owner adds an owner label (user name of the directory's uid).
	Owner bool `yaml:"owner,omitempty"`
}

// legacySection is the pre-0.3 `sections:` entry; it maps to a Path of depth 1.
type legacySection struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Mount is one mountpoint to health-check. Required mounts are retained even
// when their path is missing so health checks can fail loudly. Unknown keys
// from older configs (probe, timeout, legacy) are ignored.
type Mount struct {
	Name           string `yaml:"name"`
	Path           string `yaml:"path"`
	Required       bool   `yaml:"required,omitempty"`
	ExpectedFSType string `yaml:"expected_fstype,omitempty"`
}

// SpaceConfig is the parsed sections.yaml.
type SpaceConfig struct {
	Workers int `yaml:"workers,omitempty"`
	// MaxDirs caps the directories reported per path, so a wide tree cannot
	// flood Prometheus with series.
	MaxDirs int `yaml:"max_dirs,omitempty"`
	// MaxEntries: directories with more immediate entries than this are not
	// listed (listing reads every entry from the MDS); they are still reported.
	MaxEntries int `yaml:"max_entries,omitempty"`
	// SkipQuotas saves one MDS round trip per directory on filesystems
	// without quotas.
	SkipQuotas bool   `yaml:"skip_quotas,omitempty"`
	Paths      []Path `yaml:"paths"`

	Sections []legacySection `yaml:"sections"`
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

// LoadMounts reads mounts.yaml from path. path may be the YAML file itself or
// the enclosing config directory. It deliberately does not stat configured
// mount paths; a wedged CephFS mount can make path metadata operations block.
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
		m.Path = filepath.Clean(m.Path)
		m.ExpectedFSType = strings.TrimSpace(m.ExpectedFSType)
	}

	return cfg, nil
}

// LoadSpace reads sections.yaml from path (file or enclosing directory).
// Legacy `sections:` entries become depth-1 paths. Paths that do not exist are
// skipped with a warning to stderr.
func LoadSpace(path string) (SpaceConfig, error) {
	file, err := resolveFile(path, SectionsFilename)
	if err != nil {
		return SpaceConfig{}, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return SpaceConfig{}, fmt.Errorf("read %s: %w", file, err)
	}
	var cfg SpaceConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return SpaceConfig{}, fmt.Errorf("parse %s: %w", file, err)
	}
	for _, s := range cfg.Sections {
		cfg.Paths = append(cfg.Paths, Path{Path: s.Path, Depth: 1})
	}
	cfg.Sections = nil
	if len(cfg.Paths) == 0 {
		return SpaceConfig{}, fmt.Errorf("%s has no paths", file)
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.MaxDirs <= 0 {
		cfg.MaxDirs = DefaultMaxDirs
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}

	filtered := make([]Path, 0, len(cfg.Paths))
	for _, p := range cfg.Paths {
		if p.Path == "" {
			return SpaceConfig{}, fmt.Errorf("%s: path entry without 'path'", file)
		}
		p.Path = filepath.Clean(p.Path)
		if p.Depth <= 0 {
			p.Depth = DefaultDepth
		}
		if _, statErr := os.Stat(p.Path); statErr != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping path %s: %v\n", p.Path, statErr)
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return SpaceConfig{}, errors.New("no configured paths exist on this host")
	}
	cfg.Paths = filtered
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
