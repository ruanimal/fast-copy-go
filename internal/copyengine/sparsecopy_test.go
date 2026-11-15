//go:build linux || darwin

package copyengine

import (
	"bytes"
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestSparseCopyPreservesHolesWhenSupported(t *testing.T) {
	for _, allHole := range []bool{false, true} {
		root := t.TempDir()
		src := filepath.Join(root, "sparse")
		dst := filepath.Join(root, "dst")
		f, err := os.Create(src)
		if err != nil {
			t.Fatal(err)
		}
		if allHole {
			err = f.Truncate(8 << 20)
		} else {
			_, err = f.WriteAt([]byte("end"), (8<<20)+123)
		}
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		os.Mkdir(dst, 0755)
		r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Verify: true, Preallocate: true, BufferSize: 4096, BatchSize: 16384}, nil)
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		targetPath := filepath.Join(dst, "sparse")
		target, err := os.ReadFile(targetPath)
		if err != nil || !bytes.Equal(source, target) {
			t.Fatalf("sparse content differs: %v", err)
		}
		if r.Progress.IOBackend != "sparse" && r.Progress.IOBackend != "sparse_nocache" {
			t.Log("extent discovery unavailable; verified fallback")
			continue
		}
		var st unix.Stat_t
		if err = unix.Stat(targetPath, &st); err != nil {
			t.Fatal(err)
		}
		if st.Blocks*512 >= int64(len(target))/2 {
			t.Fatalf("holes were not preserved: blocks=%d size=%d", st.Blocks, len(target))
		}
	}
}
