//go:build android

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// readFCMToken returns the FCM registration token written by PhazeFCMService.onNewToken,
// or "" if not available yet (first run before token is issued).
func readFCMToken() string {
	// On Android, os.UserCacheDir() returns the app's files dir sibling; use the
	// standard data directory written by the Java service.
	dataDir := os.Getenv("ANDROID_DATA")
	if dataDir == "" {
		// Fallback: relative path works because Fyne sets CWD to the app's files dir.
		dataDir = "."
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "phaze_fcm_token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
