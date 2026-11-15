//go:build linux

package copyengine

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func renameNoReplace(temp, target string) error {
	err := unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	if err = os.Link(temp, target); err != nil {
		return err
	}
	return os.Remove(temp)
}
