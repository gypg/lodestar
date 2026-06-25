package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/shutdown"
)

func UpdateCore() error {
	log.Infof("start update core")

	filename, err := getDownloadFilename()
	if err != nil {
		log.Warnf("update core failed: %v", err)
		return err
	}

	downloadUrl := getUpdateURL() + "/" + filename
	log.Infof("download url: %s", downloadUrl)
	data, err := doRequestWithFallback(downloadUrl)
	if err != nil {
		log.Warnf("download failed: %v", err)
		return err
	}

	// Verify SHA256 checksum before applying the update.
	// Returns error only on mismatch (blocks update); missing checksum file
	// merely logs a warning and returns nil (non-blocking).
	if err := verifyDownloadChecksum(data, filename); err != nil {
		log.Warnf("update aborted: %v", err)
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		log.Warnf("get executable path failed: %v", err)
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		log.Warnf("eval symlinks failed: %v", err)
		return err
	}

	// Extract to a temporary directory first. This avoids partial writes
	// and, on Windows, avoids trying to overwrite the locked running exe.
	tmpDir, err := os.MkdirTemp(filepath.Dir(execPath), ".update-*")
	if err != nil {
		log.Warnf("create temp dir failed: %v", err)
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := unzip(data, tmpDir); err != nil {
		log.Warnf("unzip failed: %v", err)
		return err
	}

	if runtime.GOOS == "windows" {
		if err := windowsReplaceExecutable(execPath, tmpDir); err != nil {
			log.Warnf("windows replace executable failed: %v", err)
			return err
		}
	} else {
		// On Unix the running binary can be overwritten directly.
		if err := unzip(data, filepath.Dir(execPath)); err != nil {
			log.Warnf("unzip to target dir failed: %v", err)
			return err
		}
	}

	log.Infof("update core success")
	go restartExecutable(execPath)
	return nil
}

// windowsReplaceExecutable works around the Windows file-lock that prevents
// overwriting a running .exe. Strategy:
//  1. Rename the running exe to <name>.old (rename works on locked files).
//  2. Copy the new exe from the temp directory to the original path.
//  3. The caller then starts the new binary and exits.
func windowsReplaceExecutable(execPath, tmpDir string) error {
	baseName := filepath.Base(execPath)
	newExecPath := filepath.Join(tmpDir, baseName)

	if _, err := os.Stat(newExecPath); err != nil {
		return fmt.Errorf("new executable not found in update archive: %s", baseName)
	}

	// Step 1: Rename the running exe — this succeeds even while the file is locked.
	oldExecPath := execPath + ".old"
	_ = os.Remove(oldExecPath) // clean up stale backup from a previous update
	if err := os.Rename(execPath, oldExecPath); err != nil {
		return fmt.Errorf("rename running executable: %w", err)
	}

	// Step 2: Copy the new exe to the original path.
	if err := copyFile(newExecPath, execPath); err != nil {
		// Rollback: restore the original exe.
		_ = os.Rename(oldExecPath, execPath)
		return fmt.Errorf("copy new executable: %w", err)
	}

	// Make the new exe executable (should already be, but just in case).
	_ = os.Chmod(execPath, 0755)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func getDownloadFilename() (string, error) {
	arch := runtime.GOARCH
	goos := runtime.GOOS

	switch goos {
	case "windows":
		switch arch {
		case "386":
			return "lodestar-windows-x86.zip", nil
		case "amd64":
			return "lodestar-windows-x86_64.zip", nil
		}
	case "darwin":
		switch arch {
		case "amd64":
			return "lodestar-darwin-x86_64.zip", nil
		case "arm64":
			return "lodestar-darwin-arm64.zip", nil
		}
	case "linux":
		switch arch {
		case "386":
			return "lodestar-linux-x86.zip", nil
		case "amd64":
			return "lodestar-linux-x86_64.zip", nil
		case "arm":
			return "lodestar-linux-armv7.zip", nil
		case "arm64":
			return "lodestar-linux-arm64.zip", nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s", goos, arch)
}

func restartExecutable(execPath string) {
	shutdown.Shutdown()

	log.Infof("restarting: %q %q", execPath, os.Args[1:])

	if runtime.GOOS == "windows" {
		// Clean up the old (renamed) executable after the new process starts.
		oldExecPath := execPath + ".old"
		cmd := exec.Command(execPath, os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			log.Errorf("restarting failed: %v", err)
			// Rollback: restore old executable.
			_ = os.Remove(execPath)
			_ = os.Rename(oldExecPath, execPath)
		}
		// Best-effort cleanup of the old binary. The new process may have
		// already started, so the file might still be briefly locked.
		_ = os.Remove(oldExecPath)
		os.Exit(0)
	}

	if err := syscall.Exec(execPath, os.Args, os.Environ()); err != nil {
		log.Errorf("restarting failed: %v", err)
	}
}

// verifyDownloadChecksum attempts to download a .sha256 checksum file for the
// given archive and verifies the SHA256 hash of the downloaded data.
//
// It follows the convention: <filename>.sha256 contains the hex-encoded hash.
// Common formats supported:
//   - bare hex hash:                "a1b2c3..."
//   - sha256sum-style output:      "a1b2c3...  filename.zip"
//
// If the checksum file is not available (404 or download error), the function
// logs a warning and returns without blocking the update.
// If the checksum file is available but the hash does not match, the function
// returns an error to abort the update.
func verifyDownloadChecksum(data []byte, filename string) error {
	checksumURL := getUpdateURL() + "/" + filename + ".sha256"
	log.Infof("attempting checksum download: %s", checksumURL)

	checksumData, err := doRequestWithFallback(checksumURL)
	if err != nil {
		// Checksum file not available — log and continue (non-blocking).
		log.Warnf("WARNING: no checksum verification available for %s: %v", filename, err)
		log.Warnf("WARNING: no checksum verification — update proceeding without integrity check")
		return nil
	}

	expectedHash := parseChecksumFile(checksumData)
	if expectedHash == "" {
		log.Warnf("WARNING: checksum file for %s is empty or unparseable, skipping verification", filename)
		return nil
	}

	actualHash := sha256Hex(data)
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf(
			"checksum verification FAILED for %s: expected %s, got %s — update aborted to prevent applying a corrupted or tampered binary",
			filename, expectedHash, actualHash,
		)
	}

	log.Infof("checksum verification PASSED for %s (sha256: %s)", filename, actualHash)
	return nil
}

// sha256Hex returns the lowercase hex-encoded SHA256 digest of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// parseChecksumFile extracts the hex-encoded SHA256 hash from a checksum file.
// It handles both bare-hex and sha256sum-style ("hex  filename") formats.
func parseChecksumFile(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	// sha256sum output: "hash  filename" — take first field.
	if idx := strings.IndexAny(s, " \t"); idx > 0 {
		s = s[:idx]
	}
	// Validate: must be a valid hex string of 64 characters.
	if len(s) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(s); err != nil {
		return ""
	}
	return s
}
