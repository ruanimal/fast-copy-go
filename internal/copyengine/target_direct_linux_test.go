//go:build linux

package copyengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInferredExfatAlignment(t *testing.T) {
	for _, tc := range []struct {
		fs         unix.Statfs_t
		page, want int
		ok         bool
	}{
		{unix.Statfs_t{Type: unix.EXFAT_SUPER_MAGIC, Bsize: 131072}, 4096, 131072, true},
		{unix.Statfs_t{Type: unix.EXFAT_SUPER_MAGIC, Bsize: 512}, 4096, 4096, true},
		{unix.Statfs_t{Type: unix.EXFAT_SUPER_MAGIC, Bsize: 0}, 4096, 0, false},
		{unix.Statfs_t{Type: unix.EXFAT_SUPER_MAGIC, Bsize: 2 << 20}, 4096, 0, false},
		{unix.Statfs_t{Type: unix.EXT4_SUPER_MAGIC, Bsize: 4096}, 4096, 0, false},
	} {
		got, ok := inferredExfatAlignment(tc.fs, tc.page)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("fs=%+v page=%d: got %d, %v; want %d, %v", tc.fs, tc.page, got, ok, tc.want, tc.ok)
		}
	}
}

func targetDirectFixture(t *testing.T, root string, size int, mode DiskMode, verify bool, hook func(int, []byte, int64) (int, error)) (Result, error, []byte, string) {
	t.Helper()
	srcDir := t.TempDir()
	source := filepath.Join(srcDir, "source.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte((i*37 + 11) % 251)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "source.bin")
	opt := Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: mode, TargetDirect: true, Verify: verify, Preallocate: true, BufferSize: 4096, BatchSize: 8192, targetWriteFn: hook}
	result, err := Run(context.Background(), opt, nil)
	return result, err, data, out
}

func TestTargetDirectCopiesContentAndTail(t *testing.T) {
	for _, mode := range []DiskMode{DiskSame, DiskDifferent} {
		for _, verify := range []bool{false, true} {
			for _, size := range []int{0, 1, 511, 512, 4095, 4096, 4097, 131071, 131072, 131073} {
				t.Run(fmt.Sprintf("%s/verify=%v/size=%d", mode, verify, size), func(t *testing.T) {
					root := t.TempDir()
					r, err, want, out := targetDirectFixture(t, root, size, mode, verify, nil)
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(out)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("content mismatch: len=%d want=%d error=%v", len(got), len(want), err)
					}
					if r.Progress.IOBackend != "target_direct" {
						t.Skipf("filesystem did not enable direct writes: %s (%s)", r.Progress.IOBackend, r.Progress.IOBackendReason)
					}
				})
			}
		}
	}

}

func TestTargetDirectFirstWriteFailureFallsBack(t *testing.T) {
	root := t.TempDir()
	calls := 0
	r, err, want, out := targetDirectFixture(t, root, 20001, DiskDifferent, false, func(_ int, _ []byte, _ int64) (int, error) { calls++; return -1, unix.EINVAL })
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("fallback content mismatch: %v", err)
	}
	if calls == 0 {
		t.Skip("filesystem did not reach direct write")
	}
	if calls != 1 || r.Progress.IOBackend != "buffered" || !strings.Contains(r.Progress.IOBackendReason, "目标拒绝直接写入") || r.Progress.CopiedBytes != int64(len(want)) {
		t.Fatalf("fallback report: calls=%d progress=%+v", calls, r.Progress)
	}
}

func TestTargetDirectLaterFailurePreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "source.bin")
	if err := os.WriteFile(out, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err, _, _ := targetDirectFixture(t, root, 20001, DiskDifferent, false, func(fd int, buf []byte, off int64) (int, error) {
		calls++
		if calls == 2 {
			return -1, unix.EINVAL
		}
		return unix.Pwrite(fd, buf, off)
	})
	if calls == 0 {
		t.Skip("filesystem did not reach direct write")
	}
	if err == nil || calls != 2 {
		t.Fatalf("later error was ignored: %v (calls=%d)", err, calls)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "existing" {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}
}

// Set FASTCOPY_EXFAT_TEST_ROOT explicitly to run against an actual exFAT volume.
// All writes stay in a new private temporary directory, which is removed afterward.
func TestTargetDirectRealExfat(t *testing.T) {
	parent := os.Getenv("FASTCOPY_EXFAT_TEST_ROOT")
	if parent == "" {
		t.Skip("set FASTCOPY_EXFAT_TEST_ROOT to a writable exFAT directory")
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(parent, &fs); err != nil || uint64(fs.Type) != uint64(unix.EXFAT_SUPER_MAGIC) {
		t.Fatalf("test directory is not exFAT: %v", err)
	}
	root, err := os.MkdirTemp(parent, ".fastcopy-direct-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove test directory: %v", err)
		}
	})
	for _, mode := range []DiskMode{DiskSame, DiskDifferent} {
		for _, verify := range []bool{false, true} {
			for _, size := range []int{1, 4095, 4096, 4097, 131071, 131072, 131073, 2<<20 + 37} {
				t.Run(fmt.Sprintf("%s/verify=%v/size=%d", mode, verify, size), func(t *testing.T) {
					r, err, want, out := targetDirectFixture(t, root, size, mode, verify, nil)
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(out)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("content mismatch: %v", err)
					}
					if r.Progress.IOBackend != "target_direct" {
						t.Fatalf("direct write not used: %s (%s)", r.Progress.IOBackend, r.Progress.IOBackendReason)
					}
				})
			}
		}
	}
	t.Run("target page cache after direct write", func(t *testing.T) {
		checked := false
		r, err, want, out := targetDirectFixture(t, root, 131073, DiskDifferent, false, func(fd int, buf []byte, off int64) (int, error) {
			n, err := unix.Pwrite(fd, buf, off)
			if !checked && err == nil && n > 0 {
				checked = true
				f, e := os.Open(fmt.Sprintf("/proc/self/fd/%d", fd))
				if e != nil {
					t.Fatalf("open staging file for cache check: %v", e)
				}
				if resident := cachedPages(t, f, n); resident != 0 {
					t.Errorf("first direct write populated %d page-cache pages", resident)
				}
				f.Close()
			}
			return n, err
		})
		if err != nil || !checked || r.Progress.IOBackend != "target_direct" {
			t.Fatalf("direct write unavailable: backend=%s checked=%v error=%v", r.Progress.IOBackend, checked, err)
		}
		got, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("direct write content mismatch: %v", err)
		}
	})
	t.Run("cross-file batch", func(t *testing.T) {
		source := t.TempDir()
		for i, size := range []int{1, 4095, 4097} {
			if err := os.WriteFile(filepath.Join(source, fmt.Sprint(i)), bytes.Repeat([]byte{byte(i + 1)}, size), 0600); err != nil {
				t.Fatal(err)
			}
		}
		r, err := Run(context.Background(), Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: DiskSame, TargetDirect: true, Verify: true, BufferSize: 4096, BatchSize: 16384}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if r.Progress.IOBackend != "batch_write_direct" && r.Progress.IOBackend != "batch_direct" {
			t.Fatalf("batch direct write not used: %s (%s)", r.Progress.IOBackend, r.Progress.IOBackendReason)
		}
		for i, size := range []int{1, 4095, 4097} {
			got, err := os.ReadFile(filepath.Join(root, filepath.Base(source), fmt.Sprint(i)))
			if err != nil || !bytes.Equal(got, bytes.Repeat([]byte{byte(i + 1)}, size)) {
				t.Fatalf("batch file %d mismatch: %v", i, err)
			}
		}
	})
}

func TestTargetDirectCancelKeepsExistingFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source.bin")
	data := bytes.Repeat([]byte("data"), 32768)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "source.bin")
	if err := os.WriteFile(target, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	direct := false
	_, err := Run(ctx, Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: DiskDifferent, TargetDirect: true, Verify: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) {
		if p.IOBackend == "target_direct" && p.CopiedBytes > 0 {
			direct = true
			cancel()
		}
	})
	if !direct {
		t.Skip("filesystem did not enable direct writes")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "existing" {
		t.Fatalf("existing target changed: %q, %v", got, err)
	}
}

func TestTargetDirectCrossFileBatch(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	for i, size := range []int{1, 4095, 4097} {
		if err := os.WriteFile(filepath.Join(source, fmt.Sprint(i)), bytes.Repeat([]byte{byte(i + 1)}, size), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Run(context.Background(), Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: DiskSame, TargetDirect: true, Verify: true, BufferSize: 4096, BatchSize: 16384}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, size := range []int{1, 4095, 4097} {
		got, err := os.ReadFile(filepath.Join(root, filepath.Base(source), fmt.Sprint(i)))
		if err != nil || !bytes.Equal(got, bytes.Repeat([]byte{byte(i + 1)}, size)) {
			t.Fatalf("file %d: content mismatch, %v", i, err)
		}
	}
	if result.Progress.IOBackend != "batch_direct" && result.Progress.IOBackend != "batch_write_direct" {
		t.Skipf("filesystem did not enable batch direct writes: %s", result.Progress.IOBackend)
	}
	if result.Progress.BatchFiles != 3 {
		t.Fatalf("expected cross-file batch, got %d", result.Progress.BatchFiles)
	}
}

func TestTargetDirectBatchFailureFallsBack(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(source, fmt.Sprint(i)), bytes.Repeat([]byte{byte(i + 1)}, 4097), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	result, err := Run(context.Background(), Options{Sources: []string{source}, Destination: root, Mode: Overwrite, DiskMode: DiskSame, TargetDirect: true, Verify: true, BufferSize: 4096, BatchSize: 16384, targetWriteFn: func(_ int, _ []byte, _ int64) (int, error) { calls++; return -1, unix.EINVAL }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := os.ReadFile(filepath.Join(root, filepath.Base(source), fmt.Sprint(i)))
		if err != nil || !bytes.Equal(got, bytes.Repeat([]byte{byte(i + 1)}, 4097)) {
			t.Fatalf("file %d mismatch: %v", i, err)
		}
	}
	if calls == 0 {
		t.Skip("filesystem did not reach batch direct write")
	}
	if calls != 3 || result.Progress.IOBackend != "batch_buffered" || !strings.Contains(result.Progress.IOBackendReason, "目标拒绝直接写入") {
		t.Fatalf("batch fallback: calls=%d progress=%+v", calls, result.Progress)
	}
}
