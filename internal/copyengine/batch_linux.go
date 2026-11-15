//go:build linux

package copyengine

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"hash"
	"io"
	"os"
)

// Direct reads and writes retain the bounded batch while respecting the
// filesystem's reported memory and offset alignment.
func batchOpenDirect(path string, flags int, original *os.File, inferTarget bool) (int, int, int, bool, error) {
	fd, err := unix.Open(path, flags|unix.O_DIRECT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if directUnsupported(err) {
			return -1, 0, 0, false, nil
		}
		return -1, 0, 0, false, err
	}
	var mem, alignment int
	if inferTarget {
		var reason string
		mem, alignment, _, reason = targetDirectAlignment(fd)
		if reason != "" {
			unix.Close(fd)
			return -1, 0, 0, false, nil
		}
	} else {
		var st unix.Statx_t
		if unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_DIOALIGN, &st) != nil || st.Mask&unix.STATX_DIOALIGN == 0 || st.Dio_mem_align == 0 || st.Dio_offset_align == 0 {
			unix.Close(fd)
			return -1, 0, 0, false, nil
		}
		mem, alignment = int(st.Dio_mem_align), int(st.Dio_offset_align)
	}
	if mem > 1024*1024 || alignment > 1024*1024 {
		unix.Close(fd)
		return -1, 0, 0, false, nil
	}
	var identity unix.Stat_t
	if err = unix.Fstat(fd, &identity); err != nil {
		unix.Close(fd)
		return -1, 0, 0, false, err
	}
	info, err := original.Stat()
	if err != nil {
		unix.Close(fd)
		return -1, 0, 0, false, err
	}
	if uint64(identity.Ino) != fileInode(info) || uint64(identity.Dev) != fileDevice(info) {
		unix.Close(fd)
		return -1, 0, 0, false, errors.New("file changed before batch I/O")
	}
	return fd, mem, alignment, true, nil
}
func batchReadDirect(ctx context.Context, en entry, in *os.File, data []byte, opt Options) (bool, error) {
	fd, mem, alignment, ok, err := batchOpenDirect(en.src, unix.O_RDONLY, in, false)
	if !ok || err != nil {
		return ok, err
	}
	defer unix.Close(fd)
	size := roundUp(opt.BufferSize, alignment)
	bufs, err := opt.scratch.direct.acquire(size, mem, 1)
	if err != nil {
		return true, err
	}
	buf := bufs[0]
	for off := 0; off < len(data); off += size {
		if err = waitForTask(ctx, opt); err != nil {
			return true, err
		}
		want := min(size, len(data)-off)
		n, e := unix.Pread(fd, buf, int64(off))
		if e != nil {
			if off == 0 && directUnsupported(e) {
				return false, nil
			}
			return true, e
		}
		if n != want {
			return true, io.ErrUnexpectedEOF
		}
		copy(data[off:off+n], buf[:n])
	}
	return true, nil
}
func batchWriteDirect(ctx context.Context, out *os.File, data []byte, sourceDirect bool, opt Options, p *Progress, emit func(), h hash.Hash) (bool, error) {
	fd, mem, alignment, ok, err := batchOpenDirect(out.Name(), unix.O_WRONLY, out, opt.TargetDirect)
	if !ok || err != nil {
		return ok, err
	}
	defer unix.Close(fd)
	size := roundUp(opt.BufferSize, alignment)
	bufs, err := opt.scratch.direct.acquire(size, mem, 1)
	if err != nil {
		return true, err
	}
	buf := bufs[0]
	pwrite := unix.Pwrite
	if opt.targetWriteFn != nil {
		pwrite = opt.targetWriteFn
	}
	for off := 0; off < len(data); off += size {
		if err = waitForTask(ctx, opt); err != nil {
			return true, err
		}
		n := min(size, len(data)-off)
		copy(buf, data[off:off+n])
		padded := roundUp(n, alignment)
		clear(buf[n:padded])
		w, e := pwrite(fd, buf[:padded], int64(off))
		if e != nil {
			if off == 0 && w <= 0 && directUnsupported(e) {
				if err := unix.Ftruncate(fd, 0); err != nil {
					return true, err
				}
				p.IOBackendReason = "目标拒绝直接写入：" + e.Error()
				return false, nil
			}
			return true, e
		}
		if w != padded {
			return true, io.ErrShortWrite
		}
		if opt.Verify {
			_, _ = h.Write(data[off : off+n])
		}
		p.IOBackend = "batch_write_direct"
		if sourceDirect {
			p.IOBackend = "batch_direct"
		}
		p.CopiedBytes += int64(n)
		emit()
	}
	if err = unix.Ftruncate(fd, int64(len(data))); err != nil {
		return true, err
	}
	return true, nil
}
