//go:build !linux || (!amd64 && !arm64)

package copyengine

import (
	"context"
	"hash"
)

type aioPool struct{}

func (*aioPool) close() {}
func directAIO(context.Context, int, int, int, int, int64, [][]byte, Options, *Progress, func(), hash.Hash) (bool, error) {
	return false, nil
}
