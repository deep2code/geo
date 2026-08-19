package payment

import (
	"testing"
)

// TestAllMethodsCoexist 锁定「微信/支付宝/Stripe + 手动激活」并存语义：
// 即使未配置任何商户凭据，四者仍全部返回，且 manual 恒为可用。
func TestAllMethodsCoexist(t *testing.T) {
	ms := AllMethods()

	// 必须恰好 4 个：wechatpay / alipay / stripe / manual。
	if len(ms) != 4 {
		t.Fatalf("期望 4 种支付方式（微信/支付宝/Stripe/手动），实际 %d: %+v", len(ms), ms)
	}

	want := map[string]struct {
		region  string
		currency string
	}{
		"wechatpay": {"domestic", "CNY"},
		"alipay":    {"domestic", "CNY"},
		"stripe":    {"overseas", "USD"},
		"manual":    {"both", ""},
	}
	seen := map[string]bool{}
	for _, m := range ms {
		w, ok := want[m.ID]
		if !ok {
			t.Fatalf("出现非预期支付方式 %q", m.ID)
		}
		if m.Region != w.region || m.Currency != w.currency {
			t.Errorf("支付方式 %q 元信息不符：got region=%q currency=%q, want region=%q currency=%q",
				m.ID, m.Region, m.Currency, w.region, w.currency)
		}
		seen[m.ID] = true
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("缺少支付方式 %q", id)
		}
	}

	// manual 始终 configured=true（轻量版核心，不依赖任何商户凭据）。
	manual := ms[len(ms)-1]
	if manual.ID != "manual" || !manual.Configured {
		t.Errorf("manual 必须置底且恒为可用：got %+v", manual)
	}

	// 未配置凭据时，三家在线渠道应标为未配置（置灰态，而非消失）。
	for _, id := range []string{"wechatpay", "alipay", "stripe"} {
		for _, m := range ms {
			if m.ID == id && m.Configured {
				t.Errorf("未配置凭据时 %q 不应标为已配置", id)
			}
		}
	}

	// 排序：国内（wechatpay/alipay）在前，海外（stripe）其次，manual 置底。
	if ms[0].Region != "domestic" {
		t.Errorf("首位应为国内渠道，实际 %q", ms[0].ID)
	}
	if ms[2].ID != "stripe" {
		t.Errorf("第三位应为 stripe，实际 %q", ms[2].ID)
	}
}
