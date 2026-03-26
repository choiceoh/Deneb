//go:build windows

package plugin

import "io/fs"

// fileUIDFromInfo returns -1 on Windows (UID not applicable).
func fileUIDFromInfo(_ fs.FileInfo) int {
	return -1
}
