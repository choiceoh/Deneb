package plugin

import (
	"io/fs"
	"syscall"
)

// fileUIDFromInfo extracts the UID from a file info on Unix systems.
func fileUIDFromInfo(info fs.FileInfo) int {
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(sys.Uid)
	}
	return -1
}
