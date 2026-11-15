//go:build linux && (amd64 || arm64)

package copyengine

import (
	"context"
	"errors"
	"hash"
	"io"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// These structs match Linux's aio_abi.h on little-endian 64-bit Linux.
// Data buffers are mmap-backed and stay mapped until all requests complete.
type aioControl struct {
	data           uint64
	key, rwFlags   uint32
	opcode         uint16
	priority       int16
	fd             uint32
	buffer, length uint64
	offset         int64
	reserved       uint64
	flags, resfd   uint32
}
type aioEvent struct {
	data, object    uint64
	result, result2 int64
}
type aioPool struct {
	id    uint64
	tried bool
}

func (a *aioPool) start() bool {
	if a.tried {
		return a.id != 0
	}
	a.tried = true
	_, _, e := unix.Syscall(unix.SYS_IO_SETUP, 4, uintptr(unsafe.Pointer(&a.id)), 0)
	return e == 0 && a.id != 0
}

// io_destroy waits for operations that cannot be canceled, so buffers are not
// unmapped while the kernel may still be using them.
func (a *aioPool) close() {
	if a.id != 0 {
		_, _, _ = unix.Syscall(unix.SYS_IO_DESTROY, uintptr(a.id), 0, 0)
		a.id = 0
	}
}

// execute submits one or two independent requests and reaps every submitted
// completion even after a submission error. The caller must not reuse a buffer
// before this returns.
func (a *aioPool) execute(cbs []*aioControl) ([]aioEvent, error) {
	var pins runtime.Pinner
	for _, cb := range cbs {
		pins.Pin(cb)
	}
	defer pins.Unpin()
	submitted := 0
	var submitErr error
	for _, cb := range cbs {
		one := [1]*aioControl{cb}
		n, _, e := unix.Syscall(unix.SYS_IO_SUBMIT, uintptr(a.id), 1, uintptr(unsafe.Pointer(&one[0])))
		runtime.KeepAlive(one)
		if e != 0 {
			submitErr = e
			break
		}
		if n != 1 {
			submitErr = io.ErrShortWrite
			break
		}
		submitted++
	}
	events := make([]aioEvent, submitted)
	received := 0
	for received < submitted {
		n, _, e := unix.Syscall6(unix.SYS_IO_GETEVENTS, uintptr(a.id), 1, uintptr(submitted-received), uintptr(unsafe.Pointer(&events[received])), 0, 0)
		if e == unix.EINTR {
			continue
		}
		if e != 0 {
			a.close()
			return nil, e
		}
		if n == 0 {
			continue
		}
		received += int(n)
	}
	runtime.KeepAlive(cbs)
	return events, submitErr
}
func aioCompletion(events []aioEvent, id uint64, want int) error {
	for _, ev := range events {
		if ev.data != id {
			continue
		}
		if ev.result < 0 {
			return syscall.Errno(-ev.result)
		}
		if ev.result != int64(want) {
			if id == 1 {
				return io.ErrUnexpectedEOF
			}
			return io.ErrShortWrite
		}
		return nil
	}
	return errors.New("missing asynchronous I/O completion")
}
func aioRequest(fd int, opcode uint16, buf []byte, offset int64, id uint64) *aioControl {
	return &aioControl{data: id, opcode: opcode, fd: uint32(fd), buffer: uint64(uintptr(unsafe.Pointer(&buf[0]))), length: uint64(len(buf)), offset: offset}
}

// directAIO overlaps writing the current chunk with reading the next chunk.
// At most two requests and two aligned buffers are in flight.
func directAIO(ctx context.Context, src, dst, size, align int, total int64, bufs [][]byte, opt Options, p *Progress, emit func(), h hash.Hash) (bool, error) {
	a := &opt.scratch.aio
	if !a.start() {
		return false, nil
	}
	p.IOBackend = "direct_async"
	emit()
	if total == 0 {
		return true, nil
	}
	if err := waitForTask(ctx, opt); err != nil {
		return true, err
	}
	firstWant := int(min(int64(size), total))
	first := aioRequest(src, 0, bufs[0], 0, 1)
	events, err := a.execute([]*aioControl{first})
	if err == nil {
		err = aioCompletion(events, 1, firstWant)
	}
	// Unsupported native AIO may safely fall back only before any destination write.
	if err != nil && aioUnsupported(err) {
		a.close()
		return false, nil
	}
	if err != nil {
		return true, err
	}
	current := 0
	offset := int64(0)
	n := firstWant
	for {
		if err = waitForTask(ctx, opt); err != nil {
			return true, err
		}
		data := bufs[current]
		if opt.Verify {
			_, _ = h.Write(data[:n])
		}
		padded := roundUp(n, align)
		clear(data[n:padded])
		write := aioRequest(dst, 1, data[:padded], offset, 2)
		nextOffset := offset + int64(n)
		next := 1 - current
		requests := []*aioControl{write}
		nextWant := 0
		if nextOffset < total {
			nextWant = int(min(int64(size), total-nextOffset))
			requests = append(requests, aioRequest(src, 0, bufs[next], nextOffset, 1))
		}
		events, err = a.execute(requests)
		if err != nil {
			return true, err
		}
		if err = aioCompletion(events, 2, padded); err != nil {
			return true, err
		}
		p.CopiedBytes += int64(n)
		emit()
		if nextWant == 0 {
			break
		}
		if err = aioCompletion(events, 1, nextWant); err != nil {
			return true, err
		}
		current = next
		offset = nextOffset
		n = nextWant
	}
	return true, ctx.Err()
}

func aioUnsupported(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EAGAIN)
}
