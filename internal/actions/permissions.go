// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package actions

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

// owner is who installed packages should belong to: the user running
// brewdust, as brew itself expects.
var owner = func() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}()

// sudo is the command used to change ownership; tests replace it.
var sudo = "sudo"

// fixPermissions runs a step's chown (with sudo) and then restores the owner
// permissions brew needs: u+rwx on directories, so it can list them and
// delete what's in them, and u+r on files. Files brew made read-only stay
// read-only.
func fixPermissions(ctx context.Context, prefix string, s Step, out io.Writer, detached bool) error {
	for _, root := range append(append([]string(nil), s.Chown...), s.Modes...) {
		if err := checkPackageRoot(prefix, root); err != nil {
			return err
		}
	}
	if len(s.Chown) > 0 {
		if s.Owner == "" {
			return fmt.Errorf("don't know which user to give %s to", strings.Join(s.Chown, ", "))
		}
		args := append([]string{"chown", "-R", s.Owner}, s.Chown...)
		cmd := exec.CommandContext(ctx, sudo, args...)
		cmd.Stdout, cmd.Stderr = out, out
		if detached {
			// No terminal to ask for a password on: sudo fails, and the TUI
			// offers to rerun the step in the terminal (see NeedsTerminal).
			cmd.Args = append([]string{sudo, "-n"}, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		} else {
			cmd.Stdin = os.Stdin
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo chown -R %s %s: %w", s.Owner, strings.Join(s.Chown, " "), err)
		}
		fmt.Fprintf(out, "Gave %s to %s\n", strings.Join(s.Chown, ", "), s.Owner)
	}
	for _, root := range s.Modes {
		n, err := restoreModes(root)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Restored owner permissions on %d entries under %s\n", n, root)
	}
	return nil
}

// restoreModes adds the owner permissions brew needs wherever they're
// missing under root, skipping symlinks and anything owned by someone else.
func restoreModes(root string) (int, error) {
	uid := uint32(os.Getuid())
	fixed := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uid {
			return nil
		}
		mode := info.Mode().Perm()
		var want fs.FileMode
		switch {
		case d.IsDir():
			want = mode | 0o700
		case d.Type().IsRegular():
			want = mode | 0o400
		default:
			return nil
		}
		if want != mode {
			// A directory's new mode applies before WalkDir reads it.
			if err := os.Chmod(path, want); err != nil {
				return err
			}
			fixed++
		}
		return nil
	})
	return fixed, err
}

// checkPackageRoot refuses anything but <prefix>/Cellar/<name> or
// <prefix>/Caskroom/<token>.
func checkPackageRoot(prefix, p string) error {
	for _, dir := range []string{"Cellar", "Caskroom"} {
		rel, err := filepath.Rel(filepath.Join(prefix, dir), filepath.Clean(p))
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && !strings.HasPrefix(rel, ".") && !strings.Contains(rel, "/") {
			return nil
		}
	}
	return fmt.Errorf("refusing to change permissions of %s: not a package directory under %s", p, prefix)
}
