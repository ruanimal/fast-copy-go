//go:build !linux && !darwin

package copyengine

import "os"

func configureBatchInput(f *os.File) {}
