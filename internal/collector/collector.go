// Package collector scans CephFS directories for space and file-count
// statistics.  It first attempts to read CephFS extended attributes
// (ceph.dir.rbytes, ceph.dir.rfiles, ceph.dir.rsubdirs) for O(1) queries,
// and falls back to du(1) when those attributes are unavailable.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
// section.  It stops and propagates the first error encountered.
func ScanAll(sections []config.Section, maxWorkers int) ([]model.SectionResult, error) {
	results := make([]model.SectionResult, 0, len(sections))
	for _, section := range sections {
		res, err := ScanSection(section, maxWorkers)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// ScanSection scans the section root and all immediate subdirectories using a
// worker pool of up to maxWorkers goroutines.  Entries in the returned
// SectionResult are sorted descending by RBytes.
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
	for o := range outs {
		if o.err != nil {
			return model.SectionResult{}, o.err
		}
		entries = append(entries, o.stat)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].RBytes > entries[j].RBytes })

	return model.SectionResult{
		Name:       section.Name,
		ParentPath: section.Path,
		Parent:     &parent,
		Entries:    entries,
	}, nil
}

// cephStats returns DirStats for a single path, preferring CephFS xattrs and
// falling back to du(1).
func cephStats(path string) (model.DirStats, error) {
	name := filepath.Base(path)
	if name == "" || name == "." || name == "/" {
		name = path
	}

	rbytes, err := getXAttrInt(path, xattrBytes)
	if err == nil {
		rfiles, filesErr := getXAttrInt(path, xattrFiles)
		if filesErr != nil {
			return model.DirStats{}, filesErr
		}
		rsubdirs, subErr := getXAttrInt(path, xattrSubdirs)
		if subErr != nil {
			rsubdirs = 0
		}
		return model.DirStats{Path: path, Name: name, RBytes: rbytes, RFiles: rfiles, RSubdirs: rsubdirs}, nil
	}

	duSize, duErr := fallbackDU(path)
	if duErr != nil {
		return model.DirStats{}, duErr
	}
	return model.DirStats{Path: path, Name: name, RBytes: duSize, RFiles: 0, RSubdirs: 0}, nil
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

// fallbackDU invokes du -sb to measure directory size when CephFS xattrs are
// unavailable.  It respects config.DefaultDUTimeout.
func fallbackDU(path string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.DefaultDUTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "du", "-sb", path)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("du fallback failed for %s: %w", path, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("du fallback empty output for %s", path)
	}
	v, parseErr := strconv.ParseInt(fields[0], 10, 64)
	if parseErr != nil {
		return 0, fmt.Errorf("du parse failed for %s: %w", path, parseErr)
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
