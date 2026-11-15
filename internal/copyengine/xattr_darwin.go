//go:build darwin

package copyengine

import (
	"fmt"
	"os"
)

func copyXattrs(in, out *os.File) error {
	if err := nativeCopyFD(in, out, copyfileXattr); err != nil {
		return fmt.Errorf("copy extended attributes: %w", err)
	}
	return nil
}
func copyDirectoryXattrs(src, dst string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.Open(dst)
	if e != nil {
		return e
	}
	defer out.Close()
	return copyXattrs(in, out)
}
