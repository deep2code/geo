package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// rotatingWriter 是一个零依赖的滚动日志 writer：
//   - 单文件超过 maxSizeMB 时滚动（重命名为 <name>.<时间戳>.log，并新建当前文件）；
//   - 后台每小时清理一次：删除修改时间早于 maxAgeDays 天的滚动文件；
//   - 当前活跃文件（<name>）始终保留，不被清理。
//
// 用于 GEO_LOG_FILE 指定路径时，满足「单文件最多 20MB、最多保留 7 天」的需求，
// 且不引入第三方依赖（避免改动 go.mod 触发基础镜像重建）。
type rotatingWriter struct {
	dir     string
	name    string // 基础文件名，如 geo.log
	maxSize int64  // 单文件字节上限
	maxAge  time.Duration
	mu      sync.Mutex
	file    *os.File
	size    int64
}

// newRotatingWriter 创建滚动 writer；path 所在目录会被自动创建。
func newRotatingWriter(path string, maxSizeMB, maxAgeDays int) (*rotatingWriter, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &rotatingWriter{
		dir:     dir,
		name:    filepath.Base(path),
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		maxAge:  time.Duration(maxAgeDays) * 24 * time.Hour,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	go w.cleanupLoop()
	return w, nil
}

func (w *rotatingWriter) open() error {
	p := filepath.Join(w.dir, w.name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = fi.Size()
	return nil
}

func (w *rotatingWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(b)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(b)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
	}
	ts := time.Now().Format("20060102-150405")
	base := strings.TrimSuffix(w.name, ".log")
	rotated := filepath.Join(w.dir, base+"."+ts+".log")
	// 重命名当前活跃文件为带时间戳的滚动文件；若已不存在则忽略。
	_ = os.Rename(filepath.Join(w.dir, w.name), rotated)
	return w.open()
}

func (w *rotatingWriter) cleanupLoop() {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for range t.C {
		w.cleanup()
	}
}

// cleanup 删除修改时间早于保留期的滚动文件（当前活跃文件 name 永不被删）。
func (w *rotatingWriter) cleanup() {
	cut := time.Now().Add(-w.maxAge)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	base := strings.TrimSuffix(w.name, ".log")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == w.name {
			continue
		}
		if !strings.HasPrefix(name, base+".") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cut) {
			_ = os.Remove(filepath.Join(w.dir, name))
		}
	}
}

// 确保 rotatingWriter 实现 io.Writer（编译期检查）。
var _ io.Writer = (*rotatingWriter)(nil)
