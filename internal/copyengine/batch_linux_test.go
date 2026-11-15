//go:build linux

package copyengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossFileBatchDirectAlignment(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for i, n := range []int{1, 4095, 4096, 4097, 8193} {
		write(t, filepath.Join(src, fmt.Sprint(i)), strings.Repeat(string(rune('a'+i)), n))
	}
	os.Mkdir(dst, 0755)
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, BufferSize: 4096, BatchSize: 32768, Verify: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []int{1, 4095, 4096, 4097, 8193} {
		if got := read(t, filepath.Join(dst, "src", fmt.Sprint(i))); got != strings.Repeat(string(rune('a'+i)), n) {
			t.Fatalf("file %d corrupted", i)
		}
	}
	if r.Progress.IOBackend != "batch_direct" {
		t.Skipf("filesystem did not support batch direct I/O: %s", r.Progress.IOBackend)
	}
}
