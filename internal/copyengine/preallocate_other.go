//go:build !linux && !darwin

package copyengine

import "os"

func preallocate(f *os.File, size int64) (bool, error) { return false, nil }
