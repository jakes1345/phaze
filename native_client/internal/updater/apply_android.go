//go:build android
// +build android

package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

// Apply on Android: an app cannot silently overwrite its own APK without
// either Play-Store-managed updates or the OS package-installer flow. We
// hand the verified APK to the OS installer by writing it to the public
// Downloads directory and asking the system to open it.
//
// Real install requires the user to confirm via the Android UI. That is
// a deliberate platform constraint, not a placeholder — the same flow
// Signal, Tachiyomi, F-Droid, etc. all use for sideloaded updates.
//
// Returns nil if the APK was staged and an OPEN intent was dispatched;
// the caller may quit so the new install replaces the running app.
func Apply(downloadedPath string) error {
	// Move the verified APK into the OS Downloads directory so the
	// system file picker / package installer can read it without
	// content-URI plumbing.
	dlDir := "/storage/emulated/0/Download"
	if _, err := os.Stat(dlDir); err != nil {
		// Some OEMs use a different mount; fall back to app-private cache.
		dlDir = filepath.Dir(downloadedPath)
	}
	finalAPK := filepath.Join(dlDir, filepath.Base(downloadedPath))
	if finalAPK != downloadedPath {
		if err := os.Rename(downloadedPath, finalAPK); err != nil {
			// Cross-filesystem rename: fall back to copy+remove.
			if err := copyFile(downloadedPath, finalAPK); err != nil {
				return fmt.Errorf("stage APK to Downloads: %w", err)
			}
			_ = os.Remove(downloadedPath)
		}
	}

	// We have no JNI hook in this package to fire an Intent directly. The
	// caller (PhazeApp) already has fyne.App.OpenURL which on android-fyne
	// maps to Intent.ACTION_VIEW. Return NeedsUserActionError (defined in
	// manifest.go so callers on every platform can test for it) so the
	// caller can do app.OpenURL("file://" + finalAPK).
	return &NeedsUserActionError{Path: finalAPK}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
