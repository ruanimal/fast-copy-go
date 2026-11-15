package copyengine

import (
	"errors"
	"os"
	"path/filepath"
)

// tempDirectory reuses a private same-filesystem staging directory per target
// directory; this avoids a mkdir/rmdir pair for every small file.
func (b *buffers) tempDirectory(parent string) (string, error) {
	if b.temps == nil {
		b.temps = map[string]string{}
	}
	if dir := b.temps[parent]; dir != "" {
		return dir, nil
	}
	dir, err := os.MkdirTemp(parent, ".fastcopy-*")
	if err != nil {
		return "", err
	}
	b.temps[parent] = dir
	return dir, nil
}
func (b *buffers) cleanupTemps() error {
	var errs []error
	for parent, dir := range b.temps {
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			continue
		}
		delete(b.temps, parent)
	}
	return errors.Join(errs...)
}
func (b *buffers) tempFile(parent string) (*os.File, error) {
	dir, err := b.tempDirectory(parent)
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "data-*")
}
func tempParent(dst string) string { return filepath.Dir(dst) }
