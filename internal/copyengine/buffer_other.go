//go:build !linux

package copyengine

type directPool struct{}

func (*directPool) close() {}
