package main

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"fastcopy-go/internal/copyengine"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/theme"
)

func TestViewControlsAndClear(t *testing.T) {
	a := &app{sources: "/tmp/example", mode: "覆盖", verify: true, status: "选择来源与目标后开始复制"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 680)
	for _, label := range []string{"添加文件", "添加文件夹", "清空", "浏览…", "高级设置…", "开始复制", "取消", "复制进度"} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing control %q", label)
		}
		if r.Y < 0 || r.Y+r.H > 680 {
			t.Errorf("control %q outside view: %+v", label, r)
		}
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if a.sources != "" {
		t.Fatal("clear did not clear sources")
	}
}

func TestSourceHintAndClearPlacement(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	info, ok := tt.Access("来源路径")
	if !ok || !strings.Contains(info.Placeholder, "每行一个路径") {
		t.Fatalf("source placeholder does not include line guidance: %+v (found %v)", info, ok)
	}
	clear, ok := tt.Find("清空")
	if !ok || clear.Y+clear.H > info.Bounds.Y || clear.X < info.Bounds.X+info.Bounds.W/2 {
		t.Fatalf("clear button not above the right side of the source field: %+v; source %+v", clear, info.Bounds)
	}
	a.running, a.cancel, a.gate = true, func() {}, &copyengine.Gate{}
	tt.Frame()
	if info, ok := tt.Access("清空"); !ok || !info.Disabled {
		t.Fatalf("clear button is not disabled during a task: %+v (found %v)", info, ok)
	}
}

func TestRunningTaskKeepsCancelAccessible(t *testing.T) {
	for _, dark := range []bool{false, true} {
		canceled := false
		a := &app{mode: "覆盖", running: true, cancel: func() { canceled = true }, status: "任务进行中…", progress: copyengine.Progress{Phase: "copying", TotalFiles: 20, CopiedFiles: 5, TotalBytes: 200, CopiedBytes: 50, Path: "/some/path/file", PendingFiles: 2}}
		tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.Palette(dark)); a.view(c) }, 680, minimumWindowHeight-48)
		if _, ok := tt.Find("校验队列 2 项"); !ok {
			t.Fatal("queue status not visible")
		}
		r, ok := tt.Find("取消")
		if !ok || r.Y < 0 || r.Y+r.H > minimumWindowHeight-48 {
			t.Fatalf("dark=%v cancel outside viewport: %+v", dark, r)
		}
		if err := tt.Click("取消"); err != nil {
			t.Fatal(err)
		}
		if !canceled {
			t.Fatal("cancel did not invoke cancellation")
		}
	}
}

