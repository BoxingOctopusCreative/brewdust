// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain puts a brew and a sudo that always fail first on PATH, so no test
// can change the real Homebrew installation, even by starting a run.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "brewdust-tui-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"brew", "sudo"} {
		stub := "#!/bin/sh\necho \"" + name + " is disabled in tests\" >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
