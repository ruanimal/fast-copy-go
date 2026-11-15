//go:build !linux

package copyengine

import (
	"context"
	"hash"
	"os"
)

func batchReadDirect(context.Context, entry, *os.File, []byte, Options) (bool, error) {
	return false, nil
}
func batchWriteDirect(context.Context, *os.File, []byte, bool, Options, *Progress, func(), hash.Hash) (bool, error) {
	return false, nil
}
