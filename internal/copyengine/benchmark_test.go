package copyengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Place TMPDIR on the storage under test; tmpfs results measure memory, not disk.
func BenchmarkCopyStrategies(b *testing.B) {
	const size = 32 << 20
	for _, mode := range []DiskMode{DiskSame, DiskDifferent} {
		b.Run(string(mode), func(b *testing.B) {
			root := b.TempDir()
			src := filepath.Join(root, "source")
			dst := filepath.Join(root, "target")
			if err := os.Mkdir(dst, 0755); err != nil {
				b.Fatal(err)
			}
			f, err := os.Create(src)
			if err != nil {
				b.Fatal(err)
			}
			block := make([]byte, 256<<10)
			for i := range block {
				block[i] = byte(i*31 + 7)
			}
			for i := 0; i < size/len(block); i++ {
				if _, err = f.Write(block); err != nil {
					b.Fatal(err)
				}
			}
			if err = f.Close(); err != nil {
				b.Fatal(err)
			}
			opt := Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: mode, Preallocate: true}
			b.SetBytes(size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Run(context.Background(), opt, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCopySmallFiles(b *testing.B) {
	const count = 500
	const fileSize = 4096
	for _, mode := range []DiskMode{DiskSame, DiskDifferent} {
		b.Run(string(mode), func(b *testing.B) {
			root := b.TempDir()
			src := filepath.Join(root, "source")
			dst := filepath.Join(root, "target")
			if err := os.Mkdir(src, 0755); err != nil {
				b.Fatal(err)
			}
			if err := os.Mkdir(dst, 0755); err != nil {
				b.Fatal(err)
			}
			data := make([]byte, fileSize)
			for i := range data {
				data[i] = byte(i*31 + 7)
			}
			for i := 0; i < count; i++ {
				if err := os.WriteFile(filepath.Join(src, fmt.Sprintf("%04d", i)), data, 0644); err != nil {
					b.Fatal(err)
				}
			}
			opt := Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: mode, Verify: true}
			b.SetBytes(count * fileSize)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Run(context.Background(), opt, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
