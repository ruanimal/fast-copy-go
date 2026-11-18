package main

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThemeSelectionAndSystemAppearance(t *testing.T) {
	store := &preferencesStore{path: filepath.Join(t.TempDir(), "preferences.json")}
	a := &app{preferencesStore: store}
	a.applyPreferences(defaultPreferences())
	a.lastPreferences = a.preferences()
	var dark bool
	tt := ui.NewTester(func(c *ui.Context) {
		a.windowView(c, nil)
		dark = c.Theme().Dark
	}, 680, minimumWindowHeight)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetDark(true)
	tt.Frame()
	if !dark {
		t.Fatal("automatic theme did not follow dark system appearance")
	}
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		label, mode string
		wantDark    bool
	}{{"亮色", "light", false}, {"暗色", "dark", true}, {"自动", "", true}} {
		if err := tt.Click("界面主题"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(tc.label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.themeMode != tc.mode || dark != tc.wantDark {
			t.Fatalf("choose %s: mode=%q dark=%v", tc.label, a.themeMode, dark)
		}
		got, err := store.load()
		if err != nil || got.Theme != tc.mode {
			t.Fatalf("saved theme: %+v, %v", got, err)
		}
	}
	tt.SetDark(false)
	tt.Frame()
	if dark {
		t.Fatal("automatic theme did not follow light system appearance")
	}
}

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
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, &actions) }, 680, minimumWindowHeight)
	for _, label := range []string{"Fast Copy", "最小化", "最大化", "关闭窗口", "来源路径", "包含过滤", "任务进度卡片", "取消", "开始复制"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 680 || r.Y+r.H > minimumWindowHeight {
			t.Fatalf("control %q outside window: %+v (found %v)", label, r, ok)
		}
	}
	title, _ := tt.Find(windowTitle)
	if center := float64(title.X + title.W/2); math.Abs(center-340) > 1 {
		t.Fatalf("title center = %v, want 340", center)
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
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, &actions) }, 680, minimumWindowHeight)
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

func TestMacWindowTitleLayout(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(func(c *ui.Context) { a.windowView(c, nil) }, 680, minimumWindowHeight)
	tt.SetTitleBar(ui.TitleBar{Height: 56, Left: 80})
	title, ok := tt.Find(windowTitle)
	if !ok || math.Abs(float64(title.X+title.W/2)-340) > 1 {
		t.Fatalf("title not centered: %+v (found %v)", title, ok)
	}
	if source, ok := tt.Find("来源路径"); !ok || source.Y < 56 {
		t.Fatalf("content overlaps title bar: %+v (found %v)", source, ok)
	}
	if _, ok := tt.Find("最小化"); ok {
		t.Fatal("macOS should use native window controls")
	}
}
