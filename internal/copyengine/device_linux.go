//go:build linux

package copyengine

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Resolve partitions to their containing block device. Virtual device stacks
// are intentionally unknown: different logical volumes may share hardware.
func deviceInfo(path string) (device, bool) {
	var st syscall.Stat_t
	if syscall.Stat(path, &st) != nil {
		return device{}, false
	}
	dev := uint64(st.Dev)
	sys, err := filepath.EvalSymlinks(fmt.Sprintf("/sys/dev/block/%d:%d", unix.Major(dev), unix.Minor(dev)))
	if err != nil || strings.Contains(sys, "/virtual/") {
		return device{}, false
	}
	for p := sys; p != "/" && strings.HasPrefix(p, "/sys/"); p = filepath.Dir(p) {
		data, err := os.ReadFile(filepath.Join(p, "queue", "rotational"))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(data)) {
		case "0":
			return device{id: p, rotational: false}, true
		case "1":
			return device{id: p, rotational: true}, true
		default:
			return device{}, false
		}
	}
	return device{}, false
}
