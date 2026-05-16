// Package updater fetches the Phaze release manifest from a Nexus server
// (/api/v1/version), compares versions, downloads + SHA-256-verifies a
// platform-specific asset, and applies the update with per-OS install logic.
//
// The "Apply" step is split across files by build tag:
//   - apply_linux.go   (desktop Linux, not Android)
//   - apply_windows.go
//   - apply_android.go (launches the system package installer)
//   - apply_darwin.go  (best-effort: opens the .dmg / release page)
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Manifest mirrors the JSON returned by Nexus /api/v1/version. Old servers
// may omit Platforms; in that case Apply is impossible and the client should
// fall back to opening the download page.
type Manifest struct {
	Version      string                   `json:"version"`
	ReleaseURL   string                   `json:"release_url"`
	URL          string                   `json:"url"` // legacy fallback
	ReleaseNotes string                   `json:"release_notes,omitempty"`
	PublishedAt  string                   `json:"published_at,omitempty"`
	Platforms    map[string]PlatformAsset `json:"platforms"`
}

type PlatformAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

// Fetch retrieves the manifest from the given Nexus API base
// (e.g. "https://phazechat.world"). Timeout is conservative so a slow
// gateway doesn't block app start.
func Fetch(apiBase string) (Manifest, error) {
	var m Manifest
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get(strings.TrimRight(apiBase, "/") + "/api/v1/version")
	if err != nil {
		return m, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("version endpoint status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return m, err
	}
	return m, nil
}

// CurrentPlatformKey is the lookup key for Manifest.Platforms on this OS.
// GOOS==android is reported by the Go toolchain when GOARCH=arm64 and the
// android-ndk linker is in use; otherwise GOOS is linux/windows/darwin.
func CurrentPlatformKey() string {
	if runtime.GOOS == "android" {
		return "android"
	}
	return runtime.GOOS // "linux", "windows", "darwin"
}

// PlatformAssetFor returns the asset for the running OS, or nil if the
// manifest has no platforms map or no entry for this OS (older servers, or
// release that didn't publish a binary for this target).
func (m Manifest) PlatformAssetFor(key string) *PlatformAsset {
	if m.Platforms == nil {
		return nil
	}
	a, ok := m.Platforms[key]
	if !ok {
		return nil
	}
	if a.URL == "" {
		return nil
	}
	return &a
}

// IsNewer reports whether `latest` is strictly newer than `current`. Both
// are expected in dotted form (e.g. "1.2.3", optional leading "v",
// optional pre-release suffix "1.2.3-rc1"). Falls back to plain string !=
// when either side fails to parse, so an exotic version string never
// silently skips an update notification.
func IsNewer(latest, current string) bool {
	la, lOK := parseVersion(latest)
	cu, cOK := parseVersion(current)
	if !lOK || !cOK {
		return strings.TrimSpace(latest) != "" && latest != current
	}
	for i := 0; i < 3; i++ {
		if la[i] > cu[i] {
			return true
		}
		if la[i] < cu[i] {
			return false
		}
	}
	// Same numeric components: a non-pre-release beats a pre-release of
	// the same numerics (1.2.3 > 1.2.3-rc1), per semver §11.
	lPre := preRelease(latest)
	cPre := preRelease(current)
	if lPre == "" && cPre != "" {
		return true
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, len(parts) > 0
}

func preRelease(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[i+1:]
	}
	return ""
}

// ErrUnsupported is returned by Apply when there is no install strategy
// for the running platform (callers should fall back to opening the
// release URL in a browser).
var ErrUnsupported = errors.New("updater: in-place install not supported on this platform")

// NeedsUserActionError signals that Apply staged the update but the OS
// requires explicit user approval to install it (Android: package
// installer intent). The Path field is the on-disk location of the
// verified artifact the caller should hand to fyne.App.OpenURL.
type NeedsUserActionError struct {
	Path string
}

func (e *NeedsUserActionError) Error() string {
	return "updater: artifact staged at " + e.Path + "; user must approve install"
}

// AsNeedsUserAction returns (path, true) if err is a *NeedsUserActionError.
// Safe to call on any platform — on non-Android, Apply never returns this
// sentinel so the call simply returns ("", false).
func AsNeedsUserAction(err error) (string, bool) {
	var u *NeedsUserActionError
	if errors.As(err, &u) {
		return u.Path, true
	}
	return "", false
}
