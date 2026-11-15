package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"fastcopy-go/internal/copyengine"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

//go:embed assets/icon.png
var appIcon []byte

const minimumWindowHeight = 740

type app struct {
	win                                                                                                                                                          *mygo.Window
	sources, destination, mode, diskMode, hashMode, operation, status, chunkMB, batchMB, toleranceMS, includeFilter, excludeFilter, errorReport, language        string
	verify, verifySourceAgain, preserveXattrs, preserveACL, preserveHardlinks, preallocate, targetDirect, nonStop, advanced, paused, canceling, running, closing bool
	gate                                                                                                                                                         *copyengine.Gate
	cancel                                                                                                                                                       context.CancelFunc
	progress                                                                                                                                                     copyengine.Progress
	liveSpeed                                                                                                                                                    int64
	speedValid                                                                                                                                                   bool
	speedMeter                                                                                                                                                   *speedMeter
	historyStore                                                                                                                                                 *destinationHistoryStore
	preferencesStore                                                                                                                                             *preferencesStore
	lastPreferences                                                                                                                                              preferences
	preferencesWarning                                                                                                                                           string
	preferencesSaveFailed                                                                                                                                        bool
	sourcePaste, destinationPaste                                                                                                                                *filePaste
	destinationHistory                                                                                                                                           []string
	historyWarning                                                                                                                                               string
	targetDropID                                                                                                                                                 uint64
	targetDropPending                                                                                                                                            bool
	checkDroppedDirectory                                                                                                                                        func(string, func(error))
}

