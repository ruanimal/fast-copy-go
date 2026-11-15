//go:build !linux && !darwin

package copyengine

import "os"

type hardlinkID struct {
	dev uint64
	ino uint64
}

func hardlinkIdentity(info os.FileInfo) (hardlinkID, bool) { return hardlinkID{}, false }
