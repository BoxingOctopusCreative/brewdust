// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package scan measures disk usage and the most recent file access under a directory.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Result summarises a directory tree.
type Result struct {
	Exists     bool
	Bytes      int64     // allocated bytes on disk (like du), hard links counted once
	LastAccess time.Time // newest atime among files accepted by the Eligible func
	AccessPath string    // the file that produced LastAccess
	Errors     int       // entries that could not be read
}

// Merge folds o into r.
func (r *Result) Merge(o Result) {
	r.Exists = r.Exists || o.Exists
	r.Bytes += o.Bytes
	r.Errors += o.Errors
	if o.LastAccess.After(r.LastAccess) {
		r.LastAccess, r.AccessPath = o.LastAccess, o.AccessPath
	}
}

// Eligible reports whether a regular file's access time should count as "use".
// rel is the slash-separated path relative to the scanned root.
type Eligible func(rel string) bool

type inode struct {
	dev uint64
	ino uint64
}

// Dir walks root without following symlinks. Every entry counts toward Bytes;
// only regular files accepted by eligible count toward LastAccess.
// Reading metadata with lstat does not itself change access times.
func Dir(root string, eligible Eligible) Result {
	var r Result
	if _, err := os.Lstat(root); err != nil {
		return r
	}
	r.Exists = true
	seen := map[inode]bool{}

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			r.Errors++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			r.Errors++
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			r.Bytes += info.Size()
			return nil
		}
		if !d.IsDir() && st.Nlink > 1 {
			k := inode{uint64(st.Dev), uint64(st.Ino)}
			if seen[k] {
				return nil
			}
			seen[k] = true
		}
		r.Bytes += int64(st.Blocks) * 512

		if eligible != nil && d.Type().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err == nil && eligible(filepath.ToSlash(rel)) {
				if at := atime(st); at.After(r.LastAccess) {
					r.LastAccess, r.AccessPath = at, path
				}
			}
		}
		return nil
	})
	return r
}
