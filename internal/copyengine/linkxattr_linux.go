//go:build linux

package copyengine

import (
	"bytes"
	"fmt"
	"golang.org/x/sys/unix"
)

func copyLinkXattrs(src, dst string) error {
	list, err := readAttribute(func(b []byte) (int, error) { return unix.Llistxattr(src, b) }, 4<<20)
	if err != nil {
		return fmt.Errorf("list link attributes: %w", err)
	}
	for _, name := range bytes.Split(list, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		n := string(name)
		value, err := readAttribute(func(b []byte) (int, error) { return unix.Lgetxattr(src, n, b) }, 64<<20)
		if err != nil {
			return fmt.Errorf("read link attribute %q: %w", n, err)
		}
		if err = unix.Lsetxattr(dst, n, value, 0); err != nil {
			return fmt.Errorf("write link attribute %q: %w", n, err)
		}
	}
	return nil
}
