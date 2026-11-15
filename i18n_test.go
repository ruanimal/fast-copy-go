package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fastcopy-go/internal/copyengine"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func TestLanguageSwitchPersistsAndPreservesChoices(t *testing.T) {
	store := &preferencesStore{path: filepath.Join(t.TempDir(), "preferences.json")}
	a := &app{preferencesStore: store}
	a.applyPreferences(defaultPreferences())
	a.mode = "覆盖"
	a.lastPreferences = a.preferences()
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("界面语言"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.language != "en" {
		t.Fatalf("language = %q", a.language)
	}
	for _, label := range []string{"Language", "Extended attributes", "Verification algorithm", "Close"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 680 || r.Y+r.H > minimumWindowHeight-48 {
			t.Errorf("English control %q outside viewport: %+v (found %v)", label, r, ok)
		}
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"Add files", "Advanced settings…", "Start copy", "Existing file handling"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 680 || r.Y+r.H > minimumWindowHeight-48 {
			t.Errorf("English control %q outside viewport: %+v (found %v)", label, r, ok)
		}
	}
	if err := tt.Click("Existing file handling"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Skip existing files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	got, err := store.load()
	if err != nil || got.Language != "en" || got.Mode != "跳过已有文件" {
		t.Fatalf("saved preferences: %+v, %v", got, err)
	}
	restored := &app{}
	restored.applyPreferences(got)
	if restored.tr(restored.mode) != "Skip existing files" {
		t.Fatalf("restored selection: %+v", restored)
	}
	if err := tt.Click("Advanced settings…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Interface language"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("简体中文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.language != "" || a.mode != "跳过已有文件" {
		t.Fatalf("switch back: language %q, mode %q", a.language, a.mode)
	}
}

func TestLanguageFallbackAndLocalizedMessages(t *testing.T) {
	store := &preferencesStore{path: filepath.Join(t.TempDir(), "preferences.json")}
	for _, tc := range []struct{ json, language, mode string }{
		{`{"mode":"覆盖"}`, "", "覆盖"},
		{`{"language":"unknown","mode":"覆盖"}`, "", "覆盖"},
		{`{"language":"en","mode":"覆盖"}`, "en", "覆盖"},
	} {
		if err := os.WriteFile(store.path, []byte(tc.json), 0600); err != nil {
			t.Fatal(err)
		}
		p, err := store.load()
		if err != nil || p.Language != tc.language || p.Mode != tc.mode {
			t.Fatalf("load %s: %+v, %v", tc.json, p, err)
		}
	}
	a := &app{language: "en"}
	for input, want := range map[string]string{
		"失败：同盘缓冲：请输入 1 到 256 之间的整数":       "Failed: Same-disk buffer: Enter an integer between 1 and 256",
		"任务完成，但有 2 项错误":                   "Task completed with 2 errors",
		"失败：无法使用目标文件夹：/tmp/文件":            "Failed: Cannot use destination folder: /tmp/文件",
		"来源批量读取未启用直接 I/O；目标批量写入未启用直接 I/O": "Direct I/O was not enabled for batched source reads; Direct I/O was not enabled for batched destination writes",
	} {
		if got := a.localizeMessage(input); got != want {
			t.Errorf("localize %q = %q; want %q", input, got, want)
		}
	}
	start := time.Now()
	p := copyengine.Progress{Operation: copyengine.Copy, Started: start, Finished: start.Add(time.Second), CommittedBytes: 1024}
	if got := a.completionStatistics(p); !strings.Contains(got, "Average speed 1.0 KiB/s") || !strings.Contains(got, "Elapsed 1.00 s") {
		t.Fatalf("localized statistics: %q", got)
	}
}
