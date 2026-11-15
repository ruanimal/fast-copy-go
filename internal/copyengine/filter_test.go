package copyengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIncludeExcludeFilters(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	write(t, filepath.Join(src, "nested", "chosen", "a.txt"), "keep")
	write(t, filepath.Join(src, "nested", "chosen", "b.tmp"), "drop")
	write(t, filepath.Join(src, "nested", "chosen", "private", "c.txt"), "hidden")
	write(t, filepath.Join(src, "other", "d.txt"), "outside")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	opt := Options{Sources: []string{src}, Destination: dst, IncludePatterns: []string{"chosen/", "*.txt"}, ExcludePatterns: []string{"private/"}}
	r, err := Run(context.Background(), opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Progress.TotalFiles != 1 {
		t.Fatalf("selected %d files", r.Progress.TotalFiles)
	}
	if got := read(t, filepath.Join(dst, "source", "nested", "chosen", "a.txt")); got != "keep" {
		t.Fatal(got)
	}
	for _, p := range []string{"nested/chosen/b.tmp", "nested/chosen/private/c.txt", "other/d.txt"} {
		if _, e := os.Lstat(filepath.Join(dst, "source", p)); !os.IsNotExist(e) {
			t.Fatalf("excluded path exists: %s", p)
		}
	}
}
func TestAnchoredAndInvalidFilters(t *testing.T) {
	f, e := makeFilter([]string{"/source/top/*.txt", "[!a]*.go"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		p    string
		want bool
	}{{"source/top/a.txt", true}, {"source/deep/top/a.txt", false}, {"source/foo.go", true}, {"source/aaa.go", false}} {
		if got := f.includeFile(tc.p); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.p, got, tc.want)
		}
	}
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "dest")
	write(t, src, "data")
	os.Mkdir(dst, 0755)
	if _, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, IncludePatterns: []string{"[broken"}}, nil); err == nil {
		t.Fatal("invalid pattern accepted")
	}
	if _, err := os.Stat(filepath.Join(dst, "source")); !os.IsNotExist(err) {
		t.Fatal("invalid filter modified destination")
	}
}
