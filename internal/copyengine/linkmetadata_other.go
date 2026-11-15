//go:build !linux && !darwin

package copyengine

import (
	"errors"
	"os"
)

func copyLinkMetadata(src, dst string, info os.FileInfo, preserveXattrs, preserveACL bool) error {
	return errors.New("symbolic link metadata unsupported on this platform")
}
