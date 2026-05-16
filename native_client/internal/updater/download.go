package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DownloadAndVerify streams the asset to a temp file, computes its SHA-256
// while copying (one pass), and atomically renames into dest on success.
// Returns the final on-disk path so the caller can hand it to Apply.
//
// Refuses to download if the manifest has no SHA-256 — auto-installing an
// unverified binary downloaded from an external URL is a remote-code-
// execution primitive. The caller should fall back to opening the release
// page in a browser in that case.
func DownloadAndVerify(a PlatformAsset, destDir string) (string, error) {
	if a.URL == "" {
		return "", errors.New("updater: empty URL")
	}
	if a.SHA256 == "" {
		return "", errors.New("updater: manifest has no SHA-256, refusing to auto-install")
	}
	if a.Name == "" {
		return "", errors.New("updater: manifest has no asset name")
	}
	if strings.ContainsAny(a.Name, "/\\") {
		return "", fmt.Errorf("updater: asset name %q contains path separator", a.Name)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	finalPath := destDir + string(os.PathSeparator) + a.Name
	tmpPath := finalPath + ".part"

	client := &http.Client{Timeout: 5 * time.Minute}
	req, _ := http.NewRequest(http.MethodGet, a.URL, nil)
	req.Header.Set("User-Agent", "Phaze-Updater/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d from %s", resp.StatusCode, a.URL)
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	// Cap at 256 MiB to prevent disk-fill / runaway downloads. Real builds
	// are well under 100 MiB.
	const maxBytes = 256 << 20
	written, err := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(resp.Body, maxBytes+1))
	closeErr := f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", closeErr
	}
	if written > maxBytes {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download exceeded %d bytes", maxBytes)
	}
	if a.Size > 0 && written != a.Size {
		os.Remove(tmpPath)
		return "", fmt.Errorf("size mismatch: got %d, manifest says %d", written, a.Size)
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, a.SHA256) {
		os.Remove(tmpPath)
		return "", fmt.Errorf("SHA-256 mismatch: got %s, manifest says %s", got, a.SHA256)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return finalPath, nil
}

// StagingDir is a per-OS scratch directory for the downloaded artifact.
// It is deliberately NOT the same directory as the running binary, so a
// partial download or failed verify cannot brick the current install.
func StagingDir() string {
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		d = os.TempDir()
	}
	return d + string(os.PathSeparator) + "phaze-update"
}
