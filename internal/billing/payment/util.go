package payment

import (
	"encoding/json"
	"fmt"
	"time"
)

// jsonMarshal 标准库 JSON 序列化（支付宝 biz_content 需要紧凑无多余空白）。
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("payment: JSON 序列化失败: %w", err)
	}
	return string(b), nil
}

// alipayTimestamp 支付宝要求的 yyyy-MM-dd HH:mm:ss（本地时区）。
func alipayTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
