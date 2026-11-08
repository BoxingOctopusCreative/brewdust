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
	raw, err := exec.Command("mdls", "-raw", "-name", "kMDItemLastUsedDate", path).Output()
	if err != nil {
		return time.Time{}, false
	}
	return parseMdlsDate(string(raw))
}

func parseMdlsDate(s string) (time.Time, bool) {
	s = strings.Trim(strings.TrimSpace(s), "\x00")
	if s == "" || s == "(null)" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15:04:05 -0700", s)
	return t, err == nil
}
