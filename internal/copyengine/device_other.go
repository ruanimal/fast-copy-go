//go:build !linux

package copyengine

// A volume ID is not a physical-device ID on macOS (for example APFS volumes
// may share a container), so automatic detection remains conservative.
func deviceInfo(string) (device, bool) { return device{}, false }
