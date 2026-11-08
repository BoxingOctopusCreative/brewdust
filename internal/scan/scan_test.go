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

func TestParseLastUsed(t *testing.T) {
	launched := "2026-10-11 22:52:20 +0000\x002026-09-24 05:43:18 +0000"
	if got, ok := parseLastUsed(launched); !ok || !got.Equal(time.Date(2026, 10, 11, 22, 52, 20, 0, time.UTC)) {
		t.Errorf("indexed app: got %v, %v", got, ok)
	}
	// mdls echoes the modification date for bundles Spotlight hasn't indexed.
	if _, ok := parseLastUsed("2026-11-08 14:24:51 +0000\x002026-11-08 14:24:51 +0000"); ok {
		t.Error("last-used equal to content change should mean no record")
	}
	if _, ok := parseLastUsed("(null)\x002026-11-08 14:24:51 +0000"); ok {
		t.Error("never launched should mean no record")
	}
	if _, ok := parseLastUsed("2026-10-11 22:52:20 +0000"); !ok {
		t.Error("a lone date should still parse")
	}
}

func TestAppLastUsedUnindexedBundle(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Fresh.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := AppLastUsed(app); ok {
		t.Errorf("a bundle that was never opened reported as used at %v", got)
	}
}
