//go:build linux || darwin

package copyengine

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSymlinkTimestampAndDanglingTarget(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "link")
	dst := filepath.Join(root, "dst")
	if err := os.Symlink("missing", src); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	ts := unix.NsecToTimespec(time.Unix(1500000000, 123000000).UnixNano())
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, src, []unix.Timespec{ts, ts}, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Skip(err)
	}
	opt := Options{Sources: []string{src}, Destination: dst, Mode: SizeDate}
	if _, err := Run(context.Background(), opt, nil); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Lstat(src)
	b, err := os.Lstat(filepath.Join(dst, "link"))
	if err != nil || !a.ModTime().Equal(b.ModTime()) {
		t.Fatalf("link time: source=%v target=%v err=%v", a.ModTime(), b, err)
	}
	link, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || link != "missing" {
		t.Fatalf("link=%q err=%v", link, err)
	}
	r, err := Run(context.Background(), opt, nil)
	if err != nil || r.Progress.SkippedFiles != 1 {
		t.Fatalf("unchanged symlink not skipped: %+v %v", r.Progress, err)
	}
}
