package copyengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSameDiskBatchReadsFilesBeforeWriting(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(src, name), strings.Repeat(name, 8192))
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	sawBatch := false
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, BufferSize: 4096, BatchSize: 32768, Verify: true}, func(p Progress) {
		if p.BatchFiles >= 3 && !sawBatch {
			sawBatch = true
			for _, name := range []string{"a", "b", "c"} {
				if _, e := os.Lstat(filepath.Join(dst, "src", name)); !os.IsNotExist(e) {
					t.Errorf("file %s committed before batch read finished", name)
				}
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawBatch {
		t.Fatal("files were not read as a cross-file batch")
	}
	if result.Progress.CopiedFiles != 3 || result.Progress.PendingFiles != 0 {
		t.Fatalf("bad progress: %+v", result.Progress)
	}
	for _, name := range []string{"a", "b", "c"} {
		if got := read(t, filepath.Join(dst, "src", name)); got != strings.Repeat(name, 8192) {
			t.Fatalf("bad content: %s", name)
		}
	}
}
func TestVerificationQueueNonStopPreservesFailedTarget(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), strings.Repeat("a", 8192))
	write(t, filepath.Join(src, "b"), strings.Repeat("b", 8192))
	write(t, filepath.Join(dst, "src", "a"), "old target")
	corrupted := false
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Mode: Overwrite, Verify: true, NonStop: true, BufferSize: 4096, BatchSize: 32768}, func(p Progress) {
		if p.Phase == "verifying" && strings.HasSuffix(p.Path, "/a") && !corrupted {
			corrupted = true
			paths, e := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*", "data-*"))
			if e != nil || len(paths) != 1 {
				t.Fatalf("staging files: %v %v", paths, e)
			}
			write(t, paths[0], strings.Repeat("z", 8192))
		}
	})
	if !corrupted || err == nil || result.Progress.ErrorCount != 1 || result.Progress.PendingFiles != 0 || result.Progress.CommittedBytes != 8192 || result.Progress.Finished.IsZero() {
		t.Fatalf("verification error not reported: %+v %v", result.Progress, err)
	}
	if read(t, filepath.Join(dst, "src", "a")) != "old target" {
		t.Fatal("unverified file replaced destination")
	}
	if got := read(t, filepath.Join(dst, "src", "b")); got != strings.Repeat("b", 8192) {
		t.Fatal("subsequent file not copied")
	}
	paths, _ := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*"))
	if len(paths) != 0 {
		t.Fatalf("staging remains: %v", paths)
	}
}
func TestVerificationQueueCanAcceptWorkWhileVerifierPaused(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	os.Mkdir(src, 0755)
	os.Mkdir(dst, 0755)
	scratch := &buffers{}
	defer scratch.direct.close()
	defer scratch.aio.close()
	defer scratch.cleanupTemps()
	opt := Options{BufferSize: 4096, BatchSize: 8192, Mode: Overwrite, Verify: true, scratch: scratch}
	var staged []*stagedFile
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(src, name)
		write(t, path, strings.Repeat(name, 4096))
		info, _ := os.Stat(path)
		p := Progress{Strategy: DiskSame}
		f, err := stageFile(context.Background(), entry{path, filepath.Join(dst, name), info}, opt, &p, func() {}, nil)
		if err != nil {
			t.Fatal(err)
		}
		staged = append(staged, f)
	}
	gate := &Gate{}
	gate.Pause()
	opt.Gate = gate
	q := newVerifyQueue(context.Background(), opt)
	defer q.closeAndDiscard()
	q.jobs <- staged[0]
	q.jobs <- staged[1]
	select {
	case r := <-q.results:
		r.file.cleanup()
		t.Fatal("verification completed while paused")
	case <-time.After(25 * time.Millisecond):
	}
	gate.Resume()
	for range 2 {
		select {
		case r := <-q.results:
			if r.err != nil {
				r.file.cleanup()
				t.Fatal(r.err)
			}
			if _, err := os.Stat(r.file.en.dst); !os.IsNotExist(err) {
				r.file.cleanup()
				t.Fatal("queue committed without main approval")
			}
			if err := commitStaged(context.Background(), r.file, opt); err != nil {
				r.file.cleanup()
				t.Fatal(err)
			}
			r.file.cleanup()
		case <-time.After(3 * time.Second):
			t.Fatal("verifier stalled")
		}
	}
}
func TestVerificationQueueCancelCleansPendingFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(src, name), strings.Repeat(name, 8192))
	}
	os.Mkdir(dst, 0755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := false
	result, err := Run(ctx, Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Verify: true, BufferSize: 4096, BatchSize: 32768}, func(p Progress) {
		if p.PendingFiles > 0 && !stopped {
			stopped = true
			cancel()
		}
	})
	if !stopped || !errors.Is(err, context.Canceled) || result.Progress.PendingFiles != 0 {
		t.Fatalf("cancel: %+v %v", result.Progress, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*"))
	if len(matches) > 0 {
		t.Fatalf("pending files leaked: %v", matches)
	}
}

func TestVerificationQueueFailFastLeavesLaterTargets(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(src, name), strings.Repeat(name, 8192))
		write(t, filepath.Join(dst, "src", name), "old-"+name)
	}
	corrupted := false
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Mode: Overwrite, Verify: true, BufferSize: 4096, BatchSize: 32768}, func(p Progress) {
		if p.Phase == "verifying" && strings.HasSuffix(p.Path, "/a") && !corrupted {
			corrupted = true
			paths, _ := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*", "data-*"))
			if len(paths) != 1 {
				t.Fatalf("staging: %v", paths)
			}
			write(t, paths[0], strings.Repeat("z", 8192))
		}
	})
	if !corrupted || err == nil || result.Progress.PendingFiles != 0 {
		t.Fatalf("expected verify failure: %+v %v", result.Progress, err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if got := read(t, filepath.Join(dst, "src", name)); got != "old-"+name {
			t.Fatalf("%s was committed after failed verification", name)
		}
	}
}
func TestNoOverwriteWhenTargetAppearsDuringQueuedVerification(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, src, strings.Repeat("x", 8192))
	os.Mkdir(dst, 0755)
	target := filepath.Join(dst, "src")
	created := false
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Skip, Verify: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) {
		if p.Phase == "verifying" && !created {
			created = true
			write(t, target, "new external target")
		}
	})
	if err != nil || !created || result.Progress.SkippedFiles != 1 || result.Progress.CopiedFiles != 0 || result.Progress.PendingFiles != 0 || result.Progress.CommittedBytes != 0 {
		t.Fatalf("bad result: %+v %v", result.Progress, err)
	}
	if read(t, target) != "new external target" {
		t.Fatal("queued commit overwrote target")
	}
}

