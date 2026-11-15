//go:build linux || darwin

package copyengine

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"hash"
	"io"
	"os"
)

// sparseCopy walks data extents where the source filesystem supports them.
// Unsupported extent queries fall back before any output is written.
func sparseCopy(ctx context.Context, in, out *os.File, size int64, opt Options, p *Progress, emit func(), h hash.Hash) (bool, error) {
	if size == 0 {
		return false, nil
	}
	fd := int(in.Fd())
	_, err := unix.Seek(fd, 0, unix.SEEK_HOLE)
	if err != nil {
		if extentUnsupported(err) {
			_, resetErr := in.Seek(0, io.SeekStart)
			return false, resetErr
		}
		return true, err
	}
	_, err = unix.Seek(fd, 0, unix.SEEK_DATA)
	if err != nil && !errors.Is(err, unix.ENXIO) {
		if extentUnsupported(err) {
			_, resetErr := in.Seek(0, io.SeekStart)
			return false, resetErr
		}
		return true, err
	}
	p.IOBackend = configureCache(in, out)
	if p.IOBackend == "buffered" {
		p.IOBackend = "sparse"
		p.IOBackendReason = "保留稀疏文件空洞的路径使用普通 I/O"
	} else {
		p.IOBackend = "sparse_nocache"
	}
	emit()
	if opt.scratch.parallel[0] == nil {
		opt.scratch.parallel[0] = make([]byte, opt.BufferSize)
	}
	buf := opt.scratch.parallel[0]
	zeros := make([]byte, 64*1024)
	hole := func(length int64) error {
		if !opt.Verify {
			if err := waitForTask(ctx, opt); err != nil {
				return err
			}
			p.CopiedBytes += length
			emit()
			return nil
		}
		for length > 0 {
			if err := waitForTask(ctx, opt); err != nil {
				return err
			}
			n := int(min(length, int64(len(zeros))))
			_, _ = h.Write(zeros[:n])
			p.CopiedBytes += int64(n)
			length -= int64(n)
			emit()
		}
		return nil
	}
	pos := int64(0)
	for pos < size {
		if err = waitForTask(ctx, opt); err != nil {
			return true, err
		}
		data, e := unix.Seek(fd, pos, unix.SEEK_DATA)
		if errors.Is(e, unix.ENXIO) {
			if e = hole(size - pos); e != nil {
				return true, e
			}
			pos = size
			break
		}
		if e != nil {
			return true, e
		}
		if data < pos {
			return true, errors.New("invalid data extent")
		}
		data = min(data, size)
		if data > pos {
			if e = hole(data - pos); e != nil {
				return true, e
			}
			pos = data
		}
		if pos == size {
			break
		}
		end, e := unix.Seek(fd, pos, unix.SEEK_HOLE)
		if e != nil {
			return true, e
		}
		if end <= pos {
			return true, errors.New("invalid hole extent")
		}
		end = min(end, size)
		for pos < end {
			if e = waitForTask(ctx, opt); e != nil {
				return true, e
			}
			want := int(min(int64(len(buf)), end-pos))
			n, re := in.ReadAt(buf[:want], pos)
			if re != nil && !(errors.Is(re, io.EOF) && n == want) {
				return true, re
			}
			if n != want {
				return true, io.ErrUnexpectedEOF
			}
			if e = waitForTask(ctx, opt); e != nil {
				return true, e
			}
			written, we := out.WriteAt(buf[:n], pos)
			if we != nil {
				return true, we
			}
			if written != n {
				return true, io.ErrShortWrite
			}
			if opt.Verify {
				_, _ = h.Write(buf[:n])
			}
			pos += int64(n)
			p.CopiedBytes += int64(n)
			emit()
		}
	}
	if err = out.Truncate(size); err != nil {
		return true, err
	}
	releaseCache(in)
	return true, nil
}
func extentUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP)
}
