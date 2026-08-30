package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"my-geo/internal/config"
)

// StripeProvider Stripe 支付渠道（海外）。纯标准库实现。
//
// 凭据（缺任一项则渠道未启用）：
//   - GEO_STRIPE_SECRET_KEY        SK 测试/生产密钥
//   - GEO_STRIPE_WEBHOOK_SECRET     Webhook 签名密钥（校验回调）
//   - GEO_STRIPE_API_BASE          可选，默认 https://api.stripe.com
type StripeProvider struct {
	secretKey     string
	webhookSecret string
	apiBase       string
}

func init() {
	register("stripe", func() Provider {
		sk := envOrEmpty("GEO_STRIPE_SECRET_KEY")
		if sk == "" {
			return nil
		}
		wh := envOrEmpty("GEO_STRIPE_WEBHOOK_SECRET")
		base := envOrEmpty("GEO_STRIPE_API_BASE")
		if base == "" {
			base = "https://api.stripe.com"
		}
		return &StripeProvider{secretKey: sk, webhookSecret: wh, apiBase: base}
	})
}

// Name 渠道标识。
func (p *StripeProvider) Name() string { return "stripe" }

// CreateCheckout 创建 Stripe Checkout Session，返回托管支付页 URL。
func (p *StripeProvider) CreateCheckout(ctx context.Context, o Order, returnURL string) (*CheckoutResult, error) {
	amount := o.AmountCents
	if amount <= 0 {
		amount = 0 // 免费单 Stripe 不收，但流程保持统一
	}
	currency := strings.ToLower(o.Currency)
	if currency == "" {
		currency = "cny"
	}
	if returnURL == "" {
		returnURL = config.Env("GEO_BILLING_RETURN_URL", "")
	}
	// Stripe REST API 只接受 application/x-www-form-urlencoded（嵌套参数用
	// line_items[0][...] 方括号形式），发 JSON 会返回 4xx 并触发"渠道失败降级手动"。
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("client_reference_id", o.ID)
	form.Set("success_url", returnURL)
	form.Set("metadata[order_id]", o.ID)
	form.Set("metadata[workspace_id]", o.WorkspaceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", currency)
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(amount, 10))
	form.Set("line_items[0][price_data][product_data][name]", "GEO "+o.Plan+" 订阅")
	payload := []byte(form.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.apiBase+"/v1/checkout/sessions", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Stripe-Version", "2023-10-16")
	resp, err := paymentHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("stripe: 读取响应失败: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("stripe: HTTP %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("stripe: 解析响应失败: %w", err)
	}
	if parsed.URL == "" {
		return nil, fmt.Errorf("stripe: 响应缺少 url")
	}
	return &CheckoutResult{
		URL:             parsed.URL,
		ProviderOrderID: parsed.ID,
		Raw:             map[string]any{"id": parsed.ID, "url": parsed.URL},
	}, nil
}

// VerifyWebhook 校验 Stripe 回调签名并解析事件。
// 签名头格式：t=<ts>,v1=<hex>,v0=<hex>；HMAC-SHA256 作用于 "<ts>.<body>"。
func (p *StripeProvider) VerifyWebhook(r *http.Request, body []byte) (*WebhookEvent, error) {
	if p.webhookSecret == "" {
		return nil, fmt.Errorf("stripe: 未配置 GEO_STRIPE_WEBHOOK_SECRET，无法校验签名")
	}
	sigHeader := r.Header.Get("Stripe-Signature")
	if sigHeader == "" {
		return nil, fmt.Errorf("stripe: 缺少 Stripe-Signature 头")
	}
	ts, v1, ok := parseStripeSigHeader(sigHeader)
	if !ok {
		return nil, fmt.Errorf("stripe: Stripe-Signature 格式非法")
	}
	// 校验时间戳，防止重放攻击（允许 ±5 分钟时钟漂移）。
	tsUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("stripe: 签名时间戳非法")
	}
	if diff := time.Now().Unix() - tsUnix; diff < -300 || diff > 300 {
		return nil, fmt.Errorf("stripe: 签名时间戳已过期")
	}
	mac := hmac.New(sha256.New, []byte(p.webhookSecret))
	mac.Write([]byte(ts + "." + string(body)))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(v1)) {
		return nil, fmt.Errorf("stripe: 签名不匹配")
	}
	var evt struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string            `json:"id"`
				ClientReferenceID string            `json:"client_reference_id"`
				Metadata          map[string]string `json:"metadata"`
				PaymentIntent     string            `json:"payment_intent"`
				PaymentStatus     string            `json:"payment_status"`
				AmountTotal       int64             `json:"amount_total"` // 最低货币单位（CNY 为分）
				Currency          string            `json:"currency"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return nil, fmt.Errorf("stripe: 解析事件失败: %w", err)
	}
	orderID := evt.Data.Object.ClientReferenceID
	if orderID == "" {
		orderID = evt.Data.Object.Metadata["order_id"]
	}
	status := "pending"
	if evt.Type == "checkout.session.completed" && evt.Data.Object.PaymentStatus == "paid" {
		status = "paid"
	}
	// Stripe 金额为最低货币单位；零小数货币（JPY 等）不做换算，CNY/USD 等恰为分
	amountCents := evt.Data.Object.AmountTotal
	if status == "paid" && amountCents <= 0 {
		amountCents = 0 // 渠道未提供金额时置 0，由上层跳过校验
	}
	return &WebhookEvent{
		Provider:        "stripe",
		OrderID:         orderID,
		ProviderOrderID: evt.Data.Object.ID,
		Status:          status,
		AmountCents:     amountCents,
		RawBody:         string(body),
	}, nil
}

// parseStripeSigHeader 解析 "t=...,v1=...,v0=..." 头。
func parseStripeSigHeader(h string) (ts, v1 string, ok bool) {
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			v1 = kv[1]
		}
	}
	return ts, v1, ts != "" && v1 != ""
}

// ComputeStripeSignature 计算给定时间戳与请求体的 HMAC-SHA256 签名（供单测与本地联调）。
func ComputeStripeSignature(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	return hex.EncodeToString(mac.Sum(nil))
}

// stripeNowTimestamp 返回 Stripe 风格的时间戳字符串（便于单测复现）。
func stripeNowTimestamp() string { return fmt.Sprintf("%d", time.Now().Unix()) }
