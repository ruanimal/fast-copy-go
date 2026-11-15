//go:build linux

package copyengine

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func preallocate(f *os.File, size int64) (bool, error) {
	err := unix.Fallocate(int(f.Fd()), unix.FALLOC_FL_KEEP_SIZE, 0, size)
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return false, nil
	}
	return err == nil, err
}
