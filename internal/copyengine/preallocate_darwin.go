//go:build darwin

package copyengine

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func preallocate(f *os.File, size int64) (bool, error) {
	request := unix.Fstore_t{Flags: unix.F_ALLOCATEALL, Posmode: unix.F_PEOFPOSMODE, Length: size}
	err := unix.FcntlFstore(f.Fd(), unix.F_PREALLOCATE, &request)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		return false, nil
	}
	return err == nil, err
}