func TestQueueAndStrategyShareOneLine(t *testing.T) {
	a := &app{running: true, cancel: func() {}, progress: copyengine.Progress{PendingFiles: 1, Strategy: copyengine.DiskDifferent, IOBackend: "buffered", IOBackendReason: "来源文件系统未报告直接 I/O 对齐信息（STATX_DIOALIGN）"}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	strategyLabel := "并行读写 · " + ioBackendLabel("buffered")
	queue, queueOK := tt.Find("校验队列 1 项")
	strategy, strategyOK := tt.Find(strategyLabel)
	if !queueOK || !strategyOK || queue.Y != strategy.Y || strategy.X <= queue.X+queue.W {
		t.Fatalf("queue and strategy are not on the same line: queue=%+v (%v), strategy=%+v (%v)", queue, queueOK, strategy, strategyOK)
	}
	if info, ok := tt.Access(strategyLabel); !ok || !strings.Contains(info.Description, "取决于两端文件系统和挂载选项") || !strings.Contains(info.Description, "来源文件系统未报告") {
		t.Fatalf("buffered I/O explanation is missing: %+v (found %v)", info, ok)
	}
	a.progress.PendingFiles = 0
	tt = ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	if _, ok := tt.Find(strategyLabel); !ok {
		t.Fatal("strategy missing without a pending queue")
	}
	a.progress.PendingFiles, a.progress.Strategy = 1, ""
	tt = ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	if _, ok := tt.Find("校验队列 1 项"); !ok {
		t.Fatal("queue missing without a strategy")
	}
}

func TestIOSettings(t *testing.T) {
	a := &app{chunkMB: "4", batchMB: "64", toleranceMS: "500"}
	chunk, batch, tolerance, err := a.ioSettings()
	if err != nil || chunk != 4<<20 || batch != 64<<20 || tolerance != 500*time.Millisecond {
		t.Fatalf("settings: %d %d %v %v", chunk, batch, tolerance, err)
	}
	a.batchMB = "1"
	if _, _, _, err = a.ioSettings(); err == nil {
		t.Fatal("accepted batch smaller than I/O chunk")
	}
	a.batchMB = "64"
	a.toleranceMS = "-1"
	if _, _, _, err = a.ioSettings(); err == nil {
		t.Fatal("accepted negative tolerance")
	}
}

func TestPauseButton(t *testing.T) {
	a := &app{mode: "覆盖", running: true, gate: &copyengine.Gate{}, cancel: func() {}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 700)
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if !a.paused {
		t.Fatal("pause did not update state")
	}
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	if a.paused {
		t.Fatal("resume did not update state")
	}
}

func TestCompactLayoutAndAdvancedSettings(t *testing.T) {
	a := &app{mode: "大小或时间不同", diskMode: "自动", hashMode: "xxHash3", operation: "复制", verify: true, preallocate: true}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	for _, label := range []string{"取消", "开始复制", "复制进度"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.X+r.W > 680 || r.Y < 0 || r.Y+r.H > minimumWindowHeight-48 {
			t.Fatalf("control %q outside compact viewport: %+v (found %v)", label, r, ok)
		}
	}
	tt = ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	if !a.advanced {
		t.Fatal("advanced settings did not open")
	}
	for _, label := range []string{"扩展属性", "ACL", "硬链接", "预分配空间", "校验文件", "校验算法", "重读来源", "单次 I/O 大小", "同盘缓冲大小", "时间容差", "优先目标直接写入（Linux）", "关闭"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.X+r.W > 680 || r.Y < 0 || r.Y+r.H > minimumWindowHeight-48 {
			t.Fatalf("control %q outside compact dialog: %+v (found %v)", label, r, ok)
		}
	}
	for _, label := range []string{"应用设置", "取消设置", "文件属性", "过滤规则"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("unexpected control %q", label)
		}
	}
}

func TestCompletionStatisticsRemainVisible(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	p := copyengine.Progress{Operation: copyengine.Copy, Phase: "done", Started: start, Finished: start.Add(2 * time.Second), CommittedBytes: 1 << 20, TotalBytes: 2 << 20, CopiedBytes: 2 << 20, CopiedFiles: 1, SkippedFiles: 1, TotalFiles: 2}
	want := "平均速度 512.0 KiB/s · 总耗时 2.00 秒 · 已复制 1.0 MiB（逻辑大小）"
	if got := completionStatistics(p); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	a := &app{operation: "仅校验", status: "复制完成", progress: p}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 680)
	if r, ok := tt.Find(want); !ok || r.Y < 0 || r.Y+r.H > 680 {
		t.Fatalf("completion speed statistics not visible: %+v", r)
	}
	if got := completionStatistics(p); got != want {
		t.Fatal("completed speed changed after rendering")
	}
	p.Operation = copyengine.VerifyOnly
	if got := completionStatistics(p); got != "" {
		t.Fatalf("verify task showed copy speed: %q", got)
	}
	p.Operation = copyengine.Copy
	p.CommittedBytes = 0
	if got := completionStatistics(p); !strings.Contains(got, "平均速度 0 B/s") {
		t.Fatalf("empty copy speed: %q", got)
	}
}

func TestProcessingSpeed(t *testing.T) {
	for _, tc := range []struct {
		previous, current int64
		elapsed           time.Duration
		want              int64
	}{
		{0, 512, 500 * time.Millisecond, 1024},
		{512, 512, 500 * time.Millisecond, 0},
		{512, 256, time.Second, 0},
		{0, 100, 0, 0},
	} {
		if got := processingSpeed(tc.previous, tc.current, tc.elapsed); got != tc.want {
			t.Errorf("processingSpeed(%d, %d, %v) = %d; want %d", tc.previous, tc.current, tc.elapsed, got, tc.want)
		}
	}
}

