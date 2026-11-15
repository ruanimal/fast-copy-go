//go:build darwin

package copyengine

import (
	"fmt"
	"github.com/ebitengine/purego"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	copyfileACL      = 1
	copyfileXattr    = 4
	copyfileNoFollow = (1 << 18) | (1 << 19)
)

var nativeCopy struct {
	sync.Once
	fcopy, copy uintptr
	err         error
}

func loadNativeCopy() error {
	nativeCopy.Do(func() {
		lib, e := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if e != nil {
			nativeCopy.err = e
			return
		}
		nativeCopy.fcopy, e = purego.Dlsym(lib, "fcopyfile")
		if e != nil {
			nativeCopy.err = e
			return
		}
		nativeCopy.copy, e = purego.Dlsym(lib, "copyfile")
		nativeCopy.err = e
	})
	return nativeCopy.err
}
func nativeResult(result, errno uintptr) error {
	if int32(result) == 0 {
		return nil
	}
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return fmt.Errorf("copyfile failed without an errno")
}
func nativeCopyFD(in, out *os.File, flags uintptr) error {
	if err := loadNativeCopy(); err != nil {
		return err
	}
	r, _, e := purego.SyscallN(nativeCopy.fcopy, in.Fd(), out.Fd(), 0, flags)
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	return nativeResult(r, e)
}
func nativeCopyLink(src, dst string, flags uintptr) error {
	if err := loadNativeCopy(); err != nil {
		return err
	}
	s, e := syscall.BytePtrFromString(src)
	if e != nil {
		return e
	}
	d, e := syscall.BytePtrFromString(dst)
	if e != nil {
		return e
	}
	r, _, n := purego.SyscallN(nativeCopy.copy, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(d)), 0, flags|copyfileNoFollow)
	runtime.KeepAlive(s)
	runtime.KeepAlive(d)
	return nativeResult(r, n)
}
