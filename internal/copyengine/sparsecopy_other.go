//go:build !linux && !darwin

package copyengine

import (
	"context"
	"hash"
	"os"
)

func sparseCopy(context.Context, *os.File, *os.File, int64, Options, *Progress, func(), hash.Hash) (bool, error) {
	return false, nil
}
