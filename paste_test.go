package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func setCopiedFiles(t *testing.T, tt *ui.Tester, paths ...string) {
	t.Helper()
	data, err := transfer.FileData(paths...)
	if err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(data)
}

func TestPasteFilesIntoSources(t *testing.T) {
	a := &app{sources: "/existing"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if err := tt.Click("来源路径"); err != nil {
		t.Fatal(err)
	}
	setCopiedFiles(t, tt, "/first folder", "/第二个/file #1")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if want := "/existing\n/first folder\n/第二个/file #1"; a.sources != want {
		t.Fatalf("sources %q; want %q", a.sources, want)
	}
	// A second paste and subsequent redraw must not duplicate or erase paths.
	setCopiedFiles(t, tt, "/last")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if want := "/existing\n/first folder\n/第二个/file #1\n/last"; a.sources != want {
		t.Fatalf("sources %q; want %q", a.sources, want)
	}
}

func TestPasteFileTargetValidationAndStaleResult(t *testing.T) {
	var callbacks []func(error)
	a := &app{destination: "/previous", checkDroppedDirectory: func(_ string, done func(error)) { callbacks = append(callbacks, done) }}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	setCopiedFiles(t, tt, "/first folder")
	tt.Key(ui.Cmd, ui.KeyV)
	if !a.targetDropPending || a.destination != "/previous" || len(callbacks) != 1 {
		t.Fatalf("validation did not start: %+v", a)
	}
	setCopiedFiles(t, tt, "/second")
	tt.Key(ui.Cmd, ui.KeyV)
	if len(callbacks) != 2 {
		t.Fatalf("second validation not started: %d", len(callbacks))
	}
	callbacks[0](nil)
	if a.destination != "/previous" || !a.targetDropPending {
		t.Fatal("stale paste changed target")
	}
	callbacks[1](nil)
	tt.Frame()
	if a.destination != "/second" || a.targetDropPending {
		t.Fatalf("paste target: %q", a.destination)
	}
	setCopiedFiles(t, tt, "/a", "/b")
	tt.Key(ui.Cmd, ui.KeyV)
	if a.destination != "/second" || !strings.Contains(a.status, "一个目标文件夹") {
		t.Fatalf("multiple target paths: %q %q", a.destination, a.status)
	}
	setCopiedFiles(t, tt, "/file")
	tt.Key(ui.Cmd, ui.KeyV)
	callbacks[2](errors.New("请选择文件夹"))
	if a.destination != "/second" || !strings.Contains(a.status, "请选择文件夹") {
		t.Fatalf("invalid target: %q %q", a.destination, a.status)
	}
}

func TestPastePlainTextRetainsNormalEditing(t *testing.T) {
	a := &app{sources: "/old", destination: "/previous"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if err := tt.Click("来源路径"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard("plain source")
	tt.Key(ui.Cmd, ui.KeyV)
	if a.sources != "plain source" {
		t.Fatalf("text source paste: %q", a.sources)
	}
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard("/manual")
	tt.Key(ui.Cmd, ui.KeyV)
	if a.destination != "/manual" {
		t.Fatalf("text target paste: %q", a.destination)
	}
}

func TestPasteRejectsPartialAndInvalidPaths(t *testing.T) {
	a := &app{sources: "/old"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if err := tt.Click("来源路径"); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.URIList, []byte("file:///valid\r\nsmb://server/remote\r\n")))))
	tt.Key(ui.Cmd, ui.KeyV)
	if a.sources != "/old" || !strings.Contains(a.status, "无法识别") {
		t.Fatalf("partial paste: %q %q", a.sources, a.status)
	}
	setCopiedFiles(t, tt, filepath.Join("/", "line\nbreak"))
	tt.Key(ui.Cmd, ui.KeyV)
	if a.sources != "/old" || !strings.Contains(a.status, "换行符") {
		t.Fatalf("newline paste: %q %q", a.sources, a.status)
	}
}

func TestPasteFileIgnoredDuringTask(t *testing.T) {
	a := &app{running: true, sources: "/source", destination: "/target", cancel: func() {}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	setCopiedFiles(t, tt, "/other")
	for _, label := range []string{"来源路径", "目标文件夹路径"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Cmd, ui.KeyV)
	}
	if a.sources != "/source" || a.destination != "/target" {
		t.Fatalf("running paste changed inputs: %q %q", a.sources, a.destination)
	}
}
