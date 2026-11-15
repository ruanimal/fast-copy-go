package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fastcopy-go/internal/copyengine"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func dropOn(t *testing.T, tt *ui.Tester, label string, paths ...string) bool {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q", label)
	}
	return tt.DropFiles(r.X+r.W/2, r.Y+r.H/2, paths...)
}

func TestDropSourcesAppendsPaths(t *testing.T) {
	a := &app{sources: "/existing\n"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if !dropOn(t, tt, "来源路径", "/first folder", "/second/file.txt") {
		t.Fatal("source field did not accept drop")
	}
	if want := "/existing\n/first folder\n/second/file.txt"; a.sources != want {
		t.Fatalf("sources = %q; want %q", a.sources, want)
	}
	if !dropOn(t, tt, "来源路径", "/last") || a.sources != "/existing\n/first folder\n/second/file.txt\n/last" {
		t.Fatalf("subsequent drop: %q", a.sources)
	}
	before := a.sources
	dropOn(t, tt, "来源路径", "/valid", "/bad\nname")
	if a.sources != before || !strings.Contains(a.status, "换行符") {
		t.Fatalf("invalid source modified paths: %q, status %q", a.sources, a.status)
	}
}

func TestDropTargetDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "destination")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{destination: "/previous", checkDroppedDirectory: func(path string, done func(error)) { done(checkDirectory(path)) }}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if !dropOn(t, tt, "目标文件夹路径", dir) || a.destination != dir || a.targetDropPending {
		t.Fatalf("directory drop: destination=%q pending=%v", a.destination, a.targetDropPending)
	}
	dropOn(t, tt, "目标文件夹路径", file)
	if a.destination != dir || !strings.HasPrefix(a.status, "失败：") {
		t.Fatalf("file accepted as target: %q %q", a.destination, a.status)
	}
	dropOn(t, tt, "目标文件夹路径", dir, root)
	if a.destination != dir || !strings.Contains(a.status, "一个目标文件夹") {
		t.Fatalf("multiple targets accepted: %q %q", a.destination, a.status)
	}
	dropOn(t, tt, "目标文件夹路径", dir+"\ninvalid")
	if a.destination != dir || !strings.Contains(a.status, "换行符") {
		t.Fatalf("invalid target accepted: %q %q", a.destination, a.status)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	dropOn(t, tt, "目标文件夹路径", link)
	if a.destination != link {
		t.Fatalf("directory symlink rejected: %q", a.destination)
	}
}

func TestDropsRejectedWhileRunning(t *testing.T) {
	a := &app{running: true, cancel: func() {}, gate: &copyengine.Gate{}, sources: "/source", destination: "/target"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if dropOn(t, tt, "来源路径", "/another") || dropOn(t, tt, "目标文件夹路径", "/other") {
		t.Fatal("drop accepted while running")
	}
	if a.sources != "/source" || a.destination != "/target" {
		t.Fatal("drop modified a running task")
	}
}

func TestTargetDropPendingAndStaleResults(t *testing.T) {
	var callbacks []func(error)
	a := &app{destination: "/previous", checkDroppedDirectory: func(_ string, done func(error)) { callbacks = append(callbacks, done) }}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	dropOn(t, tt, "目标文件夹路径", "/first")
	if !a.targetDropPending || a.destination != "/previous" {
		t.Fatal("target changed before validation")
	}
	if info, ok := tt.Access("开始复制"); !ok || !info.Disabled {
		t.Fatalf("start should be disabled while validating: %+v (found %v)", info, ok)
	}
	a.start()
	if a.running || a.status != checkingTargetStatus {
		t.Fatal("task started while validating target")
	}
	dropOn(t, tt, "目标文件夹路径", "/second")
	callbacks[0](nil)
	if !a.targetDropPending || a.destination != "/previous" {
		t.Fatal("stale validation overrode newer drop")
	}
	callbacks[1](nil)
	if a.targetDropPending || a.destination != "/second" {
		t.Fatal("latest validation did not apply")
	}
	dropOn(t, tt, "目标文件夹路径", "/third")
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	tt.Type("-edited")
	if a.targetDropPending {
		t.Fatal("manual edit did not cancel pending validation")
	}
	before := a.destination
	callbacks[2](nil)
	if a.destination != before {
		t.Fatal("stale validation replaced manual edit")
	}
}

func TestDropThroughWindowAndModal(t *testing.T) {
	a := &app{checkDroppedDirectory: func(_ string, done func(error)) { done(nil) }}
	actions := windowActions{minimize: func() {}, toggleMaximize: func() {}, close: func() {}, isMaximized: func() bool { return false }}
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, actions) }, 760, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if !dropOn(t, tt, "来源路径", "/source") || a.sources != "/source" {
		t.Fatal("source drop did not work with window header")
	}
	if !dropOn(t, tt, "目标文件夹路径", "/destination") || a.destination != "/destination" {
		t.Fatal("target drop did not work with window header")
	}
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	before := a.sources
	dropOn(t, tt, "来源路径", "/hidden")
	if a.sources != before {
		t.Fatal("drop reached source through modal dialog")
	}
}

func TestHistoryChoiceCancelsPendingTargetDrop(t *testing.T) {
	var finish func(error)
	a := &app{destination: "/previous", destinationHistory: []string{"/remembered"}, checkDroppedDirectory: func(_ string, done func(error)) { finish = done }}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 752)
	if !dropOn(t, tt, "目标文件夹路径", "/slow") || !a.targetDropPending {
		t.Fatal("target validation did not start")
	}
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("/remembered"); err != nil {
		t.Fatal(err)
	}
	if a.targetDropPending || a.destination != "/remembered" {
		t.Fatalf("history selection did not cancel validation: %q", a.destination)
	}
	finish(nil)
	if a.destination != "/remembered" {
		t.Fatal("stale drop replaced selected history")
	}
}
