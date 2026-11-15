//go:build linux || darwin

package copyengine

import (
	"os"
	"syscall"
)

func maybeSparse(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return !ok || s.Blocks < 0 || s.Blocks < ((info.Size()+511)/512)
}
