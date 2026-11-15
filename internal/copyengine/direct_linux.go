//go:build linux

package copyengine

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// directCopy uses O_DIRECT only where STATX_DIOALIGN reports the actual
// filesystem requirements. Before any data is written, unsupported platforms
// fall back to buffered I/O; I/O errors after starting are never retried.
func directCopy(ctx context.Context, en entry, in, out *os.File, opt Options, p *Progress, emit func(), h hash.Hash) (bool, error) {
	src, err := unix.Open(en.src, unix.O_RDONLY|unix.O_DIRECT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if directUnsupported(err) {
			p.IOBackendReason = fmt.Sprintf("来源不支持 O_DIRECT：%v", err)
			return false, nil
		}
		return true, err
	}
	defer unix.Close(src)
	dst, err := unix.Open(out.Name(), unix.O_WRONLY|unix.O_DIRECT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if directUnsupported(err) {
			p.IOBackendReason = fmt.Sprintf("目标不支持 O_DIRECT：%v", err)
			return false, nil
		}
		return true, err
	}
	defer unix.Close(dst)
	var si, di unix.Statx_t
	sourceErr := unix.Statx(src, "", unix.AT_EMPTY_PATH, unix.STATX_DIOALIGN, &si)
	targetErr := unix.Statx(dst, "", unix.AT_EMPTY_PATH, unix.STATX_DIOALIGN, &di)
	var reasons []string
	if reason := directAlignmentReason("来源", si, sourceErr); reason != "" {
		reasons = append(reasons, reason)
	}
	if reason := directAlignmentReason("目标", di, targetErr); reason != "" {
		reasons = append(reasons, reason)
	}
	if len(reasons) > 0 {
		p.IOBackendReason = strings.Join(reasons, "；")
		return false, nil
	}
	memAlign := lcm(int(si.Dio_mem_align), int(di.Dio_mem_align))
	offsetAlign := lcm(int(si.Dio_offset_align), int(di.Dio_offset_align))
	if memAlign <= 0 || memAlign > 1024*1024 || offsetAlign <= 0 || offsetAlign > 1024*1024 {
		p.IOBackendReason = "来源和目标的直接 I/O 对齐要求超出安全范围"
		return false, nil
	}
	var stat unix.Stat_t
	if err = unix.Fstat(src, &stat); err != nil {
		return true, err
	}
	current, err := in.Stat()
	if err != nil {
		return true, err
	}
	if uint64(stat.Ino) != fileInode(current) || uint64(stat.Dev) != fileDevice(current) {
		return true, errors.New("source changed before direct I/O")
	}
	if err = unix.Fstat(dst, &stat); err != nil {
		return true, err
	}
	original, err := out.Stat()
	if err != nil {
		return true, err
	}
	if uint64(stat.Ino) != fileInode(original) || uint64(stat.Dev) != fileDevice(original) {
		return true, errors.New("temporary file changed before direct I/O")
	}
	size := opt.BufferSize
	count := 2
	if p.Strategy == DiskSame {
		size = opt.BatchSize
		count = 1
	}
	size = roundUp(size, offsetAlign)
	buffers, err := opt.scratch.direct.acquire(size, memAlign, count)
	if err != nil {
		return true, err
	}

	p.IOBackend = "direct"
	emit()
	type part struct {
		buf    []byte
		n      int
		offset int64
		err    error
	}
	read := func(b []byte, offset int64) part {
		want := int(min(int64(size), current.Size()-offset))
		n, e := unix.Pread(src, b, offset)
		if e != nil {
			return part{err: e}
		}
		if n != want {
			return part{err: io.ErrUnexpectedEOF}
		}
		return part{buf: b, n: n, offset: offset}
	}
	write := func(x part) error {
		if err := waitForTask(ctx, opt); err != nil {
			return err
		}
		padded := roundUp(x.n, offsetAlign)
		clear(x.buf[x.n:padded])
		n, e := unix.Pwrite(dst, x.buf[:padded], x.offset)
		if e != nil {
			return e
		}
		if n != padded {
			return io.ErrShortWrite
		}
		if opt.Verify {
			_, _ = h.Write(x.buf[:x.n])
		}
		p.CopiedBytes += int64(x.n)
		emit()
		return nil
	}
	asyncHandled := false
	if count == 2 {
		asyncHandled, err = directAIO(ctx, src, dst, size, offsetAlign, current.Size(), buffers, opt, p, emit, h)
		if err != nil {
			return true, err
		}
	}
	if !asyncHandled {
		p.IOBackend = "direct"
		emit()
	}
	if asyncHandled {
		// All requests completed before the mapped buffers are reused.
	} else if count == 1 {
		for offset := int64(0); offset < current.Size(); offset += int64(size) {
			if err = waitForTask(ctx, opt); err != nil {
				return true, err
			}
			x := read(buffers[0], offset)
			if x.err != nil {
				return true, x.err
			}
			if err = write(x); err != nil {
				return true, err
			}
		}
	} else {
		cctx, cancel := context.WithCancel(ctx)
		free := make(chan []byte, count)
		parts := make(chan part, count)
		done := make(chan struct{})
		for _, b := range buffers {
			free <- b
		}
		go func() {
			defer close(done)
			defer close(parts)
			for off := int64(0); off < current.Size(); off += int64(size) {
				var b []byte
				select {
				case <-cctx.Done():
					return
				case b = <-free:
				}
				if waitForTask(cctx, opt) != nil {
					return
				}
				x := read(b, off)
				select {
				case parts <- x:
				case <-cctx.Done():
					return
				}
				if x.err != nil {
					return
				}
			}
		}()
		for x := range parts {
			if x.err != nil {
				err = x.err
				break
			}
			if err = write(x); err != nil {
				break
			}
			select {
			case free <- x.buf:
			case <-cctx.Done():
				err = cctx.Err()
			}
			if err != nil {
				break
			}
		}
		cancel()
		<-done
		if err != nil {
			return true, err
		}
		if err = ctx.Err(); err != nil {
			return true, err
		}
	}
	if err = unix.Ftruncate(dst, current.Size()); err != nil {
		return true, fmt.Errorf("truncate direct output: %w", err)
	}
	return true, nil
}

func directAlignmentReason(side string, st unix.Statx_t, err error) string {
	if err != nil {
		return fmt.Sprintf("%s无法查询直接 I/O 对齐信息：%v", side, err)
	}
	if st.Mask&unix.STATX_DIOALIGN == 0 {
		return side + "文件系统未报告直接 I/O 对齐信息（STATX_DIOALIGN）"
	}
	if st.Dio_mem_align == 0 || st.Dio_offset_align == 0 {
		return side + "文件系统报告不支持直接 I/O"
	}
	return ""
}

func directUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTTY)
}
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
func lcm(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	return a / gcd(a, b) * b
}
func roundUp(n, align int) int { return (n + align - 1) / align * align }
func fileInode(info os.FileInfo) uint64 {
	if s, ok := info.Sys().(*unix.Stat_t); ok {
		return s.Ino
	}
	return fileInodeOS(info)
}
