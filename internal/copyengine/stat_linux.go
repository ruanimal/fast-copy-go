//go:build linux

package copyengine

import (
	"os"
	"syscall"
)

func fileInodeOS(info os.FileInfo) uint64 { return info.Sys().(*syscall.Stat_t).Ino }
func fileDevice(info os.FileInfo) uint64  { return uint64(info.Sys().(*syscall.Stat_t).Dev) }
