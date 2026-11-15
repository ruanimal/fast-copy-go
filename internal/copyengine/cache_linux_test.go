//go:build linux

package copyengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func cachedPages(t *testing.T, f *os.File, size int) int {
	t.Helper()
	mapping, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		t.Fatalf("mmap: %v", err)
	}
	defer unix.Munmap(mapping)
	pages := make([]byte, (size+os.Getpagesize()-1)/os.Getpagesize())
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&mapping[0])), uintptr(len(mapping)), uintptr(unsafe.Pointer(&pages[0])))
	if errno != 0 {
		t.Fatalf("mincore: %v", errno)
	}
	count := 0
	for _, page := range pages {
		if page&1 != 0 {
			count++
		}
	}
	return count
}

func TestCommitDropsCleanDestinationCacheWhenSupported(t *testing.T) {
	root := t.TempDir()
	const size = 1 << 20
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	createCached := func(name string) *os.File {
		t.Helper()
		f, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = f.Sync(); err != nil {
			t.Fatal(err)
		}
		return f
	}
	// FADV_DONTNEED is advisory, and tmpfs or some filesystems may ignore it.
	// Check a control file first so a platform limitation is not a failure.
	control := createCached("control")
	before := cachedPages(t, control, size)
	if before == 0 {
		t.Skip("filesystem did not retain control pages")
	}
	if err := unix.Fadvise(int(control.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
		t.Skipf("cache advice unsupported: %v", err)
	}
	if remaining := cachedPages(t, control, size); remaining > before/4 {
		t.Skipf("filesystem did not honor cache advice: %d/%d pages", remaining, before)
	}

	source := createCached("source")
	info, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	staged := createCached("stage")
	before = cachedPages(t, staged, size)
	if before == 0 {
		t.Skip("filesystem did not retain staged pages")
	}
	tempInfo, err := staged.Stat()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	s := &stagedFile{en: entry{dst: target, info: info}, source: source, output: staged, tempInfo: tempInfo}
	if err := commitStaged(context.Background(), s, Options{}); err != nil {
		t.Fatal(err)
	}
	committed, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer committed.Close()
	if remaining := cachedPages(t, committed, size); remaining > before/4 {
		t.Fatalf("destination cache retained %d/%d pages after commit", remaining, before)
	}
	got, err := os.ReadFile(target)
	if err != nil || len(got) != len(data) || string(got) != string(data) {
		t.Fatalf("committed file content differs: len=%d err=%v", len(got), err)
	}
}
