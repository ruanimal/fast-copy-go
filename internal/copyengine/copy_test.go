package copyengine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0640); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestCopyTreeVerifyAndLinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dest")
	write(t, filepath.Join(src, "sub", "file"), strings.Repeat("abc", 1000000))
	if e := os.Mkdir(dst, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("sub/file", filepath.Join(src, "link")); e != nil {
		t.Fatal(e)
	}
	result, e := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Verify: true, BufferSize: 4096}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(dst, "src", "sub", "file")) != read(t, filepath.Join(src, "sub", "file")) {
		t.Fatal("content differs")
	}
	link, e := os.Readlink(filepath.Join(dst, "src", "link"))
	if e != nil || link != "sub/file" {
		t.Fatalf("link=%q err=%v", link, e)
	}
	if result.Progress.CopiedFiles != 2 || result.Progress.Phase != "done" {
		t.Fatalf("bad progress: %+v", result.Progress)
	}
}
func TestPoliciesAndSameFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a", "file")
	dest := filepath.Join(root, "b")
	out := filepath.Join(dest, "file")
	write(t, src, "new")
	write(t, out, "old")
	now := time.Now().Add(-time.Hour)
	os.Chtimes(src, now, now)
	for _, mode := range []Mode{Skip, Update} {
		r, e := Run(context.Background(), Options{Sources: []string{src}, Destination: dest, Mode: mode}, nil)
		if e != nil || r.Progress.SkippedFiles != 1 || read(t, out) != "old" {
			t.Fatalf("%s: %+v %v", mode, r, e)
		}
	}
	_, e := Run(context.Background(), Options{Sources: []string{src}, Destination: dest, Mode: Overwrite}, nil)
	if e != nil || read(t, out) != "new" {
		t.Fatalf("overwrite: %v", e)
	}
	_, e = Run(context.Background(), Options{Sources: []string{src}, Destination: filepath.Dir(src)}, nil)
	if e == nil || read(t, src) != "new" {
		t.Fatal("same file was not rejected")
	}
}
func TestRejectNestedDestinationAndSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	write(t, filepath.Join(src, "dir", "file"), "safe")
	dest := filepath.Join(src, "dest")
	os.Mkdir(dest, 0755)
	if _, e := Run(context.Background(), Options{Sources: []string{src}, Destination: dest}, nil); e == nil {
		t.Fatal("nested destination accepted")
	}
	outside := filepath.Join(root, "outside")
	os.Mkdir(outside, 0755)
	target := filepath.Join(root, "target")
	os.MkdirAll(filepath.Join(target, "src"), 0755)
	os.Symlink(outside, filepath.Join(target, "src", "dir"))
	if _, e := Run(context.Background(), Options{Sources: []string{src}, Destination: target}, nil); e == nil {
		t.Fatal("destination symlink accepted")
	}
	if _, e := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(e) {
		t.Fatal("wrote through symlink")
	}
}
func TestCancelKeepsExistingFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dest")
	write(t, src, strings.Repeat("x", 1<<20))
	write(t, filepath.Join(dst, "src"), "original")
	ctx, cancel := context.WithCancel(context.Background())
	_, e := Run(ctx, Options{Sources: []string{src}, Destination: dst, BufferSize: 4096}, func(p Progress) {
		if p.CopiedBytes > 0 {
			cancel()
		}
	})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("got %v", e)
	}
	if read(t, filepath.Join(dst, "src")) != "original" {
		t.Fatal("existing file changed")
	}
	matches, _ := filepath.Glob(filepath.Join(dst, ".fastcopy-*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestStrategiesAndSizeDate(t *testing.T) {
	for _, strategy := range []DiskMode{DiskSame, DiskDifferent} {
		t.Run(string(strategy), func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "source")
			dst := filepath.Join(root, "dest")
			data := strings.Repeat("long file", 120000)
			write(t, src, data)
			os.Mkdir(dst, 0755)
			result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: SizeDate, DiskMode: strategy, Verify: true, BufferSize: 4096, BatchSize: 16384}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Progress.Strategy != strategy || read(t, filepath.Join(dst, "source")) != data {
				t.Fatalf("bad output or strategy: %+v", result.Progress)
			}
			result, err = Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: SizeDate, DiskMode: strategy, BufferSize: 4096, BatchSize: 16384}, nil)
			if err != nil || result.Progress.SkippedFiles != 1 {
				t.Fatalf("unchanged file not skipped: %+v %v", result.Progress, err)
			}
			write(t, src, "changed size")
			result, err = Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: SizeDate, DiskMode: strategy, BufferSize: 4096, BatchSize: 16384}, nil)
			if err != nil || result.Progress.CopiedFiles != 1 || read(t, filepath.Join(dst, "source")) != "changed size" {
				t.Fatalf("changed file not copied: %+v %v", result.Progress, err)
			}
		})
	}
}

