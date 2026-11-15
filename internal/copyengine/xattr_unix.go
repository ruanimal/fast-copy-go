//go:build linux

package copyengine

import (
	"bytes"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

// copyXattrs fails explicitly if an attribute cannot be preserved. It never
// silently declares a metadata-preserving copy successful.
func copyXattrs(in, out *os.File) error {
	list, err := readAttribute(func(b []byte) (int, error) { return unix.Flistxattr(int(in.Fd()), b) }, 4<<20)
	if err != nil {
		return fmt.Errorf("list extended attributes: %w", err)
	}
	for _, name := range bytes.Split(list, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		n := string(name)
		if n == "system.posix_acl_access" || n == "system.posix_acl_default" {
			continue
		}
		value, e := readAttribute(func(b []byte) (int, error) { return unix.Fgetxattr(int(in.Fd()), n, b) }, 64<<20)
		if e != nil {
			return fmt.Errorf("read extended attribute %q: %w", n, e)
		}
		if e = unix.Fsetxattr(int(out.Fd()), n, value, 0); e != nil {
			return fmt.Errorf("write extended attribute %q: %w", n, e)
		}
	}
	return nil
}
func copyDirectoryXattrs(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return copyXattrs(in, out)
}
