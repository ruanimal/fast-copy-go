//go:build linux || darwin

package copyengine

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestPreserveExtendedAttributes(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	write(t, filepath.Join(src, "file"), "value")
	if e := os.Mkdir(dst, 0755); e != nil {
		t.Fatal(e)
	}
	if err := unix.Setxattr(filepath.Join(src, "file"), "user.fastcopy-test", []byte("metadata"), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EPERM) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveXattrs: true}, nil); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, err := unix.Getxattr(filepath.Join(dst, "source", "file"), "user.fastcopy-test", buf)
	if err != nil || string(buf[:n]) != "metadata" {
		t.Fatalf("attribute=%q err=%v", buf[:n], err)
	}
}
