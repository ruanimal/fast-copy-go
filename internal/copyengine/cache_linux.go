//go:build linux

package copyengine

import (
	"golang.org/x/sys/unix"
	"os"
)

func configureCache(in, out *os.File) string {
	_ = unix.Fadvise(int(in.Fd()), 0, 0, unix.FADV_SEQUENTIAL)
	return "buffered"
}
func releaseCache(f *os.File) { _ = unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED) }
