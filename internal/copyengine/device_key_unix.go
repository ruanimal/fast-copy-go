//go:build linux || darwin

package copyengine

import (
	"os"
	"syscall"
)

func deviceKey(path string) (uint64, bool) {
	i, e := os.Stat(path)
	if e != nil {
		return 0, false
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
