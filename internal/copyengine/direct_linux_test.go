//go:build linux

package copyengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDirectCopyAlignmentAndTail(t *testing.T) {
	direct := false
	for _, mode := range []DiskMode{DiskSame, DiskDifferent} {
		for _, size := range []int{0, 1, 511, 4095, 4096, 4097, 16385, 32769} {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			data := strings.Repeat("a", size)
			write(t, src, data)
			if err := os.Mkdir(dst, 0755); err != nil {
				t.Fatal(err)
			}
			result, err := Run(context.Background(), Options{Sources: []string{src}, Destination: dst, Mode: Overwrite, DiskMode: mode, Verify: true, BufferSize: 4096, BatchSize: 16384}, nil)
			if err != nil {
				t.Fatalf("mode %s size %d: %v", mode, size, err)
			}
			if got := read(t, filepath.Join(dst, "src")); got != data {
				t.Fatalf("mode %s size %d output length %d", mode, size, len(got))
			}
			if result.Progress.IOBackend == "direct" {
				direct = true
			}
		}
	}
	if !direct {
		t.Skip("filesystem did not provide direct I/O alignment; buffered fallback verified")
	}
	fmt.Println("tested direct I/O on current filesystem")
}

func TestDirectAlignmentReason(t *testing.T) {
	cases := []struct {
		name, side, want string
		stat             unix.Statx_t
		err              error
	}{
		{name: "query error", side: "来源", err: unix.EIO, want: "来源无法查询"},
		{name: "not reported", side: "来源", want: "来源文件系统未报告直接 I/O 对齐信息"},
		{name: "explicit unsupported", side: "目标", stat: unix.Statx_t{Mask: unix.STATX_DIOALIGN}, want: "目标文件系统报告不支持直接 I/O"},
		{name: "reported", side: "目标", stat: unix.Statx_t{Mask: unix.STATX_DIOALIGN, Dio_mem_align: 4096, Dio_offset_align: 512}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := directAlignmentReason(tc.side, tc.stat, tc.err)
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}
