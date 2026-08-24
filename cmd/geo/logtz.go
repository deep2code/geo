package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
)

// tzHandler 是一个 slog.Handler 的极薄 wrapper：把每条日志记录的时间
// 从 UTC 转换到目标时区（默认 Asia/Shanghai，可用 TZ 环境变量覆盖）后再
// 交给底层 handler 输出。其余字段、级别、格式完全透传，不影响现有 text/json 两种模式。
//
// 背景：Go 标准 slog 的 Text/JSON handler 强制以 UTC 输出时间戳（不提供时区选项），
// 容器默认时区也是 UTC，导致日志比北京时间慢 8 小时、看着“不对”。
// 这里在应用层显式转换时区，使日志时间与使用者所在地一致（前端时间本就本地时区）。
type tzHandler struct {
	inner slog.Handler
	loc   *time.Location
}

// newTZHandler 构造时区 handler。loc 非空；tzName 为空时用 Asia/Shanghai。
func newTZHandler(inner slog.Handler, tzName string) *tzHandler {
	loc := time.UTC
	if tzName == "" {
		tzName = "Asia/Shanghai"
	}
	if l, err := time.LoadLocation(tzName); err == nil {
		loc = l
	}
	return &tzHandler{inner: inner, loc: loc}
}

// Enabled 透传。
func (h *tzHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle 克隆记录并把时间转换到目标时区后透传。
func (h *tzHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Time = r.Time.In(h.loc)
	return h.inner.Handle(ctx, r)
}

// WithAttrs 透传。
func (h *tzHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &tzHandler{inner: h.inner.WithAttrs(attrs), loc: h.loc}
}

// WithGroup 透传。
func (h *tzHandler) WithGroup(name string) slog.Handler {
	return &tzHandler{inner: h.inner.WithGroup(name), loc: h.loc}
}

// resolveLogLocation 解析日志时区：优先 TZ 环境变量，缺省 Asia/Shanghai。
// 加载失败（如容器内未装 tzdata）时回退 UTC，保证程序不因时区问题崩溃。
func resolveLogLocation() *time.Location {
	tz := strings.TrimSpace(os.Getenv("TZ"))
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}
