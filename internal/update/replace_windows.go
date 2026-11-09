package update

import (
	"errors"
	"os"
)

// replaceBinary swaps in a new exe. A running exe can't be overwritten on
// Windows but it can be renamed, so move it aside first. The leftover .old
// file is removed by CleanupOld on the next start.
func replaceBinary(tmp, self string) error {
	old := self + ".old"
	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Rename(old, self)
		return err
	}
	return nil
}

// CleanupOld removes the exe left behind by a previous update.
func CleanupOld() {
	if self, err := os.Executable(); err == nil {
		_ = os.Remove(self + ".old")
	}
}
