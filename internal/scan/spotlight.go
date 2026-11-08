package scan

import (
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// AppLastUsed asks Spotlight (kMDItemLastUsedDate, maintained by
// LaunchServices) when an app bundle was last opened. ok is false when there
// is no record, mdls is unavailable, or this isn't macOS.
//
// Paths are queried one at a time: with several paths, mdls skips the ones it
// can't read, which would misalign the results.
func AppLastUsed(path string) (time.Time, bool) {
	if runtime.GOOS != "darwin" {
		return time.Time{}, false
	}
	raw, err := exec.Command("mdls", "-raw",
		"-name", "kMDItemLastUsedDate", "-name", "kMDItemFSContentChangeDate", path).Output()
	if err != nil {
		return time.Time{}, false
	}
	return parseLastUsed(string(raw))
}

// parseLastUsed reads mdls -raw output for kMDItemLastUsedDate and
// kMDItemFSContentChangeDate, which it separates with NULs. For a bundle
// Spotlight hasn't indexed, mdls reports the modification date as the
// last-used date; equal dates therefore mean there's no launch record.
func parseLastUsed(raw string) (time.Time, bool) {
	vals := strings.Split(raw, "\x00")
	used, ok := parseMdlsDate(vals[0])
	if !ok {
		return time.Time{}, false
	}
	if len(vals) > 1 {
		if changed, ok := parseMdlsDate(vals[1]); ok && changed.Equal(used) {
			return time.Time{}, false
		}
	}
	return used, true
}

func parseMdlsDate(s string) (time.Time, bool) {
	s = strings.Trim(strings.TrimSpace(s), "\x00")
	if s == "" || s == "(null)" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15:04:05 -0700", s)
	return t, err == nil
}