func TestPipelineMultipleBatchesAndQueueBound(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	const count = 100
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%03d", i)
		write(t, filepath.Join(src, name), strings.Repeat(name, 400))
	}
	os.Mkdir(dst, 0755)
	maxPending, maxBatch := 0, 0
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Mode: Overwrite, Verify: true, BufferSize: 4096, BatchSize: 16384}, func(p Progress) { maxPending = max(maxPending, p.PendingFiles); maxBatch = max(maxBatch, p.BatchFiles) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Progress.CopiedFiles != count || result.Progress.PendingFiles != 0 || maxPending > 3 || maxBatch < 2 || maxBatch > maxBatchFiles {
		t.Fatalf("progress %+v max pending %d batch %d", result.Progress, maxPending, maxBatch)
	}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%03d", i)
		if got := read(t, filepath.Join(dst, "src", name)); got != strings.Repeat(name, 400) {
			t.Fatalf("file %s corrupt", name)
		}
	}
}
func TestChangedPrefetchedSourceIsNotCommitted(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(src, name), strings.Repeat(name, 8192))
	}
	os.Mkdir(dst, 0755)
	changed := false
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Verify: true, NonStop: true, BufferSize: 4096, BatchSize: 32768}, func(p Progress) {
		if p.BatchFiles >= 3 && !changed {
			changed = true
			write(t, filepath.Join(src, "b"), "changed after batch")
		}
	})
	if !changed || err == nil || r.Progress.ErrorCount != 1 || r.Progress.CopiedFiles != 2 {
		t.Fatalf("changed source not reported: %+v %v", r.Progress, err)
	}
	if _, e := os.Stat(filepath.Join(dst, "src", "b")); !os.IsNotExist(e) {
		t.Fatal("changed source committed")
	}
	for _, name := range []string{"a", "c"} {
		if read(t, filepath.Join(dst, "src", name)) != strings.Repeat(name, 8192) {
			t.Fatalf("%s not copied correctly", name)
		}
	}
}

func TestBatchHandlesHundredsOfSmallFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	const count = 180
	for i := 0; i < count; i++ {
		write(t, filepath.Join(src, fmt.Sprintf("%03d", i)), "x")
	}
	os.Mkdir(dst, 0755)
	maxBatch := 0
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, BufferSize: 4096, BatchSize: 16384}, func(p Progress) { maxBatch = max(maxBatch, p.BatchFiles) })
	if err != nil {
		t.Fatal(err)
	}
	if r.Progress.CopiedFiles != count || maxBatch != count {
		t.Fatalf("expected one large batch: %+v max batch %d", r.Progress, maxBatch)
	}
}

func TestVerificationQueuePreservesHardlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), "linked")
	if err := os.Link(filepath.Join(src, "a"), filepath.Join(src, "b")); err != nil {
		t.Skip(err)
	}
	os.Mkdir(dst, 0755)
	queued := false
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Verify: true, PreserveHardlinks: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) {
		if p.PendingFiles > 0 {
			queued = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(filepath.Join(dst, "src", "a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(dst, "src", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if !queued || !os.SameFile(a, b) || r.Progress.CopiedFiles != 2 || r.Progress.CommittedBytes != 2*int64(len("linked")) {
		t.Fatalf("queue/hardlinks: %+v queued %v", r.Progress, queued)
	}
}

func TestBatchAndQueueEmptyFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(src, name), "")
	}
	os.Mkdir(dst, 0755)
	sawBatch := false
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, DiskMode: DiskSame, Verify: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) {
		if p.BatchFiles == 3 {
			sawBatch = true
		}
	})
	if err != nil || !sawBatch || r.Progress.CopiedFiles != 3 || r.Progress.TotalBytes != 0 {
		t.Fatalf("empty files: %+v %v", r.Progress, err)
	}
	for _, name := range []string{"a", "b", "c"} {
		info, e := os.Stat(filepath.Join(dst, "src", name))
		if e != nil || info.Size() != 0 {
			t.Fatalf("empty %s: %v %v", name, info, e)
		}
	}
}

func TestRunStagesNextFileWhileVerificationRuns(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), strings.Repeat("a", 8192))
	write(t, filepath.Join(src, "b"), strings.Repeat("b", 8192))
	os.Mkdir(dst, 0755)
	started := make(chan struct{})
	release := make(chan struct{})
	twoPending := make(chan struct{})
	var startOnce, pendingOnce sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opt := Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: DiskSame, Verify: true, BufferSize: 4096, BatchSize: 32768}
	opt.verifyFn = func(ctx context.Context, s *stagedFile, opt Options, buf []byte) error {
		startOnce.Do(func() {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		})
		return verifyStaged(ctx, s, opt, buf)
	}
	type outcome struct {
		r   Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := Run(ctx, opt, func(p Progress) {
			if p.PendingFiles >= 2 {
				pendingOnce.Do(func() { close(twoPending) })
			}
		})
		done <- outcome{r, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		cancel()
		t.Fatal("verifier did not start")
	}
	select {
	case <-twoPending:
	case <-time.After(5 * time.Second):
		close(release)
		cancel()
		t.Fatal("writer did not stage another file while verification was blocked")
	}
	if _, err := os.Stat(filepath.Join(dst, "src", "a")); !os.IsNotExist(err) {
		close(release)
		cancel()
		t.Fatal("unverified target committed")
	}
	close(release)
	select {
	case out := <-done:
		if out.err != nil || out.r.Progress.CopiedFiles != 2 {
			t.Fatalf("pipeline result: %+v %v", out.r.Progress, out.err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("pipeline did not finish")
	}
}

func TestBatchAcrossMultipleSourceFiles(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "one", "a")
	b := filepath.Join(root, "two", "b")
	dst := filepath.Join(root, "dst")
	write(t, a, "first")
	write(t, b, "second")
	os.Mkdir(dst, 0755)
	batch := 0
	r, err := Run(context.Background(), Options{Sources: []string{a, b}, Destination: dst, DiskMode: DiskSame, Verify: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) { batch = max(batch, p.BatchFiles) })
	if err != nil || batch != 2 || r.Progress.CopiedFiles != 2 {
		t.Fatalf("batch: %+v %v", r.Progress, err)
	}
	if read(t, filepath.Join(dst, "a")) != "first" || read(t, filepath.Join(dst, "b")) != "second" {
		t.Fatal("multiple-source batch incorrect")
	}
}
func TestSymlinkWaitsForEarlierVerification(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), strings.Repeat("a", 4096))
	if err := os.Symlink("a", filepath.Join(src, "b")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst, "src", "b"), "existing")
	corrupted := false
	_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, Verify: true, BufferSize: 4096, BatchSize: 8192}, func(p Progress) {
		if p.Phase == "verifying" && !corrupted {
			corrupted = true
			paths, _ := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*", "data-*"))
			if len(paths) != 1 {
				t.Fatalf("staging: %v", paths)
			}
			write(t, paths[0], strings.Repeat("z", 4096))
		}
	})
	if !corrupted || err == nil {
		t.Fatalf("verification failure not reported: %v", err)
	}
	if read(t, filepath.Join(dst, "src", "b")) != "existing" {
		t.Fatal("symlink replaced target before earlier verification completed")
	}
}

