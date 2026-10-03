// Package persist provides atomic file persistence helpers shared by
// config, session, and memory stores. A write goes to a temporary
// sibling file, is fsynced, closed, permission-set, and then renamed
// over the destination, so readers never observe a partially written
// file and the previous valid file survives a failed save.
package persist

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// WriteFileAtomically writes data to path with the given permission,
// replacing any existing file atomically where the platform supports
// it. Temporary files are always removed; on failure the previous
// valid destination is left untouched.
func WriteFileAtomically(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".lato-tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("set permissions on temporary file: %w", err)
	}

	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// renameAttempts bounds retries for transient Windows "access denied"
// holds (antivirus, file indexers). On Unix the first attempt is
// normally final and retries add no delay.
const renameAttempts = 10

func replaceFile(src, dst string) error {
	var err error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 10 * time.Millisecond)
		}
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		if runtime.GOOS != "windows" {
			break
		}
	}
	return err
}
