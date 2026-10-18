package service

import (
	"errors"
	"fmt"
	"os"
)

func moveSQLiteFiles(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	moved := []string{""}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Rename(from+suffix, to+suffix); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			for i := len(moved) - 1; i >= 0; i-- {
				if restoreErr := os.Rename(to+moved[i], from+moved[i]); restoreErr != nil {
					return fmt.Errorf("move SQLite sidecar %s: %w; rollback %s: %v", from+suffix, err, to+moved[i], restoreErr)
				}
			}
			return fmt.Errorf("move SQLite sidecar %s: %w", from+suffix, err)
		}
		moved = append(moved, suffix)
	}
	return nil
}

func removeSQLiteFiles(path string) error {
	for _, suffix := range []string{"-shm", "-wal", ""} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
