//go:build darwin

package copyengine

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func renameNoReplace(temp, target string) error {
	err := unix.RenamexNp(temp, target, unix.RENAME_EXCL)
	if !errors.Is(err, unix.ENOTSUP) && !errors.Is(err, unix.EINVAL) {
		return err
	}
	if err = os.Link(temp, target); err != nil {
		return err
	}
	return os.Remove(temp)
}
