//go:build !windows

package update

import "os"

func replaceBinary(tmp, self string) error { return os.Rename(tmp, self) }

// CleanupOld is a no-op outside Windows.
func CleanupOld() {}
