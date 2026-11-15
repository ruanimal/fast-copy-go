//go:build linux

package copyengine

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This optional real-device comparison reads an existing source and writes only
// inside a newly created private temporary directory on the target volume.
func TestTargetDirectSMBPerformance(t *testing.T) {
	sourceDir, targetDir := os.Getenv("FASTCOPY_SMB_BENCH_DIR"), os.Getenv("FASTCOPY_EXFAT_TEST_ROOT")
	if sourceDir == "" || targetDir == "" {
		t.Skip("set FASTCOPY_SMB_BENCH_DIR and FASTCOPY_EXFAT_TEST_ROOT")
	}
	var srcFS, dstFS unix.Statfs_t
	if err := unix.Statfs(sourceDir, &srcFS); err != nil || (uint32(srcFS.Type) != uint32(unix.CIFS_SUPER_MAGIC) && uint32(srcFS.Type) != uint32(unix.SMB2_SUPER_MAGIC)) {
		t.Fatalf("source is not SMB: %v", err)
	}
	if err := unix.Statfs(targetDir, &dstFS); err != nil || uint64(dstFS.Type) != uint64(unix.EXFAT_SUPER_MAGIC) {
		t.Fatalf("target is not exFAT: %v", err)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	var source string
	var size int64
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() < 16<<20 || info.Size() > 32<<20 || maybeSparse(info) {
			continue
		}
		if source == "" || info.Size() < size {
			source, size = filepath.Join(sourceDir, e.Name()), info.Size()
		}
	}
	if source == "" {
		t.Fatal("no suitable 16–32 MiB non-sparse regular file in source directory")
	}
	root, err := os.MkdirTemp(targetDir, ".fastcopy-benchmark-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove benchmark directory: %v", err)
		}
	})
	hash := func(path string) [32]byte {
		t.Helper()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		releaseCache(f)
		var sum [32]byte
		copy(sum[:], h.Sum(nil))
		return sum
	}
	want := hash(source)
	t.Logf("source size: %.2f MiB; verification disabled; two trials per mode", float64(size)/(1<<20))
	for _, direct := range []bool{false, true, true, false} {
		start := time.Now()
		r, err := Run(context.Background(), Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: DiskDifferent, Preallocate: true, TargetDirect: direct}, nil)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if direct && r.Progress.IOBackend != "target_direct" {
			t.Fatalf("direct mode fell back: %s (%s)", r.Progress.IOBackend, r.Progress.IOBackendReason)
		}
		if got := hash(filepath.Join(root, filepath.Base(source))); got != want {
			t.Fatal("copied content hash differs")
		}
		t.Logf("mode=%s elapsed=%s speed=%.2f MiB/s backend=%s", map[bool]string{false: "ordinary", true: "target-direct"}[direct], elapsed.Round(time.Millisecond), float64(size)/(1<<20)/elapsed.Seconds(), r.Progress.IOBackend)
	}
}
