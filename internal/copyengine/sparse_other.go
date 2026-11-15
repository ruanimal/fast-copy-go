//go:build !linux && !darwin

package copyengine

import "os"

func maybeSparse(info os.FileInfo) bool { return true }
