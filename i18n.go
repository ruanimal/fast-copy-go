package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The existing Chinese option values and status messages remain stable internal
// identifiers, so preferences written by older releases continue to work.
var english = map[string]string{
	"语言":   "Language",
	"界面语言": "Interface language",
	"主题":   "Theme",
	"界面主题": "Interface theme",
	"亮色":   "Light",
	"暗色":   "Dark",
	"选择路径": "Choose paths",
	"文件复制": "File copy",
	"选择来源和目标，开始一个复制任务": "Choose sources and a destination to start a copy task",
	"遇错继续":  "Continue on error",
	"高级设置…": "Advanced settings…",
	"高级设置":  "Advanced settings",
	"来源":    "Sources",
	"清空":    "Clear",
	"来源路径":  "Source paths",
	"选择、拖入或粘贴文件、文件夹，也可以输入路径（每行一个路径）": "Select, drop or paste files or folders, or enter paths (one per line)",
	"添加文件":         "Add files",
	"添加文件夹":        "Add folders",
	"目标文件夹":        "Destination folder",
	"浏览…":          "Browse…",
	"选择或拖入已有文件夹":   "Select or drop an existing folder",
	"目标文件夹路径":      "Destination folder path",
	"目标文件夹历史":      "Destination history",
	"最近使用的目标文件夹":   "Recent destinations",
	"清除历史记录":       "Clear history",
	"操作":           "Operation",
	"操作类型":         "Operation type",
	"复制":           "Copy",
	"仅校验":          "Verify only",
	"已存在的文件":       "Existing files",
	"已存在文件的处理方式":   "Existing file handling",
	"大小或时间不同":      "Size or time differs",
	"覆盖":           "Overwrite",
	"跳过已有文件":       "Skip existing files",
	"仅覆盖较旧文件":      "Overwrite older files only",
	"读写策略":         "I/O strategy",
	"读写策略选择":       "I/O strategy selection",
	"自动":           "Auto",
	"同一硬盘":         "Same disk",
	"不同设备 / SSD":   "Different devices / SSD",
	"包含":           "Include",
	"排除":           "Exclude",
	"包含过滤":         "Include filter",
	"排除过滤":         "Exclude filter",
	"多个过滤规则用分号分隔":  "Separate filter rules with semicolons",
	"例如 *.txt;照片/": "e.g. *.txt;photos/",
	"例如 *.tmp;缓存/": "e.g. *.tmp;cache/",
	"任务进度卡片":       "Task progress panel",
	"任务进度":         "Task progress",
	"复制进度":         "Copy progress",
	"扫描中":          "Scanning",
	"复制中":          "Copying",
	"校验中":          "Verifying",
	"已完成":          "Completed",
	"有错误":          "Errors",
	"待开始":          "Ready",
	"未完成":          "Incomplete",
	"已取消":          "Canceled",
	"已暂停":          "Paused",
	"正在取消":         "Canceling",
	"处理速度 暂无进度":    "Processing speed: no progress",
	"每半秒更新，按最近约 3 秒内已完成的逻辑处理量计算；暂无进度可能表示读写或同步尚未完成，不表示磁盘空闲。稀疏文件空洞和保留的硬链接也会计入，不代表物理磁盘吞吐量。": "Updated every half second using logical progress over roughly the last 3 seconds. No progress may mean I/O or sync is still in progress. Sparse holes and preserved hard links are included, so this is not physical disk throughput.",
	"%s / %s  ·  完成 %d / %d 个文件  ·  跳过 %d": "%s / %s  ·  %d / %d files completed  ·  %d skipped",
	"校验队列 %d 项":       "Verification queue: %d",
	"错误 %d 项":         "Errors: %d",
	" · %d 个文件":       " · %d files",
	"同盘批量":            "Same-disk batch",
	"并行读写":            "Parallel I/O",
	" 回退原因：":          " Fallback reason: ",
	" 说明：":            " Details: ",
	"查看错误":            "View errors",
	"任务错误":            "Task errors",
	"以下项目未能完成":        "The following items could not be completed",
	"不会删除来源文件":        "Source files are not deleted",
	"取消":              "Cancel",
	"暂停":              "Pause",
	"继续":              "Resume",
	"开始复制":            "Start copy",
	"开始校验":            "Start verification",
	"文件属性与空间":         "File attributes and space",
	"扩展属性":            "Extended attributes",
	"硬链接":             "Hard links",
	"预分配空间":           "Preallocate",
	"校验":              "Verification",
	"校验文件":            "Verify files",
	"校验算法":            "Verification algorithm",
	"重读来源":            "Reread source",
	"单次 I/O（MiB）":     "I/O size (MiB)",
	"单次 I/O 大小":       "I/O size",
	"同盘缓冲（MiB）":       "Same-disk buffer (MiB)",
	"同盘缓冲大小":          "Same-disk buffer size",
	"时间容差（毫秒）":        "Time tolerance (ms)",
	"时间容差":            "Time tolerance",
	"优先目标直接写入（Linux）": "Prefer direct destination writes (Linux)",
	"来源使用程序缓冲区读取，目标尝试直接写入；不支持时在写入前回退。稀疏文件保留原有处理方式。": "Read sources into application buffers and attempt direct writes to the destination; fall back before writing if unsupported. Sparse files retain their existing handling.",
	"关闭":          "Close",
	"最小化":         "Minimize",
	"最大化":         "Maximize",
	"还原窗口":        "Restore window",
	"关闭窗口":        "Close window",
	"%.2f 秒":      "%.2f s",
	"%.1f 毫秒":     "%.1f ms",
	"%d 分 %02d 秒": "%d min %02d s",
	"平均速度 %s/s · 总耗时 %s · 已复制 %s（逻辑大小）":                      "Average speed %s/s · Elapsed %s · Copied %s (logical size)",
	"按成功复制文件的逻辑大小计算；耗时包含扫描、校验和暂停时间。稀疏文件及硬链接的逻辑大小可能大于实际写入量。 ": "Based on the logical size of successfully copied files; elapsed time includes scanning, verification and pauses. Sparse files and hard links may have a larger logical size than physical writes. ",
	"普通 I/O（建议回收缓存）":  "Buffered I/O (cache eviction advised)",
	"普通 I/O":          "Buffered I/O",
	"直接 I/O":          "Direct I/O",
	"异步直接 I/O":        "Async direct I/O",
	"禁用缓存":            "Uncached I/O",
	"稀疏文件普通 I/O":      "Buffered sparse I/O",
	"稀疏文件 I/O（禁用缓存）":  "Uncached sparse I/O",
	"批量普通 I/O":        "Batched buffered I/O",
	"跨文件直接 I/O":       "Batched direct I/O",
	"跨文件批量 I/O（直接写入）": "Batched I/O (direct writes)",
	"目标直接写入":          "Direct destination writes",
	"当前文件使用普通读写；是否经过缓存取决于两端文件系统和挂载选项。Linux 会建议内核在处理后回收缓存，但不保证即时生效。": "This file uses buffered I/O; caching depends on the filesystems and mount options. On Linux, cache eviction is advisory and may not be immediate.",
	"来源使用普通读取，目标使用直接 I/O 写入；普通读取的缓存行为由文件系统和挂载选项决定。":                 "The source uses buffered reads and the destination uses direct writes. Read caching depends on the filesystem and mount options.",
	"来源使用普通读取和程序缓冲区，目标已请求 O_DIRECT 写入；实际效果由文件系统和内核决定。":              "The source uses buffered reads; O_DIRECT writes were requested for the destination. Actual behavior depends on the filesystem and kernel.",
	"文件数据已请求直接 I/O；独立校验读取可能使用系统缓存。":                                 "Direct I/O was requested for file data; separate verification reads may use the system cache.",
	"已请求系统绕过文件缓存，实际效果由操作系统和文件系统决定。":                                 "Bypassing the file cache was requested; actual behavior depends on the OS and filesystem.",
	"I/O 方式由当前文件及文件系统决定。":                                           "I/O mode depends on the current file and filesystem.",
	"选择来源与目标后开始复制":        "Choose sources and a destination to start copying",
	"任务进行中…":              "Task in progress…",
	"正在取消…":               "Canceling…",
	"已取消；已完成的文件保留":        "Canceled; completed files were kept",
	"校验完成":                "Verification complete",
	"复制完成":                "Copy complete",
	"正在检查目标文件夹…":          "Checking destination folder…",
	"目标文件夹已设置":            "Destination folder set",
	"历史记录仅在本次运行可用":        "History is available only for this session",
	"无法添加包含换行符的路径":        "Cannot add a path containing a newline",
	"无法添加空路径或包含换行符的路径":    "Cannot add an empty path or a path containing a newline",
	"请选择文件夹":              "Choose a folder",
	"请选择一个目标文件夹":          "Choose one destination folder",
	"目标路径为空或包含换行符":        "Destination path is empty or contains a newline",
	"无法读取剪贴板中的文件":         "Cannot read files from the clipboard",
	"剪贴板包含无法识别的本地文件路径":    "Clipboard contains unrecognized local file paths",
	"同盘缓冲不能小于单次 I/O 大小":   "Same-disk buffer cannot be smaller than the I/O size",
	"目标文件系统报告不支持直接 I/O":   "Destination filesystem reports direct I/O is unsupported",
	"目标未报告直接 I/O 对齐信息":    "Destination did not report direct I/O alignment",
	"exFAT 文件系统块大小超出安全范围": "exFAT block size is outside the safe range",
	"目标直接 I/O 对齐要求超出安全范围": "Destination direct I/O alignment is outside the safe range",
	"目标未报告直接 I/O 对齐信息，已按 exFAT 文件系统块和内存页大小保守对齐": "Destination did not report direct I/O alignment; using conservative exFAT block and page alignment",
	"来源使用普通读取，目标已请求直接写入":                        "Buffered source reads and direct destination writes were requested",
	"保留稀疏文件空洞的路径使用普通 I/O":                       "Buffered I/O preserves sparse file holes",
	"来源批量读取未启用直接 I/O":                           "Direct I/O was not enabled for batched source reads",
	"目标批量写入未启用直接 I/O":                           "Direct I/O was not enabled for batched destination writes",
	"操作系统未启用无缓存 I/O":                            "The OS did not enable uncached I/O",
	"来源和目标的直接 I/O 对齐要求超出安全范围":                   "Source and destination direct I/O alignment requirements exceed the safe range",
	"来源文件系统未报告直接 I/O 对齐信息（STATX_DIOALIGN）":      "Source filesystem did not report direct I/O alignment (STATX_DIOALIGN)",
	"目标文件系统未报告直接 I/O 对齐信息（STATX_DIOALIGN）":      "Destination filesystem did not report direct I/O alignment (STATX_DIOALIGN)",
	"来源文件系统报告不支持直接 I/O":                         "Source filesystem reports direct I/O is unsupported",
}

