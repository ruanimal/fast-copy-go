package main

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const speedWindow = 3 * time.Second

type speedSample struct {
	at    time.Time
	bytes int64
}

// speedMeter averages completed logical progress over a short rolling window.
// It does not estimate bytes still inside an unfinished I/O operation.
type speedMeter struct {
	processed  atomic.Int64
	generation atomic.Uint64
	mu         sync.Mutex
	samples    []speedSample
}

func (m *speedMeter) reset(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples[:0], speedSample{at: at, bytes: m.processed.Load()})
	m.generation.Add(1)
}

func (m *speedMeter) sample(at time.Time) (int64, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.processed.Load()
	generation := m.generation.Load()
	if len(m.samples) == 0 || !at.After(m.samples[len(m.samples)-1].at) || current < m.samples[len(m.samples)-1].bytes {
		m.samples = append(m.samples[:0], speedSample{at: at, bytes: current})
		return 0, generation
	}
	m.samples = append(m.samples, speedSample{at: at, bytes: current})
	cutoff := at.Add(-speedWindow)
	// Keep the last sample at or before the cutoff as the interval baseline.
	for len(m.samples) > 1 && !m.samples[1].at.After(cutoff) {
		m.samples = m.samples[1:]
	}
	first := m.samples[0]
	return processingSpeed(first.bytes, current, at.Sub(first.at)), generation
}

func processingSpeed(previous, current int64, elapsed time.Duration) int64 {
	if current <= previous || elapsed <= 0 {
		return 0
	}
	speed := float64(current-previous) / elapsed.Seconds()
	if speed >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(speed)
}

func ioBackendExplanation(backend string) string {
	switch backend {
	case "buffered", "batch_buffered", "sparse":
		return "当前文件使用普通读写；是否经过缓存取决于两端文件系统和挂载选项。Linux 会建议内核在处理后回收缓存，但不保证即时生效。"
	case "batch_write_direct":
		return "来源使用普通读取，目标使用直接 I/O 写入；普通读取的缓存行为由文件系统和挂载选项决定。"
	case "target_direct":
		return "来源使用普通读取和程序缓冲区，目标已请求 O_DIRECT 写入；实际效果由文件系统和内核决定。"
	case "direct", "direct_async", "batch_direct":
		return "文件数据已请求直接 I/O；独立校验读取可能使用系统缓存。"
	case "nocache", "sparse_nocache":
		return "已请求系统绕过文件缓存，实际效果由操作系统和文件系统决定。"
	default:
		return "I/O 方式由当前文件及文件系统决定。"
	}
}

func ioBackendLabel(backend string) string {
	if backend == "buffered" {
		if runtime.GOOS == "linux" {
			return "普通 I/O（建议回收缓存）"
		}
		return "普通 I/O"
	}
	return map[string]string{
		"direct": "直接 I/O", "direct_async": "异步直接 I/O", "nocache": "禁用缓存",
		"sparse": "稀疏文件普通 I/O", "sparse_nocache": "稀疏文件 I/O（禁用缓存）",
		"batch_buffered": "批量普通 I/O", "batch_direct": "跨文件直接 I/O", "batch_write_direct": "跨文件批量 I/O（直接写入）", "target_direct": "目标直接写入",
	}[backend]
}
