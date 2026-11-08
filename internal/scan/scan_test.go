package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirSizeAndAtime(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, atime time.Time) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, 10000), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, atime, atime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	older := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bin := write("bin/tool", older)
	write("share/doc/readme", newer) // newer, but not eligible
	if err := os.Link(bin, filepath.Join(root, "bin/tool-link")); err != nil {
		t.Fatal(err)
	}

	r := Dir(root, func(rel string) bool { return !strings.HasPrefix(rel, "share/") })
	if !r.Exists || r.Errors != 0 {
		t.Fatalf("exists=%v errors=%d", r.Exists, r.Errors)
	}
	if !r.LastAccess.Equal(older) {
		t.Errorf("LastAccess = %v, want %v", r.LastAccess, older)
	}
	// Two 10 kB files (the hard link counted once), plus directories.
	if r.Bytes < 20000 || r.Bytes > 60000 {
		t.Errorf("Bytes = %d, expected roughly 20-60 kB", r.Bytes)
	}

	if Dir(filepath.Join(root, "missing"), nil).Exists {
		t.Error("missing dir reported as existing")
	}
}

func TestParseMdlsDate(t *testing.T) {
	if _, ok := parseMdlsDate("(null)"); ok {
		t.Error("(null) should not parse")
	}
	got, ok := parseMdlsDate("2026-11-08 04:11:23 +0000")
	if !ok || !got.Equal(time.Date(2026, 11, 8, 4, 11, 23, 0, time.UTC)) {
		t.Errorf("got %v, %v", got, ok)
	}
}
