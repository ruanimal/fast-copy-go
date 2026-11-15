//go:build !linux && !darwin

package copyengine

import (
	"errors"
	"os"
)

func copyXattrs(in, out *os.File) error {
	return errors.New("extended attributes are unsupported on this platform")
}
func copyDirectoryXattrs(src, dst string) error {
	return errors.New("extended attributes are unsupported on this platform")
}