func (a *app) pick(directory, destination bool) {
	if destination {
		a.cancelTargetDrop()
	}
	title := a.tr("选择路径")
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Directory: directory, Multiple: !destination, Title: title})
		mygo.RunOnMain(func() {
			if err != nil {
				a.status = err.Error()
			} else if len(paths) > 0 && !a.running {
				if destination {
					if strings.Contains(paths[0], "\n") {
						a.status = "无法添加包含换行符的路径"
					} else {
						a.destination = paths[0]
					}
				} else {
					a.appendSources(paths)
				}
			}
			a.win.Invalidate()
		})
	}()
}
func (a *app) start() {
	if a.targetDropPending {
		a.status = checkingTargetStatus
		return
	}
	var paths []string
	for _, line := range strings.Split(a.sources, "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	mode := copyengine.SizeDate
	switch a.mode {
	case "覆盖":
		mode = copyengine.Overwrite
	case "跳过已有文件":
		mode = copyengine.Skip
	case "仅覆盖较旧文件":
		mode = copyengine.Update
	}
	diskMode := copyengine.DiskAuto
	switch a.diskMode {
	case "同一硬盘":
		diskMode = copyengine.DiskSame
	case "不同设备 / SSD":
		diskMode = copyengine.DiskDifferent
	}
	hashMode := map[string]copyengine.HashMode{"xxHash3": copyengine.HashXXH3, "SHA-256": copyengine.HashSHA256, "SHA-512": copyengine.HashSHA512, "SHA3-256": copyengine.HashSHA3_256, "SHA3-512": copyengine.HashSHA3_512, "SHA-1": copyengine.HashSHA1, "MD5": copyengine.HashMD5}[a.hashMode]
	operation := copyengine.Copy
	if a.operation == "仅校验" {
		operation = copyengine.VerifyOnly
	}
	chunk, batch, tolerance, err := a.ioSettings()
	if err != nil {
		a.status = "失败：" + err.Error()
		return
	}
	opt := copyengine.Options{Sources: paths, Destination: a.destination, Mode: mode, Verify: a.verify, DiskMode: diskMode, HashMode: hashMode, VerifySourceAgain: a.verifySourceAgain, PreserveXattrs: a.preserveXattrs, PreserveACL: a.preserveACL, PreserveHardlinks: a.preserveHardlinks, Preallocate: a.preallocate, TargetDirect: a.targetDirect, Operation: operation, NonStop: a.nonStop, BufferSize: chunk, BatchSize: batch, TimestampTolerance: tolerance, IncludePatterns: strings.Split(a.includeFilter, ";"), ExcludePatterns: strings.Split(a.excludeFilter, ";")}
	a.gate = &copyengine.Gate{}
	a.paused = false
	a.canceling = false
	opt.Gate = a.gate
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.running = true
	a.status = "任务进行中…"
	a.progress = copyengine.Progress{}
	a.liveSpeed = 0
	a.speedValid = false
	a.errorReport = ""
	store := a.historyStore
	historySnapshot := append([]string(nil), a.destinationHistory...)
	gate := a.gate
	meter := &speedMeter{}
	meter.reset(time.Now())
	a.speedMeter = meter
	speedDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-speedDone:
				return
			case <-ticker.C:
				speed, generation := meter.sample(time.Now())
				mygo.RunOnMain(func() {
					if a.running && !a.paused && a.gate == gate && meter.generation.Load() == generation {
						a.liveSpeed, a.speedValid = speed, true
						a.win.Invalidate()
					}
				})
			}
		}
	}()
	go func() {
		last := time.Time{}
		recorded := false
		result, err := copyengine.Run(ctx, opt, func(p copyengine.Progress) {
			meter.processed.Store(p.CopiedBytes)
			if !recorded && p.Destination != "" {
				recorded = true
				history := recentDestination(historySnapshot, p.Destination)
				warning := ""
				if store == nil {
					warning = "历史记录仅在本次运行可用"
				} else if saved, e := store.remember(p.Destination); e != nil {
					warning = "无法保存历史记录：" + e.Error()
				} else {
					history = saved
				}
				mygo.RunOnMain(func() { a.destinationHistory = history; a.historyWarning = warning; a.win.Invalidate() })
			}
			if p.Phase != "done" && time.Since(last) < 100*time.Millisecond {
				return
			}
			last = time.Now()
			mygo.RunOnMain(func() { a.progress = p; a.win.Invalidate() })
		})
		close(speedDone)
		mygo.RunOnMain(func() {
			a.progress = result.Progress
			a.running = false
			a.speedMeter = nil
			a.cancel = nil
			a.gate = nil
			a.paused = false
			a.canceling = false
			if a.closing {
				a.win.Close()
				return
			}
			switch {
			case errors.Is(err, context.Canceled):
				a.status = "已取消；已完成的文件保留"
			case err != nil:
				a.errorReport = err.Error()
				if result.Progress.ErrorCount > 0 {
					a.status = fmt.Sprintf("任务完成，但有 %d 项错误", result.Progress.ErrorCount)
				} else {
					a.status = "失败：" + err.Error()
				}
			default:
				if opt.Operation == copyengine.VerifyOnly {
					a.status = "校验完成"
				} else {
					a.status = "复制完成"
				}
			}
			a.win.Invalidate()
		})
	}()
}
func (a *app) ioSettings() (int, int, time.Duration, error) {
	return parseIOSettings(a.chunkMB, a.batchMB, a.toleranceMS)
}

func parseIOSettings(chunkMB, batchMB, toleranceMS string) (int, int, time.Duration, error) {
	parse := func(value string, fallback, min, max int) (int, error) {
		if strings.TrimSpace(value) == "" {
			return fallback, nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < min || n > max {
			return 0, fmt.Errorf("请输入 %d 到 %d 之间的整数", min, max)
		}
		return n, nil
	}
	chunk, err := parse(chunkMB, 2, 1, 16)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("单次 I/O 大小：%w", err)
	}
	batch, err := parse(batchMB, 32, 1, 256)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("同盘缓冲：%w", err)
	}
	if batch < chunk {
		return 0, 0, 0, errors.New("同盘缓冲不能小于单次 I/O 大小")
	}
	ms, err := parse(toleranceMS, 0, 0, 86400000)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("时间容差：%w", err)
	}
	return chunk * 1024 * 1024, batch * 1024 * 1024, time.Duration(ms) * time.Millisecond, nil
}

func bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// completionStatistics uses committed logical bytes and the whole task duration,
// including scanning, verification, and pauses.
func completionStatistics(p copyengine.Progress) string {
	return (&app{}).completionStatistics(p)
}

