//go:build darwin

package copyengine

import (
	"golang.org/x/sys/unix"
	"os"
)

func configureCache(in, out *os.File) string {
	_, a := unix.FcntlInt(in.Fd(), unix.F_NOCACHE, 1)
	_, b := unix.FcntlInt(out.Fd(), unix.F_NOCACHE, 1)
	if a == nil && b == nil {
		return "nocache"
	}
	return "buffered"
}
func releaseCache(f *os.File) {}
