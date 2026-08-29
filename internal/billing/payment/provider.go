// Package payment 支付渠道抽象层。统一封装微信支付、支付宝、Stripe 的下单与
// Webhook 校验，全部基于标准库（crypto/rsa、crypto/sha256、crypto/aes、net/http），
// 零第三方 SDK 依赖。
//
// 设计哲学：凭据缺失时渠道「未启用」（GetProvider 返回 nil），计费层据此自动
// 降级为「手动激活」模式——这正是「免费 + 手动激活」轻量版的核心：无需任何
// 商户账号即可跑通订阅/配额闭环；配置环境变量后无缝切换为在线支付。
package payment

import (
	"context"
	"net/http"
)

// Order 渠道下单请求（与 billing.Order 解耦，避免循环依赖）。
type Order struct {
	ID          string // 我方订单 ID（渠道透传，用于回调解关联）
	WorkspaceID string
	Plan        string
	AmountCents int64  // 金额（分）
	Currency    string // CNY / USD
	ReturnURL   string // 支付完成回跳地址（Stripe 用）
}

// CheckoutResult 渠道下单结果。
type CheckoutResult struct {
	URL             string         // 用户跳转支付页（Stripe 托管页 / 微信 Native code_url / 支付宝 page 表单）
	ProviderOrderID string         // 渠道侧订单号
	Raw             map[string]any // 调试用原始响应
}

// WebhookEvent 标准化支付回调事件。
type WebhookEvent struct {
	Provider        string
	OrderID         string // 我方订单 ID
	ProviderOrderID string
	Status          string // paid / failed / refunded
	AmountCents     int64  // 渠道侧实际支付金额（分）；0 表示渠道未提供（跳过金额校验）
	RawBody         string
}

// Provider 支付渠道统一接口。
type Provider interface {
	// Name 渠道标识：wechatpay / alipay / stripe。
	Name() string
	// CreateCheckout 创建支付会话，返回跳转地址。
	CreateCheckout(ctx context.Context, o Order, returnURL string) (*CheckoutResult, error)
	// VerifyWebhook 校验回调签名并解析为标准事件；签名失败返回 error。
	VerifyWebhook(r *http.Request, body []byte) (*WebhookEvent, error)
}
