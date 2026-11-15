//go:build linux || darwin

package copyengine

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func readAttribute(read func([]byte) (int, error), maxSize int) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		n, err := read(nil)
		if err != nil {
			return nil, err
		}
		if n > maxSize {
			return nil, fmt.Errorf("attribute exceeds %d bytes", maxSize)
		}
		if n == 0 {
			return []byte{}, nil
		}
		b := make([]byte, n)
		n, err = read(b)
		if err == unix.ERANGE {
			continue
		}
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
	return nil, fmt.Errorf("attribute changed during copy")
}
