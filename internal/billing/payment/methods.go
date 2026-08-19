package payment

import "sort"

// Method 支付方式元信息（用于前端渲染支付按钮与轻量版入口）。
type Method struct {
	ID         string `json:"id"`         // wechatpay / alipay / stripe / manual
	Name       string `json:"name"`       // 微信支付 / 支付宝 / Stripe / 手动激活
	Region     string `json:"region"`     // domestic（国内）/ overseas（海外）/ both
	Currency   string `json:"currency"`   // CNY / USD（manual 为空）
	Configured bool   `json:"configured"` // 在线渠道凭据是否齐备；manual 恒为 true
}

// knownMeta 在线渠道的静态展示信息。即使凭据缺失也能展示按钮（置灰态），
// 凭据齐备时由 AllMethods 动态标注 Configured=true。
var knownMeta = map[string]Method{
	"wechatpay": {ID: "wechatpay", Name: "微信支付", Region: "domestic", Currency: "CNY"},
	"alipay":    {ID: "alipay", Name: "支付宝", Region: "domestic", Currency: "CNY"},
	"stripe":    {ID: "stripe", Name: "Stripe", Region: "overseas", Currency: "USD"},
}

// AllMethods 返回全部已知支付方式，在线渠道（微信/支付宝/Stripe）与手动激活
// 并存、互不替代：配置好的亮起，未配置的展示为「待配置」，`manual` 始终可选。
// 国内渠道（微信/支付宝）在前，海外（Stripe）在后，手动激活置底。
func AllMethods() []Method {
	regMu.RLock()
	defer regMu.RUnlock()
	var ms []Method
	for id, meta := range knownMeta {
		m := meta
		if ctor, ok := registry[id]; ok && ctor() != nil {
			m.Configured = true
		}
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool {
		// 国内优先，其次按 ID 稳定排序。
		if ms[i].Region != ms[j].Region {
			return ms[i].Region == "domestic"
		}
		return ms[i].ID < ms[j].ID
	})
	ms = append(ms, Method{
		ID: "manual", Name: "手动激活（免费/轻量版）",
		Region: "both", Currency: "", Configured: true,
	})
	return ms
}
