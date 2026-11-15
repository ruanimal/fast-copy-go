//go:build !linux && !darwin

package copyengine

import "os"

func configureCache(in, out *os.File) string { return "buffered" }
func releaseCache(f *os.File)                {}
