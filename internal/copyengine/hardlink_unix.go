//go:build linux || darwin

package copyengine

import (
	"os"
	"syscall"
)

type hardlinkID struct {
	dev uint64
	ino uint64
}

func hardlinkIdentity(info os.FileInfo) (hardlinkID, bool) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Nlink < 2 {
		return hardlinkID{}, false
	}
	return hardlinkID{uint64(s.Dev), uint64(s.Ino)}, true
}
