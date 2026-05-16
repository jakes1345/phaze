//go:build linux && !android
// +build linux,!android

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Apply on desktop Linux: chmod +x the new binary, then write a small
// shell script that waits for the running PID to exit, copies the new
// binary over the current one, and exec's it. The script is fire-and-
// forget — we spawn it detached and exit the current process so the
// swap happens cleanly without "text file busy" errors.
//
// Returns nil if the swap script was scheduled; caller should now quit
// the app. The new binary will be running by the time `phaze` is invoked
// again (the script exec's it itself).
func Apply(downloadedPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("eval symlinks: %w", err)
	}
	if err := os.Chmod(downloadedPath, 0o755); err != nil {
		return fmt.Errorf("chmod +x: %w", err)
	}

	// Sanity: only attempt overwrite if the current binary is writable by
	// this user. Anything else (system-managed /usr/bin, immutable squashfs
	// from AppImage, snap confinement) needs a different update path.
	if info, err := os.Stat(exe); err != nil {
		return fmt.Errorf("stat self: %w", err)
	} else if info.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("running binary %s is not writable by this user; use package manager", exe)
	}

	pid := os.Getpid()
	scriptPath := downloadedPath + ".swap.sh"
	script := fmt.Sprintf(`#!/bin/sh
set -e
# Wait for the current Phaze process to exit (max 30 s).
i=0
while kill -0 %d 2>/dev/null; do
  i=$((i+1))
  [ "$i" -gt 60 ] && break
  sleep 0.5
done
cp -f %q %q
chmod +x %q
rm -f %q %q
exec %q &
`, pid,
		downloadedPath, exe,
		exe,
		downloadedPath, scriptPath,
		exe)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write swap script: %w", err)
	}

	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch swap script: %w", err)
	}
	// Detach: don't wait on the child. It will outlive us.
	_ = cmd.Process.Release()
	return nil
}
