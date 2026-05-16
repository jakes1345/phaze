//go:build windows
// +build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Apply on Windows: Win32 refuses to overwrite a running .exe, so we
// spawn a CMD batch that waits for the current PID to exit, copies the
// new binary over, and relaunches it. Detached + minimised so the user
// doesn't see a console flash.
func Apply(downloadedPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	pid := os.Getpid()
	batPath := downloadedPath + ".swap.bat"
	// Use ROBOCOPY for the move (handles AV-locked files better than COPY).
	// /MT:1 keeps it single-threaded; the source directory is just our temp.
	src := downloadedPath
	dstDir := filepath.Dir(exe)
	dstName := filepath.Base(exe)
	srcDir := filepath.Dir(src)
	srcName := filepath.Base(src)

	bat := fmt.Sprintf(`@echo off
setlocal
set "PID=%d"
:waitloop
tasklist /FI "PID eq %%PID%%" 2>NUL | find "%%PID%%" >NUL
if %%errorlevel%%==0 (
  timeout /t 1 /nobreak >NUL
  goto waitloop
)
robocopy %q %q %q /MT:1 /R:5 /W:1 /NJH /NJS /NDL /NFL /NC /NS >NUL
if exist %q (
  move /Y %q %q >NUL
)
del /F /Q %q
start "" %q
del /F /Q "%%~f0"
endlocal
`, pid,
		srcDir, dstDir, srcName,
		filepath.Join(dstDir, srcName), filepath.Join(dstDir, srcName), filepath.Join(dstDir, dstName),
		src,
		exe)

	if err := os.WriteFile(batPath, []byte(bat), 0o644); err != nil {
		return fmt.Errorf("write swap script: %w", err)
	}

	// /B = no new window, but we still want the batch to keep running
	// when we exit, so use START /MIN with cmd /c.
	cmd := exec.Command("cmd", "/C", "start", "/min", "", "cmd", "/c", batPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch swap script: %w", err)
	}
	_ = cmd.Process.Release()
	// Touch a sentinel so packaging-time inspection knows this binary
	// expects the swap script to take over.
	_ = touchSentinel(strings.Join([]string{batPath, exe}, "\n"))
	return nil
}

func touchSentinel(_ string) error { return nil }
