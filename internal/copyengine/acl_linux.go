//go:build linux

package copyengine

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func copyACL(in, out *os.File) error {
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		value, err := readAttribute(func(b []byte) (int, error) { return unix.Fgetxattr(int(in.Fd()), name, b) }, 4<<20)
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
			removeErr := unix.Fremovexattr(int(out.Fd()), name)
			if removeErr != nil && !errors.Is(removeErr, unix.ENODATA) && !errors.Is(removeErr, unix.ENOTSUP) {
				return fmt.Errorf("remove inherited ACL %s: %w", name, removeErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("read ACL %s: %w", name, err)
		}
		if err = unix.Fsetxattr(int(out.Fd()), name, value, 0); err != nil {
			return fmt.Errorf("write ACL %s: %w", name, err)
		}
	}
	return nil
}
func copyLinkACL(src, dst string) error { return nil }
