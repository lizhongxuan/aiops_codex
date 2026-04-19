package filepatch

import (
	"fmt"
	"os"
	"path/filepath"
)

// Rollback restores files to their pre-patch state using the provided backups.
// The backups map keys are file paths relative to cwd, and values are the original
// file contents (nil value means the file did not exist and should be removed).
func Rollback(action *PatchAction, cwd string, backups map[string][]byte) error {
	var rollbackErr error
	for relPath, content := range backups {
		absPath := filepath.Join(cwd, relPath)
		if content == nil {
			// File did not exist before patch — remove it
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				rollbackErr = fmt.Errorf("rollback: failed to remove %s: %w", relPath, err)
			}
		} else {
			// Restore original content
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				rollbackErr = fmt.Errorf("rollback: failed to create dir for %s: %w", relPath, err)
				continue
			}
			if err := os.WriteFile(absPath, content, 0o644); err != nil {
				rollbackErr = fmt.Errorf("rollback: failed to restore %s: %w", relPath, err)
			}
		}
	}
	return rollbackErr
}

// captureBackup records the current state of a file before a change is applied.
// For files that don't exist yet (creates), it stores nil to indicate removal on rollback.
// For renames, it captures both the source file and marks the destination as non-existent.
func captureBackup(fc *FileChange, cwd string, backups map[string][]byte) error {
	switch fc.Mode {
	case ModeCreate:
		// File doesn't exist yet; on rollback we remove it
		backups[fc.NewPath] = nil

	case ModeDelete:
		// Capture existing content so we can restore on rollback
		absPath := filepath.Join(cwd, fc.OldPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("backup: cannot read %s: %w", fc.OldPath, err)
		}
		backups[fc.OldPath] = data

	case ModeRename:
		// Capture source file content and mark destination as non-existent
		absPath := filepath.Join(cwd, fc.OldPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("backup: cannot read %s: %w", fc.OldPath, err)
		}
		backups[fc.OldPath] = data
		backups[fc.NewPath] = nil

	case ModeModify:
		path := fc.NewPath
		if path == "" {
			path = fc.OldPath
		}
		absPath := filepath.Join(cwd, path)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("backup: cannot read %s: %w", path, err)
		}
		backups[path] = data

	default:
		return fmt.Errorf("backup: unknown change mode: %s", fc.Mode)
	}
	return nil
}
