//go:build darwin

package copyengine

import (
	"fmt"
	"os"
)

func copyACL(in, out *os.File) error {
	if err := nativeCopyFD(in, out, copyfileACL); err != nil {
		return fmt.Errorf("copy ACL: %w", err)
	}
	return nil
}
func copyLinkACL(src, dst string) error {
	if err := nativeCopyLink(src, dst, copyfileACL); err != nil {
		return fmt.Errorf("copy link ACL: %w", err)
	}
	return nil
}