func TestChangedVerifiedTemporaryFileIsNotCommitted(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dst")
	write(t, src, strings.Repeat("x", 8192))
	write(t, filepath.Join(dst, "source"), "original")
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	scratch := &buffers{}
	defer scratch.direct.close()
	defer scratch.aio.close()
	defer scratch.cleanupTemps()
	opt := Options{Verify: true, Mode: Overwrite, BufferSize: 4096, BatchSize: 8192, scratch: scratch}
	p := Progress{Strategy: DiskSame}
	staged, err := stageFile(context.Background(), entry{src, filepath.Join(dst, "source"), info}, opt, &p, func() {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.cleanup()
	if err = verifyStaged(context.Background(), staged, opt, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if err = staged.output.Truncate(1); err != nil {
		t.Fatal(err)
	}
	if err = commitStaged(context.Background(), staged, opt); err == nil {
		t.Fatal("changed temporary file was committed")
	}
	if read(t, filepath.Join(dst, "source")) != "original" {
		t.Fatal("old target changed")
	}
}

func TestReplacedTemporaryPathIsNotCommittedOrRemoved(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dst")
	write(t, src, "source-data")
	write(t, filepath.Join(dst, "source"), "original")
	info, _ := os.Stat(src)
	scratch := &buffers{}
	defer scratch.direct.close()
	defer scratch.aio.close()
	defer scratch.cleanupTemps()
	opt := Options{Verify: true, Mode: Overwrite, BufferSize: 4096, BatchSize: 8192, scratch: scratch}
	p := Progress{Strategy: DiskSame}
	staged, err := stageFile(context.Background(), entry{src, filepath.Join(dst, "source"), info}, opt, &p, func() {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := staged.output.Name()
	if err = verifyStaged(context.Background(), staged, opt, make([]byte, 4096)); err != nil {
		staged.cleanup()
		t.Fatal(err)
	}
	moved := name + "-moved"
	if err = os.Rename(name, moved); err != nil {
		staged.cleanup()
		t.Fatal(err)
	}
	write(t, name, "replacement")
	if err = commitStaged(context.Background(), staged, opt); err == nil {
		t.Fatal("replaced temporary path was committed")
	}
	staged.cleanup()
	if read(t, filepath.Join(dst, "source")) != "original" || read(t, name) != "replacement" {
		t.Fatal("target or replacement was modified")
	}
	os.Remove(name)
	os.Remove(moved)
}

func TestCompletedStatisticsCountCommittedFilesOnly(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), "skip this source")
	write(t, filepath.Join(src, "b"), strings.Repeat("b", 8192))
	if err := os.Symlink("b", filepath.Join(src, "c")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst, "src", "a"), "keep existing")
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Skip, Verify: true, BufferSize: 4096, BatchSize: 8192}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := result.Progress
	if p.Operation != Copy || p.CopiedFiles != 2 || p.SkippedFiles != 1 || p.CommittedBytes != 8192 || p.Started.IsZero() || p.Finished.IsZero() || p.Finished.Before(p.Started) {
		t.Fatalf("incorrect completion statistics: %+v", p)
	}
}
