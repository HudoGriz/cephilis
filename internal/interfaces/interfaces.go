// Package interfaces defines the abstract boundaries used by the CLI so that
// concrete implementations (collector, mount) can be swapped in tests.
package interfaces

import (
	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

// Collector scans a set of configured sections and returns per-section results.
type Collector interface {
	ScanAll(sections []config.Section, maxWorkers int) ([]model.SectionResult, error)
}

// Prober checks whether one or more CephFS mounts are healthy and responsive.
type Prober interface {
	Check(m config.Mount) model.MountResult
	CheckAll(mounts []config.Mount) []model.MountResult
}
