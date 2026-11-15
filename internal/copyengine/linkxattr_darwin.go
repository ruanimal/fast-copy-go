//go:build darwin

package copyengine

import "fmt"

func copyLinkXattrs(src, dst string) error {
	if err := nativeCopyLink(src, dst, copyfileXattr); err != nil {
		return fmt.Errorf("copy link attributes: %w", err)
	}
	return nil
}
