// Package collector scans CephFS directories for space and file-count
// statistics.  It first attempts to read CephFS extended attributes
// (ceph.dir.rbytes, ceph.dir.rfiles, ceph.dir.rsubdirs) for O(1) queries.
// There is deliberately no du(1) fallback: walking a multi-PB tree takes hours
// and blocks in D-state on a CephFS brownout.
package collector

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/HudoGriz/cephilis/internal/config"
	"github.com/HudoGriz/cephilis/internal/model"
)

var (
	xattrBytes   = "ceph.dir.rbytes"
	xattrFiles   = "ceph.dir.rfiles"
	xattrSubdirs = "ceph.dir.rsubdirs"
)

// ScanAll scans each section sequentially and returns one SectionResult per
// section.  A failing section is returned with Err set instead of aborting
// the scan, so one moved or unreadable path does not blank the whole report.
func ScanAll(sections []config.Section, maxWorkers int) []model.SectionResult {
	results := make([]model.SectionResult, 0, len(sections))
	for _, section := range sections {
		res, err := ScanSection(section, maxWorkers)
		if err != nil {
			res = model.SectionResult{Name: section.Name, ParentPath: section.Path, Err: err}
		}
		results = append(results, res)
	}
	return results
}

// ScanSection scans the section root and all immediate subdirectories using a
// worker pool of up to maxWorkers goroutines.  Entries in the returned
// SectionResult are sorted descending by RBytes.  Subdirectories whose stats
// cannot be read are skipped and counted in FailedDirs.
func ScanSection(section config.Section, maxWorkers int) (model.SectionResult, error) {
	if maxWorkers <= 0 {
		maxWorkers = config.DefaultWorkers
	}

	parent, err := cephStats(section.Path)
	if err != nil {
		return model.SectionResult{}, err
	}

	subdirs, err := listSubdirs(section.Path)
	if err != nil {
		return model.SectionResult{}, err
	}

	type out struct {
		stat model.DirStats
		err  error
	}

	jobs := make(chan string)
	outs := make(chan out, len(subdirs))
	workers := maxWorkers
	if workers > len(subdirs) && len(subdirs) > 0 {
		workers = len(subdirs)
	}
	if workers == 0 {
		workers = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				st, scanErr := cephStats(p)
				outs <- out{stat: st, err: scanErr}
			}
		}()
	}

	go func() {
		for _, p := range subdirs {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(outs)
	}()

	entries := make([]model.DirStats, 0, len(subdirs))
	failed := 0
	for o := range outs {
		if o.err != nil {
			failed++
			continue
		}
		entries = append(entries, o.stat)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].RBytes > entries[j].RBytes })

	return model.SectionResult{
		Name:       section.Name,
		ParentPath: section.Path,
		Parent:     &parent,
		Entries:    entries,
		FailedDirs: failed,
	}, nil
}

// cephStats returns DirStats for a single path from CephFS xattrs.
func cephStats(path string) (model.DirStats, error) {
	name := filepath.Base(path)
	if name == "" || name == "." || name == "/" {
		name = path
	}

	rbytes, err := getXAttrInt(path, xattrBytes)
	if err != nil {
		return model.DirStats{}, fmt.Errorf("read %s on %s (not CephFS?): %w", xattrBytes, path, err)
	}
	rfiles, err := getXAttrInt(path, xattrFiles)
	if err != nil {
		return model.DirStats{}, fmt.Errorf("read %s on %s: %w", xattrFiles, path, err)
	}
	rsubdirs, err := getXAttrInt(path, xattrSubdirs)
	if err != nil {
		rsubdirs = 0
	}
	return model.DirStats{Path: path, Name: name, RBytes: rbytes, RFiles: rfiles, RSubdirs: rsubdirs}, nil
}

// getXAttrInt reads a CephFS extended attribute and parses it as int64.
func getXAttrInt(path, attr string) (int64, error) {
	buf := make([]byte, 64)
	n, err := syscall.Getxattr(path, attr, buf)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("empty xattr")
	}
	v, parseErr := strconv.ParseInt(strings.TrimSpace(string(bytes.Trim(buf[:n], "\x00"))), 10, 64)
	if parseErr != nil {
		return 0, fmt.Errorf("parse xattr %s for %s: %w", attr, path, parseErr)
	}
	return v, nil
}

// listSubdirs returns the sorted list of immediate subdirectory paths under
// path.  Permission errors are silently ignored (returns empty slice).
func listSubdirs(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return []string{}, nil
		}
		return nil, err
	}
	paths := make([]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(path, e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}
