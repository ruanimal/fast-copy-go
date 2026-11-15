//go:build linux

package copyengine

import (
	"context"
	"fmt"
	"hash"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// targetDirectAlignment prefers kernel-provided limits. Linux exFAT does not
// currently report STATX_DIOALIGN on some kernels; its filesystem block size,
// combined with the page size, supplies a conservative alignment for a trial.
func targetDirectAlignment(fd int) (mem, offset int, inferred bool, reason string) {
	var sx unix.Statx_t
	err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_DIOALIGN, &sx)
	if err == nil && sx.Mask&unix.STATX_DIOALIGN != 0 {
		if sx.Dio_mem_align == 0 || sx.Dio_offset_align == 0 {
			return 0, 0, false, "目标文件系统报告不支持直接 I/O"
		}
		mem, offset = int(sx.Dio_mem_align), int(sx.Dio_offset_align)
	} else {
		if err != nil && !directUnsupported(err) {
			return 0, 0, false, fmt.Sprintf("目标直接 I/O 对齐查询失败：%v", err)
		}
		var fs unix.Statfs_t
		if e := unix.Fstatfs(fd, &fs); e != nil || uint64(fs.Type) != uint64(unix.EXFAT_SUPER_MAGIC) {
			return 0, 0, false, "目标未报告直接 I/O 对齐信息"
		}
		align, ok := inferredExfatAlignment(fs, os.Getpagesize())
		if !ok {
			return 0, 0, false, "exFAT 文件系统块大小超出安全范围"
		}
		mem, offset, inferred = align, align, true
	}
	if mem <= 0 || offset <= 0 || mem > 1024*1024 || offset > 1024*1024 {
		return 0, 0, false, "目标直接 I/O 对齐要求超出安全范围"
	}
	return mem, offset, inferred, ""
}

func inferredExfatAlignment(fs unix.Statfs_t, pageSize int) (int, bool) {
	if uint64(fs.Type) != uint64(unix.EXFAT_SUPER_MAGIC) || fs.Bsize <= 0 || fs.Bsize > 1024*1024 || pageSize <= 0 || pageSize > 1024*1024 {
		return 0, false
	}
	align := lcm(int(fs.Bsize), pageSize)
	return align, align > 0 && align <= 1024*1024
}

// targetDirectCopy reads the source normally into aligned, bounded buffers;
// only the target requires O_DIRECT. A rejected first write can safely fall
// back because the staging file is private and source reads are positional.
func targetDirectCopy(ctx context.Context, en entry, in, out *os.File, opt Options, p *Progress, emit func(), h hash.Hash) (bool, error) {
	fd, err := unix.Open(out.Name(), unix.O_WRONLY|unix.O_DIRECT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if directUnsupported(err) {
			p.IOBackendReason = fmt.Sprintf("目标不支持 O_DIRECT：%v", err)
			return false, nil
		}
		return true, err
	}
	defer unix.Close(fd)
	mem, alignment, inferred, reason := targetDirectAlignment(fd)
	if reason != "" {
		p.IOBackendReason = reason
		return false, nil
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return true, err
	}
	original, err := out.Stat()
	if err != nil {
		return true, err
	}
	if uint64(st.Ino) != fileInode(original) || uint64(st.Dev) != fileDevice(original) {
		return true, fmt.Errorf("temporary file changed before direct I/O")
	}
	blockSize, count := opt.BufferSize, 2
	if p.Strategy == DiskSame {
		blockSize, count = opt.BatchSize, 1
	}
	blockSize = roundUp(blockSize, alignment)
	buffers, err := opt.scratch.direct.acquire(blockSize, mem, count)
	if err != nil {
		return true, err
	}
	defer releaseCache(in)
	_ = unix.Fadvise(int(in.Fd()), 0, 0, unix.FADV_SEQUENTIAL)
	if inferred {
		p.IOBackendReason = "目标未报告直接 I/O 对齐信息，已按 exFAT 文件系统块和内存页大小保守对齐"
	} else {
		p.IOBackendReason = "来源使用普通读取，目标已请求直接写入"
	}
	type part struct {
		buf    []byte
		n      int
		offset int64
		err    error
	}
	read := func(buf []byte, offset int64) part {
		want := int(min(int64(blockSize), en.info.Size()-offset))
		n, err := in.ReadAt(buf[:want], offset)
		if err != nil && !(err == io.EOF && n == want) {
			return part{err: err}
		}
		if n != want {
			return part{err: io.ErrUnexpectedEOF}
		}
		return part{buf: buf, n: n, offset: offset}
	}
	written := int64(0)
	pwrite := unix.Pwrite
	if opt.targetWriteFn != nil {
		pwrite = opt.targetWriteFn
	}
	write := func(x part) (bool, error) {
		if err := waitForTask(ctx, opt); err != nil {
			return true, err
		}
		padded := roundUp(x.n, alignment)
		clear(x.buf[x.n:padded])
		n, err := pwrite(fd, x.buf[:padded], x.offset)
		if err != nil {
			if written == 0 && n <= 0 && directUnsupported(err) {
				if e := unix.Ftruncate(fd, 0); e != nil {
					return true, e
				}
				p.IOBackendReason = fmt.Sprintf("目标拒绝直接写入，已回退普通 I/O：%v", err)
				return false, nil
			}
			return true, err
		}
		if n != padded {
			return true, io.ErrShortWrite
		}
		if opt.Verify {
			_, _ = h.Write(x.buf[:x.n])
		}
		written += int64(x.n)
		p.IOBackend = "target_direct"
		p.CopiedBytes += int64(x.n)
		emit()
		return true, nil
	}
	if count == 1 {
		for off := int64(0); off < en.info.Size(); off += int64(blockSize) {
			if err = waitForTask(ctx, opt); err != nil {
				return true, err
			}
			x := read(buffers[0], off)
			if x.err != nil {
				return true, x.err
			}
			handled, err := write(x)
			if !handled || err != nil {
				return handled, err
			}
		}
	} else {
		child, cancel := context.WithCancel(ctx)
		free := make(chan []byte, count)
		parts := make(chan part, count)
		done := make(chan struct{})
		for _, buf := range buffers {
			free <- buf
		}
		go func() {
			defer close(done)
			defer close(parts)
			for off := int64(0); off < en.info.Size(); off += int64(blockSize) {
				var buf []byte
				select {
				case <-child.Done():
					return
				case buf = <-free:
				}
				if waitForTask(child, opt) != nil {
					return
				}
				x := read(buf, off)
				select {
				case parts <- x:
				case <-child.Done():
					return
				}
				if x.err != nil {
					return
				}
			}
		}()
		handled := true
		for x := range parts {
			if x.err != nil {
				err = x.err
				break
			}
			handled, err = write(x)
			if !handled || err != nil {
				break
			}
			select {
			case free <- x.buf:
			case <-child.Done():
				err = child.Err()
			}
			if err != nil {
				break
			}
		}
		cancel()
		<-done
		if !handled || err != nil {
			return handled, err
		}
		if err = ctx.Err(); err != nil {
			return true, err
		}
	}
	if err = unix.Ftruncate(fd, en.info.Size()); err != nil {
		return true, fmt.Errorf("truncate direct output: %w", err)
	}
	if en.info.Size() == 0 {
		p.IOBackend = "target_direct"
		emit()
	}
	return true, nil
}
