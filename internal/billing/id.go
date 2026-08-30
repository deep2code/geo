package billing

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"
)

// newID 生成带前缀的随机 ID（16 字节熵，URL 安全，无依赖）。
func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 在主流平台不会失败；极端情况退化为时间+计数熵。
		// 不能退化为常量 0——同窗口多笔订单会拿到同一 ID 造成主键冲突。
		fallbackNewIDMu.Lock()
		fallbackNewIDSeq++
		seq := fallbackNewIDSeq
		fallbackNewIDMu.Unlock()
		b = make([]byte, 16)
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(b[8:], uint64(seq))
	}
	if prefix == "" {
		return hex.EncodeToString(b)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

// fallbackNewIDSeq / fallbackNewIDMu 是 rand 失败时的时间熵回退计数器。
var (
	fallbackNewIDSeq uint64
	fallbackNewIDMu  sync.Mutex
)
