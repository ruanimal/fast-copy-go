package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLinuxWindowControlsAndLayout(t *testing.T) {
	minimized, closed := false, false
	maximized := false
	a := &app{}
	actions := windowActions{
		minimize:       func() { minimized = true },
		toggleMaximize: func() { maximized = !maximized },
		close:          func() { closed = true },
		isMaximized:    func() bool { return maximized },
	}
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, actions) }, 680, minimumWindowHeight)
	for _, label := range []string{"Fast Copy", "最小化", "最大化", "关闭窗口", "来源路径", "包含过滤", "任务进度卡片", "取消", "开始复制"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 680 || r.Y+r.H > minimumWindowHeight {
			t.Fatalf("control %q outside window: %+v (found %v)", label, r, ok)
		}
	}
	if err := tt.Click("最小化"); err != nil || !minimized {
		t.Fatalf("minimize: %v, called=%v", err, minimized)
	}
	if err := tt.Click("最大化"); err != nil || !maximized {
		t.Fatalf("maximize: %v, maximized=%v", err, maximized)
	}
	if err := tt.Click("还原窗口"); err != nil || maximized {
		t.Fatalf("restore: %v, maximized=%v", err, maximized)
	}
	if err := tt.Click("关闭窗口"); err != nil || !closed {
		t.Fatalf("close: %v, called=%v", err, closed)
	}
}

func TestLinuxWindowDialogDoesNotOverlapHeader(t *testing.T) {
	a := &app{verify: true, hashMode: "xxHash3"}
	actions := windowActions{minimize: func() {}, toggleMaximize: func() {}, close: func() {}, isMaximized: func() bool { return false }}
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, actions) }, 680, minimumWindowHeight)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("校验文件"); !ok {
		t.Fatal("advanced settings not visible")
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.advanced {
		t.Fatal("dialog did not close")
	}
}
