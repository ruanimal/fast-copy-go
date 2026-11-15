//go:build !linux

package copyengine

import (
	"context"
	"hash"
	"os"
)

func directCopy(context.Context, entry, *os.File, *os.File, Options, *Progress, func(), hash.Hash) (bool, error) {
	return false, nil
}

func targetDirectCopy(context.Context, entry, *os.File, *os.File, Options, *Progress, func(), hash.Hash) (bool, error) {
	return false, nil
}
