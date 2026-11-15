//go:build darwin

package copyengine

import (
	"golang.org/x/sys/unix"
	"os"
)

func configureBatchInput(f *os.File) { _, _ = unix.FcntlInt(f.Fd(), unix.F_NOCACHE, 1) }
