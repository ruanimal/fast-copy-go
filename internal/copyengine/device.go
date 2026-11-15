package copyengine

type device struct {
	id         string
	rotational bool
}
type deviceResult struct {
	device device
	ok     bool
}
type deviceCache struct{ entries map[uint64]deviceResult }

func (c *deviceCache) lookup(path string) (device, bool) {
	key, ok := deviceKey(path)
	if !ok {
		return device{}, false
	}
	if result, found := c.entries[key]; found {
		return result.device, result.ok
	}
	d, known := deviceInfo(path)
	c.entries[key] = deviceResult{d, known}
	return d, known
}

// chooseStrategy only assumes parallel I/O when physical-device information is
// reliable. Manual modes are useful for virtual, network or composite devices.
func chooseStrategy(src, dest string, requested DiskMode, cache *deviceCache) DiskMode {
	if requested != DiskAuto {
		return requested
	}
	a, okA := cache.lookup(src)
	b, okB := cache.lookup(dest)
	return classifyDevices(a, b, okA, okB)
}
func classifyDevices(a, b device, okA, okB bool) DiskMode {
	if !okA || !okB {
		return DiskSame
	}
	if !a.rotational || !b.rotational || a.id != b.id {
		return DiskDifferent
	}
	return DiskSame
}
