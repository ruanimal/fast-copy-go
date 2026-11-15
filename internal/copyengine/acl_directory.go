package copyengine

import "os"

func copyDirectoryACL(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return copyACL(in, out)
}