func (a *app) completionStatistics(p copyengine.Progress) string {
	if p.Operation != copyengine.Copy || p.Started.IsZero() || p.Finished.IsZero() {
		return ""
	}
	elapsed := p.Finished.Sub(p.Started)
	if elapsed <= 0 {
		return ""
	}
	speed := float64(p.CommittedBytes) / elapsed.Seconds()
	speedBytes := int64(math.MaxInt64)
	if speed < float64(math.MaxInt64) {
		speedBytes = int64(speed)
	}
	duration := fmt.Sprintf(a.tr("%.2f 秒"), elapsed.Seconds())
	if elapsed < time.Second {
		duration = fmt.Sprintf(a.tr("%.1f 毫秒"), float64(elapsed)/float64(time.Millisecond))
	} else if elapsed >= time.Minute {
		seconds := int64(elapsed.Round(time.Second) / time.Second)
		duration = fmt.Sprintf(a.tr("%d 分 %02d 秒"), seconds/60, seconds%60)
	}
	return fmt.Sprintf(a.tr("平均速度 %s/s · 总耗时 %s · 已复制 %s（逻辑大小）"), bytes(speedBytes), duration, bytes(p.CommittedBytes))
}

// Empty progress lines keep the same text metrics without exposing placeholder
// content to the user or assistive technologies.
func progressLinePlaceholder(c *ui.Context) {
	components.Text(c, "占位").FontSize(12).SingleLine().Invisible()
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	verifyOnly := a.operation == "仅校验"
	p := a.progress
	value := float64(0)
	if p.TotalBytes > 0 {
		value = float64(p.CopiedBytes+p.SkippedBytes) / float64(p.TotalBytes)
	}
	if p.Phase == "done" {
		value = 1
	}
	if p.Phase == "scanning" {
		value = -1
	}
	phase := map[string]string{"scanning": "扫描中", "copying": "复制中", "verifying": "校验中", "done": "已完成", "error": "有错误"}[p.Phase]
	if !a.running && p.Phase != "done" && p.Phase != "error" {
		phase = "待开始"
	}
	if strings.HasPrefix(a.status, "失败：") {
		phase = "未完成"
	}
	if strings.HasPrefix(a.status, "已取消") {
		phase = "已取消"
	}
	if a.running && a.paused {
		phase = "已暂停"
	}
	if a.running && a.canceling {
		phase = "正在取消"
	}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(8, 28, 6).Gap(6).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Box(c).Size(36, 36).Radius(12).Background(t.Accent.Alpha(.12)).Center().Children(func() { components.Icon(c, lucide.Copy).Size(20, 20).TextColor(t.Accent) })
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						components.Text(c, a.tr("文件复制")).FontSize(22).Bold()
						components.Text(c, a.tr("选择来源和目标，开始一个复制任务")).FontSize(13).TextColor(t.TextMuted)
					})
					if a.preferencesWarning != "" {
						components.Text(c, a.localizeMessage(a.preferencesWarning)).FontSize(12).SingleLine().MinWidth(0).Tooltip(a.localizeMessage(a.preferencesWarning)).TextColor(t.Danger)
					}
					components.Checkbox(c, &a.nonStop, a.tr("遇错继续")).Disabled(a.running)
					if components.Button(c, a.tr("高级设置…"), components.ButtonSecondary).Disabled(a.running).Clicked() {
						a.advanced = true
					}
				})
				ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(12).Gap(6).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						components.Icon(c, lucide.Files).Size(17, 17).TextColor(t.Accent)
						components.Text(c, a.tr("来源")).Bold()
						ui.Spacer(c)
						if components.Button(c, a.tr("清空"), components.ButtonGhost).Padding(0, 8).Disabled(a.running).Clicked() {
							a.sources = ""
						}
						if components.Button(c, a.tr("添加文件"), components.ButtonSecondary).Disabled(a.running).Clicked() {
							a.pick(false, false)
						}
						if components.Button(c, a.tr("添加文件夹"), components.ButtonSecondary).Disabled(a.running).Clicked() {
							a.pick(true, false)
						}
					})
					sourceField := components.TextBox(c, &a.sources, components.TextBoxOptions{Multiline: true, ReadOnly: a.running}).Grow(1).MinHeight(56).Placeholder(a.tr("选择、拖入或粘贴文件、文件夹，也可以输入路径（每行一个路径）")).Label(a.tr("来源路径"))
					if !a.running {
						svc := c.Services()
						sourceField.OnPaste(func(text string) bool { return queueFilePaste(svc, &a.sourcePaste, text) })
						if pending := a.sourcePaste; pending != nil {
							a.sourcePaste = nil
							if pending.err != nil {
								a.status = "失败：" + pending.err.Error()
							} else {
								a.appendSources(pending.paths)
							}
						}
					}
					if !a.running {
						if paths := sourceField.DroppedFiles(); len(paths) > 0 {
							a.appendSources(paths)
						}
						if sourceField.FileDragOver() {
							sourceField.Border(2, t.Accent)
						}
					}
					ui.Box(c).FillWidth().Height(1).Background(t.Border)
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						components.Icon(c, lucide.FolderOpen).Size(17, 17).TextColor(t.Accent)
						components.Text(c, a.tr("目标文件夹")).Bold()
						if a.historyWarning != "" {
							components.Text(c, a.localizeMessage(a.historyWarning)).FontSize(12).SingleLine().MinWidth(0).Tooltip(a.localizeMessage(a.historyWarning)).TextColor(t.TextMuted)
						}
						ui.Spacer(c)
						if components.Button(c, a.tr("浏览…"), components.ButtonSecondary).Disabled(a.running).Clicked() {
							a.pick(true, true)
						}
					})
					ui.Row(c).FillWidth().Gap(8).Children(func() {
						a.destinationInput(c)
					})
				})
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					ui.Column(c).Width(a.operationWidth()).Gap(5).Children(func() {
						components.Text(c, a.tr("操作")).FontSize(12).TextColor(t.TextMuted)
						components.NativeSelect(c, &a.operation, []components.NativeSelectOption[string]{{Value: "复制", Label: a.tr("复制")}, {Value: "仅校验", Label: a.tr("仅校验")}}).Trigger.FillWidth().Disabled(a.running).Label(a.tr("操作类型"))
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
						components.Text(c, a.tr("已存在的文件")).FontSize(12).TextColor(t.TextMuted)
						components.NativeSelect(c, &a.mode, []components.NativeSelectOption[string]{{Value: "大小或时间不同", Label: a.tr("大小或时间不同")}, {Value: "覆盖", Label: a.tr("覆盖")}, {Value: "跳过已有文件", Label: a.tr("跳过已有文件")}, {Value: "仅覆盖较旧文件", Label: a.tr("仅覆盖较旧文件")}}).Trigger.FillWidth().Disabled(a.running || verifyOnly).Label(a.tr("已存在文件的处理方式"))
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
						components.Text(c, a.tr("读写策略")).FontSize(12).TextColor(t.TextMuted)
						components.NativeSelect(c, &a.diskMode, []components.NativeSelectOption[string]{{Value: "自动", Label: a.tr("自动")}, {Value: "同一硬盘", Label: a.tr("同一硬盘")}, {Value: "不同设备 / SSD", Label: a.tr("不同设备 / SSD")}}).Trigger.FillWidth().Disabled(a.running || verifyOnly).Label(a.tr("读写策略选择"))
					})
				})
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					components.Text(c, a.tr("包含")).FontSize(12).Tooltip(a.tr("多个过滤规则用分号分隔")).TextColor(t.TextMuted)
					components.TextBox(c, &a.includeFilter, components.TextBoxOptions{ReadOnly: a.running}).Grow(1).MinWidth(0).Placeholder(a.tr("例如 *.txt;照片/")).Label(a.tr("包含过滤"))
					components.Text(c, a.tr("排除")).FontSize(12).Tooltip(a.tr("多个过滤规则用分号分隔")).TextColor(t.TextMuted)
					components.TextBox(c, &a.excludeFilter, components.TextBoxOptions{ReadOnly: a.running}).Grow(1).MinWidth(0).Placeholder(a.tr("例如 *.tmp;缓存/")).Label(a.tr("排除过滤"))
				})
			})
		})
		// Progress and cancellation stay visible beneath the task settings.
		ui.Column(c).FillWidth().Padding(8, 28, 20).Gap(10).Children(func() {
			ui.Column(c).FillWidth().Shrink(0).Label(a.tr("任务进度卡片")).Padding(12, 18).Gap(5).Radius(12).Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					components.Text(c, a.tr("任务进度")).Bold()
					ui.Spacer(c)
					if a.running && !a.paused && a.speedValid && p.TotalFiles > 0 {
						label := "处理速度 " + bytes(a.liveSpeed) + "/s"
						if a.liveSpeed == 0 {
							label = "处理速度 暂无进度"
						}
						components.Text(c, a.localizeMessage(label)).FontSize(12).Tooltip(a.tr("每半秒更新，按最近约 3 秒内已完成的逻辑处理量计算；暂无进度可能表示读写或同步尚未完成，不表示磁盘空闲。稀疏文件空洞和保留的硬链接也会计入，不代表物理磁盘吞吐量。")).TextColor(t.TextMuted)
						ui.Box(c).Width(12)
					}
					components.Text(c, a.tr(phase)).FontSize(12).TextColor(t.TextMuted)
				})
				components.Progress(c, value).Label(a.tr("复制进度"))
				progressLines := 0
				if p.TotalFiles > 0 {
					stats := fmt.Sprintf(a.tr("%s / %s  ·  完成 %d / %d 个文件  ·  跳过 %d"), bytes(p.CopiedBytes+p.SkippedBytes), bytes(p.TotalBytes), p.CopiedFiles, p.TotalFiles, p.SkippedFiles)
					components.Text(c, stats).FontSize(12).SingleLine().TextColor(t.TextMuted)
					progressLines++
				}
				stats := ""
				if !a.running {
					stats = a.completionStatistics(p)
				}
				if stats != "" {
					components.Text(c, stats).FontSize(12).SingleLine().Tooltip(a.tr("按成功复制文件的逻辑大小计算；耗时包含扫描、校验和暂停时间。稀疏文件及硬链接的逻辑大小可能大于实际写入量。 ") + stats).TextColor(t.TextMuted)
					progressLines++
				}
				hasDetails := p.PendingFiles > 0 || (p.Strategy != "" && a.running) || p.ErrorCount > 0 || a.errorReport != ""
				showStatus := a.status != "" && a.status != "任务进行中…" && !(a.running && (a.paused || a.canceling))
				inlineStatus := !a.running && showStatus && hasDetails
				if hasDetails {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						if p.PendingFiles > 0 {
							components.Text(c, fmt.Sprintf(a.tr("校验队列 %d 项"), p.PendingFiles)).FontSize(12).SingleLine().TextColor(t.TextMuted)
						}
						if p.Strategy != "" && a.running {
							if p.PendingFiles > 0 {
								components.Text(c, "·").FontSize(12).TextColor(t.TextMuted)
							}
							strategy := a.tr(map[copyengine.DiskMode]string{copyengine.DiskSame: "同盘批量", copyengine.DiskDifferent: "并行读写"}[p.Strategy])
							backend := a.tr(ioBackendLabel(p.IOBackend))
							detail := strategy + " · " + backend
							if strings.HasPrefix(p.IOBackend, "batch_") && p.BatchFiles > 1 {
								detail += fmt.Sprintf(a.tr(" · %d 个文件"), p.BatchFiles)
							}
							tip := detail + " · " + a.tr(ioBackendExplanation(p.IOBackend))
							if p.IOBackendReason != "" {
								label := " 回退原因："
								if p.IOBackend == "target_direct" || p.IOBackend == "batch_write_direct" {
									label = " 说明："
								}
								tip += a.tr(label) + a.localizeMessage(p.IOBackendReason)
							}
							components.Text(c, detail).FontSize(12).SingleLine().MinWidth(0).Tooltip(tip).TextColor(t.TextMuted)
						}
						if inlineStatus {
							color := t.TextMuted
							if strings.HasPrefix(a.status, "失败：") {
								color = t.Danger
							}
							components.Text(c, a.localizeMessage(a.status)).FontSize(12).SingleLine().Tooltip(a.localizeMessage(a.status)).TextColor(color).Grow(1).MinWidth(0)
						}
						if p.ErrorCount > 0 {
							components.Text(c, fmt.Sprintf(a.tr("错误 %d 项"), p.ErrorCount)).FontSize(12).TextColor(t.Danger)
						}
						if a.errorReport != "" && components.Button(c, a.tr("查看错误"), components.ButtonLink).FontSize(12).Clicked() {
							detail := a.localizeMessage(a.errorReport)
							title, message := a.tr("任务错误"), a.tr("以下项目未能完成")
							go func() {
								_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageError, Title: title, Message: message, Detail: detail})
							}()
						}
					})
					progressLines++
				}
				if showStatus && !inlineStatus {
					color := t.TextMuted
					if strings.HasPrefix(a.status, "失败：") {
						color = t.Danger
					}
					components.Text(c, a.localizeMessage(a.status)).FontSize(12).SingleLine().Tooltip(a.localizeMessage(a.status)).TextColor(color)
					progressLines++
				} else if p.Path != "" && a.running {
					components.Text(c, p.Path).FontSize(12).SingleLine().Tooltip(p.Path).TextColor(t.TextMuted)
					progressLines++
				}
				for ; progressLines < 3; progressLines++ {
					progressLinePlaceholder(c)
				}
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				components.Text(c, a.tr("不会删除来源文件")).FontSize(12).TextColor(t.TextMuted)
				ui.Spacer(c)
				if components.Button(c, a.tr("取消"), components.ButtonOutline).Disabled(!a.running || a.canceling).Clicked() {
					a.canceling = true
					a.cancel()
					a.status = "正在取消…"
				}
				if a.running {
					label := "暂停"
					if a.paused {
						label = "继续"
					}
					if components.Button(c, a.tr(label), components.ButtonPrimary).Disabled(a.canceling).Clicked() {
						if a.speedMeter != nil {
							a.speedMeter.reset(time.Now())
						}
						if a.paused {
							a.gate.Resume()
							a.paused = false
							a.speedValid = false
							a.status = "任务进行中…"
						} else {
							a.gate.Pause()
							a.paused = true
							a.status = "已暂停"
						}
					}
				} else {
					actionLabel := "开始复制"
					if verifyOnly {
						actionLabel = "开始校验"
					}
					if components.Button(c, a.tr(actionLabel), components.ButtonPrimary).Disabled(a.targetDropPending).Clicked() {
						a.start()
					}
				}
			})
		})
	})
	a.advancedDialog(c)
	a.persistPreferences()
}
func main() {
	a := &app{status: "选择来源与目标后开始复制"}
	a.applyPreferences(defaultPreferences())
	preferencesStore, preferencesErr := defaultPreferencesStore()
	if preferencesErr == nil {
		a.preferencesStore = preferencesStore
		var saved preferences
		saved, preferencesErr = preferencesStore.load()
		if preferencesErr == nil {
			a.applyPreferences(saved)
		}
	}
	if preferencesErr != nil {
		a.preferencesWarning = "无法读取设置：" + preferencesErr.Error()
	}
	a.lastPreferences = a.preferences()
	store, err := defaultHistoryStore()
	if err != nil {
		a.historyWarning = "无法读取历史记录：" + err.Error()
	} else {
		a.historyStore = store
		a.destinationHistory, err = store.load()
		if err != nil {
			a.historyWarning = "无法读取历史记录：" + err.Error()
		}
	}
	mygo.App.WhenReady(func() {
		a.win = newMainWindow(a)
		if err := a.win.SetIcon(appIcon); err != nil {
			log.Printf("设置窗口图标失败: %v", err)
		}
		a.win.OnMaximize(a.win.Invalidate)
		a.win.OnUnmaximize(a.win.Invalidate)
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.running {
				e.PreventDefault()
				a.closing = true
				a.canceling = true
				a.status = "正在取消…"
				a.cancel()
				a.win.Invalidate()
			}
		})
		a.win.OnClosed(func() {
			a.flushPreferences()
			if a.cancel != nil {
				a.cancel()
			}
			mygo.App.Quit()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
