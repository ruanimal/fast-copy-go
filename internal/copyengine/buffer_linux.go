//go:build linux

package copyengine

import (
	"golang.org/x/sys/unix"
	"unsafe"
)

// directPool owns mmap-backed buffers; the OS mapping satisfies O_DIRECT's
// alignment requirements and is reused across files in a job.
type directPool struct {
	size, align, count int
	maps, bufs         [][]byte
}

func (p *directPool) close() {
	for _, m := range p.maps {
		_ = unix.Munmap(m)
	}
	p.maps = nil
	p.bufs = nil
	p.size = 0
	p.align = 0
	p.count = 0
}
func (p *directPool) acquire(size, align, count int) ([][]byte, error) {
	if p.size == size && p.align == align && p.count == count {
		return p.bufs, nil
	}
	p.close()
	for range count {
		m, err := unix.Mmap(-1, 0, size+align, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
		if err != nil {
			p.close()
			return nil, err
		}
		if err = unix.Madvise(m, unix.MADV_DONTFORK); err != nil {
			_ = unix.Munmap(m)
			p.close()
			return nil, err
		}
		p.maps = append(p.maps, m)
		addr := uintptr(unsafe.Pointer(&m[0]))
		shift := int((uintptr(align) - addr%uintptr(align)) % uintptr(align))
		p.bufs = append(p.bufs, m[shift:shift+size])
	}
	p.size = size
	p.align = align
	p.count = count
	return p.bufs, nil
}