func TestLiveSpeedVisibleDuringTaskAndHiddenWhenPaused(t *testing.T) {
	meter := &speedMeter{}
	meter.reset(time.Now())
	a := &app{mode: "覆盖", running: true, cancel: func() {}, gate: &copyengine.Gate{}, speedMeter: meter, speedValid: true, liveSpeed: 1024, progress: copyengine.Progress{Phase: "copying", TotalFiles: 1}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	r, ok := tt.Find("处理速度 1.0 KiB/s")
	if !ok || r.X < 0 || r.X+r.W > 680 || r.Y < 0 || r.Y+r.H > minimumWindowHeight-48 {
		t.Fatalf("live speed not visible in compact viewport: %+v (found %v)", r, ok)
	}
	a.liveSpeed = 0
	tt.Frame()
	if _, ok := tt.Find("处理速度 暂无进度"); !ok {
		t.Fatal("missing progress-waiting message when the sampling window has no completions")
	}
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("处理速度 暂无进度"); ok {
		t.Fatal("live speed remains visible while paused")
	}
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	if a.speedValid {
		t.Fatal("resume retained stale speed")
	}
	if meter.generation.Load() != 3 {
		t.Fatal("pause and resume did not reset speed samples")
	}
}

func TestDestinationHistoryDropdown(t *testing.T) {
	a := &app{destination: "/current", destinationHistory: []string{"/third", "/second", "/first"}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 700)
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	for _, path := range a.destinationHistory {
		if _, ok := tt.Find(path); !ok {
			t.Fatalf("history item %q missing", path)
		}
	}
	if err := tt.Click("/second"); err != nil {
		t.Fatal(err)
	}
	if a.destination != "/second" || strings.Join(a.destinationHistory, ",") != "/third,/second,/first" {
		t.Fatalf("choosing history changed state unexpectedly: destination=%q history=%v", a.destination, a.destinationHistory)
	}
	if _, ok := tt.Find("最近使用的目标文件夹"); ok {
		t.Fatal("history remained open after selection")
	}
	if err := tt.Click("目标文件夹历史"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("/first"); !ok {
		t.Fatal("arrow did not open history")
	}
	if err := tt.Click("目标文件夹历史"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("最近使用的目标文件夹"); ok {
		t.Fatal("arrow did not close history")
	}
	if err := tt.Click("目标文件夹历史"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("/first"); err != nil {
		t.Fatal(err)
	}
	if a.destination != "/first" {
		t.Fatalf("arrow selection: %q", a.destination)
	}
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/custom")
	if a.destination != "/custom" {
		t.Fatalf("editing after a history selection: %q", a.destination)
	}
}

func TestDestinationHistoryAllowsTypingAndKeyboardSelection(t *testing.T) {
	a := &app{destinationHistory: []string{"/home/photos", "/home/videos"}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 700)
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	tt.Type("photos")
	if a.destination != "photos" {
		t.Fatalf("manual input: %q", a.destination)
	}
	if _, ok := tt.Find("/home/photos"); !ok {
		t.Fatal("matching history not visible")
	}
	if _, ok := tt.Find("/home/videos"); ok {
		t.Fatal("nonmatching history still visible")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.destination != "/home/photos" {
		t.Fatalf("keyboard selection: %q", a.destination)
	}
}

func TestDestinationHistoryDisabledWhileRunning(t *testing.T) {
	a := &app{running: true, destinationHistory: []string{"/target"}, cancel: func() {}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 700)
	_ = tt.Click("目标文件夹路径")
	_ = tt.Click("目标文件夹历史")
	if _, ok := tt.Find("/target"); ok {
		t.Fatal("history opened during task")
	}
	if info, ok := tt.Access("目标文件夹路径"); !ok || !info.ReadOnly {
		t.Fatalf("target not read-only: %+v", info)
	}
}

func TestDestinationHistoryEmptyAllowsTyping(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, 700)
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	tt.Type("/new")
	if a.destination != "/new" {
		t.Fatalf("manual input: %q", a.destination)
	}
	if _, ok := tt.Find("最近使用的目标文件夹"); ok {
		t.Fatal("empty history opened")
	}
}

func TestDestinationHistoryLongPathAndDismissal(t *testing.T) {
	path := "/" + strings.Repeat("long-directory/", 100)
	a := &app{destinationHistory: []string{path}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	if err := tt.Click("目标文件夹路径"); err != nil {
		t.Fatal(err)
	}
	panel, ok := tt.Find("最近使用的目标文件夹")
	if !ok || panel.X < 0 || panel.X+panel.W > 680 {
		t.Fatalf("history popup outside window: %+v (found %v)", panel, ok)
	}
	tt.Key(0, ui.KeyEscape)
	if _, ok := tt.Find("最近使用的目标文件夹"); ok {
		t.Fatal("Escape did not close history")
	}
}

func TestAdvancedSettingsTakeEffectImmediately(t *testing.T) {
	a := &app{verify: true, hashMode: "xxHash3", preallocate: true}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("高级设置…")
	click("扩展属性")
	click("校验文件")
	if !a.preserveXattrs || a.verify {
		t.Fatal("settings did not update immediately")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.advanced || !a.preserveXattrs || a.verify {
		t.Fatal("closing dialog discarded settings")
	}
	click("高级设置…")
	click("关闭")
	if a.advanced || !a.preserveXattrs || a.verify {
		t.Fatal("close changed settings")
	}
}

func TestTargetDirectSettingAvailability(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	info, ok := tt.Access("优先目标直接写入（Linux）")
	if !ok || info.Disabled != (runtime.GOOS != "linux") {
		t.Fatalf("incorrect setting availability: %+v (found %v)", info, ok)
	}
	if runtime.GOOS == "linux" {
		if err := tt.Click("优先目标直接写入（Linux）"); err != nil || !a.targetDirect {
			t.Fatalf("setting did not take effect: %v, %v", err, a.targetDirect)
		}
	}
}

func TestMainWindowFiltersAreEditableAndReadOnlyDuringTask(t *testing.T) {
	a := &app{}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	if err := tt.Click("包含过滤"); err != nil {
		t.Fatal(err)
	}
	tt.Type("*.txt;照片/")
	if err := tt.Click("排除过滤"); err != nil {
		t.Fatal(err)
	}
	tt.Type("*.tmp")
	if a.includeFilter != "*.txt;照片/" || a.excludeFilter != "*.tmp" {
		t.Fatalf("filter values: %q, %q", a.includeFilter, a.excludeFilter)
	}
	a.running, a.cancel, a.gate = true, func() {}, &copyengine.Gate{}
	tt.Frame()
	for _, label := range []string{"包含过滤", "排除过滤"} {
		if info, ok := tt.Access(label); !ok || !info.ReadOnly {
			t.Fatalf("%s should be read-only while running: %+v (found %v)", label, info, ok)
		}
	}
}

func TestAdvancedSettingsShowInvalidValues(t *testing.T) {
	a := &app{hashMode: "xxHash3"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for _, label := range []string{"高级设置…", "单次 I/O 大小"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Type("99")
	if a.chunkMB != "99" {
		t.Fatalf("value not updated immediately: %q", a.chunkMB)
	}
	_, _, _, err := a.ioSettings()
	if err == nil {
		t.Fatal("invalid settings accepted")
	}
	if _, ok := tt.Find(err.Error()); !ok {
		t.Fatalf("validation error not visible: %v", err)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.advanced || a.chunkMB != "99" {
		t.Fatal("closing changed invalid value")
	}
	a.start()
	if !strings.HasPrefix(a.status, "失败：") || a.running {
		t.Fatal("task started with invalid value")
	}
}

func TestAdvancedSettingsVerifyOnly(t *testing.T) {
	a := &app{operation: "仅校验", hashMode: "xxHash3"}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("高级设置…"); err != nil {
		t.Fatal(err)
	}
	if info, ok := tt.Access("校验文件"); !ok || !info.Disabled || !info.Checked {
		t.Fatalf("verify-only checkbox: %+v (found %v)", info, ok)
	}
	if info, ok := tt.Access("校验算法"); !ok || info.Disabled {
		t.Fatalf("verify-only algorithm: %+v (found %v)", info, ok)
	}
	if err := tt.Click("校验算法"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("SHA-256"); err != nil {
		t.Fatal(err)
	}
	if a.hashMode != "SHA-256" || a.verify {
		t.Fatalf("verify-only selection changed copy preference: algorithm=%q verify=%v", a.hashMode, a.verify)
	}
}

func TestMainContentDoesNotOverlapProgress(t *testing.T) {
	a := &app{running: true, canceling: true, cancel: func() {}, gate: &copyengine.Gate{}, historyWarning: "历史记录仅在本次运行可用", status: "正在取消…", progress: copyengine.Progress{Phase: "copying", TotalFiles: 20, PendingFiles: 2, ErrorCount: 1, Path: "/some/path", Strategy: copyengine.DiskDifferent, IOBackend: "buffered"}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	settings, ok := tt.Find("包含过滤")
	if !ok {
		t.Fatal("missing strategy control")
	}
	progress, ok := tt.Find("任务进度")
	if !ok || settings.Y+settings.H > progress.Y {
		t.Fatalf("main settings overlap progress: settings=%+v progress=%+v", settings, progress)
	}
}

func TestAdvancedSettingsUnavailableDuringTask(t *testing.T) {
	a := &app{running: true, cancel: func() {}, gate: &copyengine.Gate{}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	_ = tt.Click("高级设置…")
	if a.advanced {
		t.Fatal("settings opened while a task was running")
	}
}

func TestProgressCardFixedAcrossTaskStates(t *testing.T) {
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := copyengine.Progress{Operation: copyengine.Copy, Phase: "copying", Started: started, TotalFiles: 100, CopiedFiles: 10, TotalBytes: 10000, CopiedBytes: 1000, Path: "/some/long/current/path", PendingFiles: 2, ErrorCount: 1, Strategy: copyengine.DiskDifferent, IOBackend: "buffered"}
	completed := base
	completed.Phase = "done"
	completed.Finished = started.Add(10 * time.Second)
	completed.CommittedBytes = 1000
	cases := []struct {
		name    string
		app     app
		visible []string
	}{
		{"idle", app{status: "选择来源与目标后开始复制"}, []string{"选择来源与目标后开始复制"}},
		{"scanning", app{running: true, cancel: func() {}, gate: &copyengine.Gate{}, status: "任务进行中…", progress: copyengine.Progress{Phase: "scanning"}}, nil},
		{"copying", app{running: true, cancel: func() {}, gate: &copyengine.Gate{}, status: "任务进行中…", progress: base}, []string{"校验队列 2 项", "并行读写 · " + ioBackendLabel("buffered"), "错误 1 项", "/some/long/current/path"}},
		{"verifying", app{running: true, cancel: func() {}, gate: &copyengine.Gate{}, status: "任务进行中…", progress: func() copyengine.Progress { p := base; p.Phase = "verifying"; return p }()}, []string{"校验队列 2 项", "/some/long/current/path"}},
		{"paused", app{running: true, paused: true, cancel: func() {}, gate: &copyengine.Gate{}, status: "已暂停", progress: base}, []string{"已暂停", "/some/long/current/path"}},
		{"completed", app{status: "复制完成", progress: completed}, []string{"复制完成", completionStatistics(completed)}},
		{"completed_with_errors", app{status: "任务完成，但有 1 项错误", errorReport: "example", progress: completed}, []string{"任务完成，但有 1 项错误", "错误 1 项", "查看错误", completionStatistics(completed)}},
		{"failed", app{status: "失败：example", errorReport: "example", progress: base}, []string{"失败：example", "错误 1 项", "查看错误"}},
		{"canceled", app{status: "已取消；已完成的文件保留", progress: base}, []string{"已取消；已完成的文件保留", "错误 1 项"}},
	}
	for _, viewport := range [][2]int{{680, minimumWindowHeight - 60}, {680, minimumWindowHeight - 48}, {760, 752}} {
		for _, dark := range []bool{false, true} {
			var reference ui.Rect
			for i := range cases {
				tc := &cases[i]
				t.Run(tc.name, func(t *testing.T) {
					tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.Palette(dark)); tc.app.view(c) }, viewport[0], viewport[1])
					card, ok := tt.Find("任务进度卡片")
					if !ok || card.H <= 0 || card.X < 0 || card.Y < 0 || card.X+card.W > float32(viewport[0]) || card.Y+card.H > float32(viewport[1]) {
						t.Fatalf("card outside viewport: %+v (found %v)", card, ok)
					}
					if i == 0 {
						reference = card
					} else if card != reference {
						t.Fatalf("card moved or resized: %+v; idle %+v", card, reference)
					}
					filter, ok := tt.Find("包含过滤")
					if !ok || filter.Y+filter.H > card.Y {
						t.Fatalf("filter overlaps card: %+v, card=%+v", filter, card)
					}
					cancel, ok := tt.Find("取消")
					if !ok || cancel.Y < card.Y+card.H || cancel.Y+cancel.H > float32(viewport[1]) {
						t.Fatalf("cancel outside footer: %+v, card=%+v", cancel, card)
					}
					for _, label := range tc.visible {
						r, ok := tt.Find(label)
						if !ok || r.Y < card.Y || r.Y+r.H > card.Y+card.H {
							t.Errorf("%q outside progress card: %+v (found %v), card=%+v", label, r, ok, card)
						}
					}
				})
			}
		}
	}
}

func TestProgressCardUsesContentMetricsAndHidesPlaceholders(t *testing.T) {
	var smallHeight float32
	for _, fontSize := range []float32{14, 20} {
		var referenceHeight float32
		for _, withErrors := range []bool{false, true} {
			a := &app{}
			if withErrors {
				a.errorReport = "example"
				a.progress.ErrorCount = 1
			}
			tt := ui.NewTester(func(c *ui.Context) {
				palette := theme.LightPalette()
				palette.FontSize = fontSize
				c.SetTheme(palette)
				a.view(c)
			}, 800, 900)
			card, ok := tt.Find("任务进度卡片")
			if !ok {
				t.Fatal("progress card missing")
			}
			if !withErrors {
				referenceHeight = card.H
				if _, ok := tt.Access("占位"); ok {
					t.Fatal("placeholder exposed to accessibility")
				}
			} else if card.H != referenceHeight {
				t.Fatalf("font size %v: errors changed card height: %v to %v", fontSize, referenceHeight, card.H)
			}
		}
		if fontSize == 14 {
			smallHeight = referenceHeight
		} else if referenceHeight <= smallHeight {
			t.Fatalf("card did not adapt to larger text: %v then %v", smallHeight, referenceHeight)
		}
	}
}

func TestProgressCardHasNoExtraCopyingLine(t *testing.T) {
	a := &app{running: true, cancel: func() {}, gate: &copyengine.Gate{}, status: "任务进行中…", progress: copyengine.Progress{Phase: "copying", TotalFiles: 12, CopiedFiles: 4, TotalBytes: 1000, CopiedBytes: 600, PendingFiles: 2, Strategy: copyengine.DiskDifferent, IOBackend: "buffered", Path: "/source/example.mp4"}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	card, cardOK := tt.Find("任务进度卡片")
	path, pathOK := tt.Find("/source/example.mp4")
	if !cardOK || !pathOK || card.Y+card.H-(path.Y+path.H) > 18 {
		t.Fatalf("extra empty line after current path: card=%+v (%v), path=%+v (%v)", card, cardOK, path, pathOK)
	}
}

func TestCompletedErrorStatusSharesLineAndKeepsDetailsVisible(t *testing.T) {
	a := &app{status: "失败：" + strings.Repeat("较长错误信息", 30), errorReport: "example", progress: copyengine.Progress{ErrorCount: 1}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 680, minimumWindowHeight-48)
	card, cardOK := tt.Find("任务进度卡片")
	status, statusOK := tt.Find(a.status)
	details, detailsOK := tt.Find("查看错误")
	if !cardOK || !statusOK || !detailsOK || status.Y != details.Y || details.X+details.W > card.X+card.W {
		t.Fatalf("error details outside status line: card=%+v (%v), status=%+v (%v), details=%+v (%v)", card, cardOK, status, statusOK, details, detailsOK)
	}
}

func TestExtraWindowHeightExpandsSourceInput(t *testing.T) {
	var previousHeight float32
	for i, height := range []int{minimumWindowHeight - 60, minimumWindowHeight + 40, minimumWindowHeight + 140} {
		a := &app{status: "选择来源与目标后开始复制"}
		tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(theme.LightPalette()); a.view(c) }, 760, height)
		source, sourceOK := tt.Find("来源路径")
		filter, filterOK := tt.Find("包含过滤")
		card, cardOK := tt.Find("任务进度卡片")
		if !sourceOK || !filterOK || !cardOK {
			t.Fatalf("missing layout elements: source=%v filter=%v card=%v", sourceOK, filterOK, cardOK)
		}
		if gap := card.Y - (filter.Y + filter.H); gap < 0 || gap > 20 {
			t.Fatalf("unexpected gap between filters and progress at height %d: %v", height, gap)
		}
		if i > 0 && source.H <= previousHeight {
			t.Fatalf("source input did not grow with window: previous=%v current=%v", previousHeight, source.H)
		}
		previousHeight = source.H
	}
}