func TestHashAlgorithmsAndSourceRecheck(t *testing.T) {
	for _, hashMode := range []HashMode{HashXXH3, HashSHA256, HashSHA512, HashSHA3_256, HashSHA3_512, HashSHA1, HashMD5} {
		t.Run(string(hashMode), func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "source")
			dst := filepath.Join(root, "dest")
			write(t, src, strings.Repeat("content", 10000))
			if e := os.Mkdir(dst, 0755); e != nil {
				t.Fatal(e)
			}
			_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Verify: true, VerifySourceAgain: true, HashMode: hashMode, DiskMode: DiskDifferent}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if read(t, src) != read(t, filepath.Join(dst, "source")) {
				t.Fatal("content differs")
			}
		})
	}
}

func TestVerifyOnlyDoesNotWriteAndDetectsCorruption(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, src, "original")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst}, nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dst, "src")
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Sources: []string{src}, Destination: dst, Operation: VerifyOnly, HashMode: HashSHA256}
	result, err := Run(context.Background(), opt, nil)
	if err != nil || result.Progress.CopiedFiles != 1 {
		t.Fatalf("verify success: %+v %v", result, err)
	}
	write(t, target, "bad-data")
	os.Chtimes(target, before.ModTime(), before.ModTime())
	_, err = Run(context.Background(), opt, nil)
	if err == nil || read(t, target) != "bad-data" {
		t.Fatalf("corruption not reported, or destination changed: %v", err)
	}
	os.Remove(target)
	_, err = Run(context.Background(), opt, nil)
	if err == nil {
		t.Fatal("missing target accepted")
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("verify created target")
	}
}

func TestPreserveHardlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "one"), "linked")
	if err := os.Link(filepath.Join(src, "one"), filepath.Join(src, "two")); err != nil {
		t.Skip(err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, PreserveHardlinks: true, Verify: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(filepath.Join(dst, "src", "one"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(dst, "src", "two"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("destination hardlinks do not share an inode")
	}
}

func TestNoOverwriteWhenDestinationAppearsDuringCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, src, strings.Repeat("a", 8192))
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dst, "src")
	created := false
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Skip, BufferSize: 4096, BatchSize: 4096, DiskMode: DiskSame}, func(p Progress) {
		if !created && p.CopiedBytes > 0 {
			created = true
			write(t, target, "arrived later")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || read(t, target) != "arrived later" || result.Progress.SkippedFiles != 1 {
		t.Fatalf("racing destination was replaced: %+v", result.Progress)
	}
}

func TestTimestampTolerance(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "destination")
	write(t, src, "same")
	write(t, filepath.Join(dst, "source"), "same")
	base := time.Unix(1700000000, 0)
	os.Chtimes(src, base.Add(time.Second), base.Add(time.Second))
	os.Chtimes(filepath.Join(dst, "source"), base, base)
	r, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Update, TimestampTolerance: 2 * time.Second}, nil)
	if err != nil || r.Progress.SkippedFiles != 1 {
		t.Fatalf("tolerance not applied: %+v %v", r.Progress, err)
	}
	r, err = Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: SizeDate, TimestampTolerance: 2 * time.Second}, nil)
	if err != nil || r.Progress.SkippedFiles != 1 {
		t.Fatalf("size/date tolerance not applied: %+v %v", r.Progress, err)
	}
}

func TestNonStopContinuesAfterError(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), "blocked")
	write(t, filepath.Join(src, "b"), "ok")
	if err := os.MkdirAll(filepath.Join(dst, "src", "a"), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, NonStop: true}, nil)
	if err == nil || result.Progress.ErrorCount != 1 || result.Progress.Phase != "error" {
		t.Fatalf("missing failure report: %+v %v", result.Progress, err)
	}
	if read(t, filepath.Join(dst, "src", "b")) != "ok" {
		t.Fatal("later file was not copied")
	}
	info, e := os.Stat(filepath.Join(dst, "src", "a"))
	if e != nil || !info.IsDir() {
		t.Fatal("conflicting directory was changed")
	}
}

func TestPreallocateKeepsExactFileSize(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	data := strings.Repeat("copy", 300001)
	write(t, src, data)
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Preallocate: true, Verify: true, DiskMode: DiskDifferent}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "src")); got != data {
		t.Fatalf("preallocation changed content or length: got %d want %d", len(got), len(data))
	}
}

