//go:build !linux && !darwin

package copyengine

import (
	"errors"
	"os"
)

func copyACL(in, out *os.File) error    { return errors.New("ACL copy unsupported") }
func copyLinkACL(src, dst string) error { return errors.New("ACL copy unsupported") }
