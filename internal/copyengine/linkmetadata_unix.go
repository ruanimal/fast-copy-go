//go:build linux || darwin

package copyengine

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func copyLinkMetadata(src, dst string, info os.FileInfo, preserveXattrs, preserveACL bool) error {
	if preserveXattrs {
		if err := copyLinkXattrs(src, dst); err != nil {
			return err
		}
	}
	ts, err := unix.TimeToTimespec(info.ModTime())
	if err != nil {
		return fmt.Errorf("link timestamp: %w", err)
	}
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, dst, []unix.Timespec{ts, ts}, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("set link timestamp: %w", err)
	}
	if preserveACL {
		return copyLinkACL(src, dst)
	}
	return nil
}
