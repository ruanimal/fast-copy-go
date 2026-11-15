//go:build !linux && !darwin

package copyengine

import "os"

func renameNoReplace(temp, target string) error {
	if err := os.Link(temp, target); err != nil {
		return err
	}
	return os.Remove(temp)
}
