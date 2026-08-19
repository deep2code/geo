package billing

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newID 生成带前缀的随机 ID（16 字节熵，URL 安全，无依赖）。
func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 在主流平台不会失败；极端情况下退化为时间熵。
		return fmt.Sprintf("%s-%d", prefix, 0)
	}
	if prefix == "" {
		return hex.EncodeToString(b)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
