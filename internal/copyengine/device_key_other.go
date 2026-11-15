//go:build !linux && !darwin

package copyengine

func deviceKey(string) (uint64, bool) { return 0, false }
