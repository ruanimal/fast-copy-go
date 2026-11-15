//go:build linux && (amd64 || arm64)

package copyengine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestNativeAIOCopy(t *testing.T) {
	if unsafe.Sizeof(aioControl{}) != 64 || unsafe.Sizeof(aioEvent{}) != 32 {
		t.Fatal("incorrect Linux AIO ABI layout")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	data := strings.Repeat("async-data", 20000) + "x"
	write(t, src, data)
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskDifferent, BufferSize: 4096, BatchSize: 16384, Verify: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "src")) != data {
		t.Fatal("AIO output differs")
	}
	if r.Progress.IOBackend != "direct_async" {
		t.Skipf("native AIO not available here; backend=%s", r.Progress.IOBackend)
	}
}

func TestNativeAIOCancelWhilePaused(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, src, strings.Repeat("x", 256<<10))
	write(t, filepath.Join(dst, "src"), "original")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := &Gate{}
	paused := make(chan struct{})
	type outcome struct {
		r   Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		stopped := false
		r, err := Run(ctx, Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: DiskDifferent, BufferSize: 4096, BatchSize: 4096, Gate: gate}, func(p Progress) {
			if p.IOBackend == "direct_async" && p.CopiedBytes > 0 && !stopped {
				stopped = true
				gate.Pause()
				close(paused)
			}
		})
		done <- outcome{r, err}
	}()
	select {
	case <-paused:
	case out := <-done:
		if out.err != nil {
			t.Fatal(out.err)
		}
		t.Skipf("native AIO unavailable; backend=%s", out.r.Progress.IOBackend)
	case <-time.After(5 * time.Second):
		t.Fatal("copy did not start")
	}
	cancel()
	select {
	case out := <-done:
		if !errors.Is(out.err, context.Canceled) {
			t.Fatalf("got %v", out.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation stalled")
	}
	if read(t, filepath.Join(dst, "src")) != "original" {
		t.Fatal("canceled AIO replaced target")
	}
}

func TestDirectFallbackWhenNativeAIOUnavailable(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	data := strings.Repeat("fallback", 3000)
	write(t, src, data)
	os.Mkdir(dst, 0755)
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	scratch := &buffers{}
	scratch.aio.tried = true
	defer scratch.direct.close()
	defer scratch.cleanupTemps()
	p := Progress{Strategy: DiskDifferent}
	opt := Options{BufferSize: 4096, BatchSize: 16384, Verify: true, scratch: scratch}
	if err = copyFile(context.Background(), entry{src, filepath.Join(dst, "src"), info}, opt, &p, func() {}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "src")) != data {
		t.Fatal("fallback changed output")
	}
	if p.IOBackend != "direct" {
		t.Skipf("filesystem did not support direct I/O: %s", p.IOBackend)
	}
}