func (a *app) tr(key string) string {
	if a.language == "en" {
		if value, ok := english[key]; ok {
			return value
		}
	}
	return key
}

func (a *app) operationWidth() float32 {
	if a.language == "en" {
		return 140
	}
	return 110
}

var localizedPatterns = []struct {
	pattern *regexp.Regexp
	format  string
}{
	{regexp.MustCompile(`^任务完成，但有 ([0-9]+) 项错误$`), "Task completed with %s errors"},
	{regexp.MustCompile(`^请输入 ([0-9]+) 到 ([0-9]+) 之间的整数$`), "Enter an integer between %s and %s"},
}

// Translate only known message boundaries, preserving paths and OS error text.
func (a *app) localizeMessage(message string) string {
	if a.language != "en" || message == "" {
		return message
	}
	if translated, ok := english[message]; ok {
		return translated
	}
	for _, p := range localizedPatterns {
		if m := p.pattern.FindStringSubmatch(message); m != nil {
			args := make([]any, len(m)-1)
			for i := range args {
				args[i] = m[i+1]
			}
			return fmt.Sprintf(p.format, args...)
		}
	}
	for _, p := range []struct{ zh, en string }{
		{"失败：", "Failed: "}, {"无法使用目标文件夹：", "Cannot use destination folder: "},
		{"无法保存历史记录：", "Cannot save history: "}, {"无法读取历史记录：", "Cannot read history: "}, {"无法清除历史记录：", "Cannot clear history: "}, {"读取目标文件夹历史：", "Reading destination history: "},
		{"无法保存设置：", "Cannot save settings: "}, {"无法读取设置：", "Cannot read settings: "}, {"解析设置：", "Parsing settings: "}, {"单次 I/O 大小：", "I/O size: "}, {"同盘缓冲：", "Same-disk buffer: "}, {"时间容差：", "Time tolerance: "},
		{"处理速度 ", "Processing speed: "}, {"目标直接 I/O 对齐查询失败：", "Destination direct I/O alignment query failed: "}, {"目标不支持 O_DIRECT：", "Destination does not support O_DIRECT: "}, {"来源不支持 O_DIRECT：", "Source does not support O_DIRECT: "}, {"目标拒绝直接写入，已回退普通 I/O：", "Destination rejected direct writes; fell back to buffered I/O: "}, {"目标拒绝直接写入：", "Destination rejected direct writes: "}, {"来源无法查询直接 I/O 对齐信息：", "Cannot query source direct I/O alignment: "}, {"目标无法查询直接 I/O 对齐信息：", "Cannot query destination direct I/O alignment: "},
	} {
		if strings.HasPrefix(message, p.zh) {
			return p.en + a.localizeMessage(strings.TrimPrefix(message, p.zh))
		}
	}
	// The engine can join two known reasons with this delimiter.
	if left, right, ok := strings.Cut(message, "；"); ok {
		if _, known := english[left]; known {
			return a.localizeMessage(left) + "; " + a.localizeMessage(right)
		}
	}
	return message
}
