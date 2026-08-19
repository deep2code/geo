package payment

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// registry 渠道构造表；各渠道文件在 init() 中注册。
var (
	regMu    sync.RWMutex
	registry = map[string]func() Provider{}
)

// register 供各渠道 init() 调用，注册「凭据齐备时构造 Provider」的工厂。
// 工厂返回 nil 表示凭据缺失（渠道未启用）。
func register(name string, ctor func() Provider) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[name] = ctor
}

// GetProvider 返回已配置凭据的渠道实例；未配置或未注册返回 nil。
// 调用方（billing）应据此降级为手动激活模式。
func GetProvider(name string) Provider {
	regMu.RLock()
	ctor, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	regMu.RUnlock()
	if !ok {
		return nil
	}
	p := ctor()
	if p == nil {
		return nil
	}
	return p
}

// ConfiguredProviders 返回当前已启用（凭据齐备）的渠道名，按字母序。
// 用于 /api/v1/billing/plans 的可用支付方式提示与系统诊断。
func ConfiguredProviders() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	var names []string
	for name, ctor := range registry {
		if ctor() != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// envOrEmpty 读取环境变量，未设置返回空串。
func envOrEmpty(k string) string { return strings.TrimSpace(os.Getenv(k)) }
