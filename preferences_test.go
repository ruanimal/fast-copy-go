package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func TestPreferencesRoundTripAndDefaults(t *testing.T) {
	store := &preferencesStore{path: filepath.Join(t.TempDir(), "config", "preferences.json")}
	if got, err := store.load(); err != nil || got != defaultPreferences() {
		t.Fatalf("missing preferences: %+v, %v", got, err)
	}
	want := preferences{Theme: "dark", Operation: "仅校验", Mode: "覆盖", DiskMode: "不同设备 / SSD", HashMode: "SHA-512",
		Verify: false, VerifySourceAgain: true, PreserveXattrs: true, PreserveACL: true, PreserveHardlinks: true,
		Preallocate: false, TargetDirect: true, ChunkMB: "invalid", BatchMB: "64", ToleranceMS: "500"}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	got, err := (&preferencesStore{path: store.path}).load()
	if err != nil || got != want {
		t.Fatalf("restart: got %+v, %v; want %+v", got, err, want)
	}
	a := &app{}
	a.applyPreferences(got)
	if actual := a.preferences(); actual != want {
		t.Fatalf("application state: got %+v, want %+v", actual, want)
	}
	if _, _, _, err := a.ioSettings(); err == nil {
		t.Fatal("invalid saved I/O value was accepted")
	}
}

func TestPreferencesPartialInvalidAndCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	store := &preferencesStore{path: path}
	if err := os.WriteFile(path, []byte(`{"verify":false,"mode":"unknown","hash_mode":"SHA-256","theme":"unknown"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := store.load()
	want := defaultPreferences()
	want.Verify, want.HashMode = false, "SHA-256"
	if err != nil || got != want {
		t.Fatalf("partial preferences: %+v, %v; want %+v", got, err, want)
	}
	corrupt := []byte(`{"verify":`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = store.load()
	if err == nil || got != defaultPreferences() {
		t.Fatalf("corrupt preferences: %+v, %v", got, err)
	}
	if data, err := os.ReadFile(path); err != nil || !reflect.DeepEqual(data, corrupt) {
		t.Fatalf("corrupt file changed: %q, %v", data, err)
	}
}

func TestPreferencesPersistFromUI(t *testing.T) {
	store := &preferencesStore{path: filepath.Join(t.TempDir(), "preferences.json")}
	a := &app{preferencesStore: store}
	a.applyPreferences(defaultPreferences())
	a.lastPreferences = a.preferences()
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, minimumWindowHeight)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	choose := func(label, item string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(item); err != nil {
			t.Fatal(err)
		}
	}
	choose("已存在文件的处理方式", "覆盖")
	choose("读写策略选择", "不同设备 / SSD")
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"扩展属性", "ACL", "硬链接", "预分配空间", "重读来源"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS == "linux" {
		if err := tt.Click("优先目标直接写入（Linux）"); err != nil {
			t.Fatal(err)
		}
	}
	choose("校验算法", "SHA-256")
	if err := tt.Click("单次 I/O 大小"); err != nil {
		t.Fatal(err)
	}
	tt.Type("4")
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	choose("操作类型", "仅校验")
	tt.Frame()
	got, err := store.load()
	if err != nil || got != a.preferences() {
		t.Fatalf("saved UI settings: %+v, %v; app %+v", got, err, a.preferences())
	}
	if got.Operation != "仅校验" || got.Mode != "覆盖" || got.DiskMode != "不同设备 / SSD" || got.HashMode != "SHA-256" || got.ChunkMB != "4" || !got.PreserveACL || !got.VerifySourceAgain {
		t.Fatalf("UI values missing: %+v", got)
	}
}

func TestPreferencesSaveFailureAndRetry(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{preferencesStore: &preferencesStore{path: filepath.Join(parent, "preferences.json")}}
	a.applyPreferences(defaultPreferences())
	a.lastPreferences = a.preferences()
	a.mode = "覆盖"
	a.persistPreferences()
	if !strings.Contains(a.preferencesWarning, "无法保存设置") {
		t.Fatalf("missing failure warning: %q", a.preferencesWarning)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	a.flushPreferences()
	got, err := a.preferencesStore.load()
	if err != nil || got.Mode != "覆盖" || a.preferencesWarning != "" {
		t.Fatalf("retry: %+v, %v, %q", got, err, a.preferencesWarning)
	}
}

func TestPreferencesLoadErrorDoesNotOverwriteOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	corrupt := []byte(`{"verify":`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	store := &preferencesStore{path: path}
	p, err := store.load()
	if err == nil {
		t.Fatal("expected load error")
	}
	a := &app{preferencesStore: store, preferencesWarning: "无法读取设置：" + err.Error()}
	a.applyPreferences(p)
	a.lastPreferences = a.preferences()
	a.flushPreferences()
	if data, err := os.ReadFile(path); err != nil || !reflect.DeepEqual(data, corrupt) {
		t.Fatalf("closing overwrote corrupt preferences: %q, %v", data, err)
	}
}