func TestPauseResumeAndCancel(t *testing.T) {
	for _, cancelWhilePaused := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelWhilePaused), func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			write(t, src, strings.Repeat("x", 256*1024))
			write(t, filepath.Join(dst, "src"), "existing")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gate := &Gate{}
			paused := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				stopped := false
				_, err := Run(ctx, Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: DiskSame, BufferSize: 4096, BatchSize: 4096, Gate: gate}, func(p Progress) {
					if p.CopiedBytes > 0 && !stopped {
						stopped = true
						gate.Pause()
						close(paused)
					}
				})
				done <- err
			}()
			select {
			case <-paused:
			case <-time.After(3 * time.Second):
				t.Fatal("copy did not reach pause")
			}
			select {
			case err := <-done:
				t.Fatalf("copy completed while paused: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			if read(t, filepath.Join(dst, "src")) != "existing" {
				t.Fatal("destination replaced while paused")
			}
			if cancelWhilePaused {
				cancel()
			} else {
				gate.Resume()
			}
			select {
			case err := <-done:
				if cancelWhilePaused && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel: %v", err)
				}
				if !cancelWhilePaused && err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("paused task did not finish")
			}
			if cancelWhilePaused && read(t, filepath.Join(dst, "src")) != "existing" {
				t.Fatal("cancel changed destination")
			}
		})
	}
}

func TestSourceChangeAfterScanIsRejected(t *testing.T) {
	for _, mode := range []Mode{Overwrite, Skip, SizeDate} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			write(t, src, "old")
			write(t, filepath.Join(dst, "src"), "destination")
			modified := false
			_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: mode}, func(p Progress) {
				if p.Phase == "copying" && !modified {
					modified = true
					write(t, src, "source changed")
				}
			})
			if err == nil || read(t, filepath.Join(dst, "src")) != "destination" {
				t.Fatalf("changed source was accepted or target changed: %v", err)
			}
		})
	}
}

func TestVerificationFailureDoesNotReplaceExistingTarget(t *testing.T) {
	for _, mutateSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("source=%v", mutateSource), func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			original := strings.Repeat("a", 8192)
			write(t, src, original)
			write(t, filepath.Join(dst, "src"), "old target")
			st, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			_, err = Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, Verify: true, VerifySourceAgain: mutateSource, DiskMode: DiskDifferent, BufferSize: 4096, BatchSize: 4096}, func(p Progress) {
				if p.Phase != "verifying" || changed {
					return
				}
				changed = true
				if mutateSource {
					write(t, src, strings.Repeat("b", len(original)))
					if e := os.Chtimes(src, st.ModTime(), st.ModTime()); e != nil {
						t.Fatal(e)
					}
				} else {
					paths, e := filepath.Glob(filepath.Join(dst, ".fastcopy-*", "data-*"))
					if e != nil || len(paths) != 1 {
						t.Fatalf("temporary file not found: %v %v", paths, e)
					}
					write(t, paths[0], strings.Repeat("c", len(original)))
				}
			})
			if !changed || err == nil {
				t.Fatalf("verification did not detect corruption: %v", err)
			}
			if read(t, filepath.Join(dst, "src")) != "old target" {
				t.Fatal("verification failure replaced target")
			}
		})
	}
}

func TestStagingDirectoryReusedAndRemoved(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	write(t, filepath.Join(src, "a"), strings.Repeat("a", 8192))
	write(t, filepath.Join(src, "b"), strings.Repeat("b", 8192))
	os.Mkdir(dst, 0755)
	seen := map[string]bool{}
	_, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, BufferSize: 4096, BatchSize: 4096}, func(p Progress) {
		if p.CopiedBytes > 0 {
			dirs, _ := filepath.Glob(filepath.Join(dst, "src", ".fastcopy-*"))
			for _, dir := range dirs {
				seen[dir] = true
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("expected one shared staging directory, got %v", seen)
	}
	for dir := range seen {
		if _, e := os.Stat(dir); !os.IsNotExist(e) {
			t.Fatalf("staging directory remains: %s", dir)
		}
	}
}

func TestReportsDestinationOnlyAfterValidation(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "destination")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dest)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reported string
	_, err = Run(ctx, Options{Sources: []string{filepath.Join(root, "source")}, Destination: dest}, func(p Progress) {
		if p.Destination != "" {
			reported = p.Destination
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || reported != resolved {
		t.Fatalf("validated destination: %q, %v; want %q and cancellation", reported, err, resolved)
	}
	reported = ""
	_, err = Run(context.Background(), Options{Sources: []string{filepath.Join(root, "source")}, Destination: filepath.Join(root, "missing")}, func(p Progress) {
		if p.Destination != "" {
			reported = p.Destination
		}
	})
	if err == nil || reported != "" {
		t.Fatalf("invalid destination reported: %q, %v", reported, err)
	}
}
