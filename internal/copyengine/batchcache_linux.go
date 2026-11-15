//go:build linux

package copyengine

import (
	"golang.org/x/sys/unix"
	"os"
)

func configureBatchInput(f *os.File) { _ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_SEQUENTIAL) }
