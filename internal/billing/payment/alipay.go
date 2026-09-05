package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math"
	"my-geo/internal/config"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// AlipayProvider 支付宝支付渠道（国内）。纯标准库实现，使用 RSA2（SHA256WithRSA）。
//
// 凭据（缺任一项则渠道未启用）：
//   - GEO_ALIPAY_APP_ID          应用 ID
//   - GEO_ALIPAY_PRIVATE_KEY     商户应用私钥（PEM，PKCS1 或 PKCS8）
//   - GEO_ALIPAY_PUBLIC_KEY      支付宝公钥（PEM，PKIX），用于验签异步通知
//   - GEO_ALIPAY_GATEWAY         可选，默认 https://openapi.alipay.com/gateway.do
type AlipayProvider struct {
	appID        string
	privateKey   *rsa.PrivateKey
	alipayPubKey *rsa.PublicKey
	gateway      string
}

func init() {
	register("alipay", func() Provider {
		appID := envOrEmpty("GEO_ALIPAY_APP_ID")
		privPEM := envOrEmpty("GEO_ALIPAY_PRIVATE_KEY")
		pubPEM := envOrEmpty("GEO_ALIPAY_PUBLIC_KEY")
		if appID == "" || privPEM == "" || pubPEM == "" {
			return nil
		}
		priv, err := parseRSAPrivateKey(privPEM)
		if err != nil {
			slog.Warn("alipay: 私钥解析失败，支付渠道未启用", slog.Any("error", err))
			return nil
		}
		pub, err := parseRSAPublicKey(pubPEM)
		if err != nil {
			slog.Warn("alipay: 公钥解析失败，支付渠道未启用", slog.Any("error", err))
			return nil
		}
		gw := envOrEmpty("GEO_ALIPAY_GATEWAY")
		if gw == "" {
			gw = "https://openapi.alipay.com/gateway.do"
		}
		return &AlipayProvider{appID: appID, privateKey: priv, alipayPubKey: pub, gateway: gw}
	})
}

// Name 渠道标识。
func (p *AlipayProvider) Name() string { return "alipay" }

// CreateCheckout 生成支付宝网页支付跳转 URL（alipay.trade.page.pay）。
func (p *AlipayProvider) CreateCheckout(ctx context.Context, o Order, _ string) (*CheckoutResult, error) {
	biz := map[string]any{
		"out_trade_no": o.ID,
		"product_code": "FAST_INSTANT_TRADE_PAY",
		"total_amount": fmt.Sprintf("%d.%02d", o.AmountCents/100, o.AmountCents%100),
		"subject":      "GEO " + o.Plan + " 订阅",
	}
	bizJSON, err := jsonMarshal(biz)
	if err != nil {
		return nil, err
	}
	params := map[string]string{
		"app_id":      p.appID,
		"method":      "alipay.trade.page.pay",
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   alipayTimestamp(),
		"version":     "1.0",
		"notify_url":  config.Env("GEO_ALIPAY_NOTIFY_URL", ""),
		"return_url":  config.Env("GEO_BILLING_RETURN_URL", ""),
		"biz_content": bizJSON,
	}
	sign, err := signRSA2(params, p.privateKey)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return &CheckoutResult{
		URL:             p.gateway + "?" + q.Encode(),
		ProviderOrderID: o.ID,
		Raw:             map[string]any{"out_trade_no": o.ID},
	}, nil
}

// VerifyWebhook 校验支付宝异步通知签名并解析交易状态。
// 通知为表单 POST；验签使用支付宝公钥对排序参数（排除 sign/sign_type）做 RSA2。
func (p *AlipayProvider) VerifyWebhook(r *http.Request, body []byte) (*WebhookEvent, error) {
	// 注意：调用方（handlers.go 的 HandleWebhook）已用 io.ReadAll 读空 r.Body，
	// 此处必须解析已传入的 body，而非再次 r.ParseForm()——否则 PostForm 为空、
	// 永远拿不到 sign，所有支付宝回调都会失败。微信/Stripe 的 VerifyWebhook 同样消费 body 参数。
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("alipay: 解析表单失败: %w", err)
	}
	params := map[string]string{}
	for k, v := range vals {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	sign := params["sign"]
	if sign == "" {
		return nil, fmt.Errorf("alipay: 缺少 sign")
	}
	if !verifyRSA2(params, sign, p.alipayPubKey) {
		return nil, fmt.Errorf("alipay: 签名校验失败")
	}
	orderID := params["out_trade_no"]
	status := "pending"
	if strings.EqualFold(params["trade_status"], "TRADE_SUCCESS") ||
		strings.EqualFold(params["trade_status"], "TRADE_FINISHED") {
		status = "paid"
	}
	// total_amount 为元（两位小数字符串），换算为分做金额一致性校验
	amountCents := int64(0)
	if amt, aerr := strconv.ParseFloat(strings.TrimSpace(params["total_amount"]), 64); aerr == nil {
		amountCents = int64(math.Round(amt * 100))
	}
	return &WebhookEvent{
		Provider:        "alipay",
		OrderID:         orderID,
		ProviderOrderID: orderID,
		Status:          status,
		AmountCents:     amountCents,
		RawBody:         string(body),
	}, nil
}

// ---- RSA2 工具（可单测） ----

// signRSA2 对参数按 key 字典序拼接为 k=v&...，做 SHA256WithRSA 签名并 base64。
func signRSA2(params map[string]string, key *rsa.PrivateKey) (string, error) {
	return signSortedString(buildSortedQuery(params), key)
}

// verifyRSA2 用支付宝公钥校验签名。
func verifyRSA2(params map[string]string, sign string, pub *rsa.PublicKey) bool {
	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false
	}
	msg := buildSortedQuery(params)
	h := sha256.Sum256([]byte(msg))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], raw) == nil
}

// buildSortedQuery 排除 sign/sign_type 后按 key 升序拼 k=v&...（支付宝规则）。
func buildSortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(params[k])
	}
	return b.String()
}

func signSortedString(msg string, key *rsa.PrivateKey) (string, error) {
	h := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// parseRSAPrivateKey 解析 PKCS1 或 PKCS8 PEM 私钥。
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	pemStr = normalizePEM(pemStr, "PRIVATE KEY")
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("alipay: 私钥 PEM 解析失败")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	keyIface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("alipay: 私钥解析失败（非 PKCS1/PKCS8）: %w", err)
	}
	k, ok := keyIface.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("alipay: 私钥非 RSA")
	}
	return k, nil
}

// parseRSAPublicKey 解析 PKIX PEM 公钥或 X.509 证书（微信平台证书为
// -----BEGIN CERTIFICATE----- 形式，需先解析证书再取其公钥）。
func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	pemStr = normalizePEM(pemStr, "PUBLIC KEY")
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("alipay: 公钥 PEM 解析失败")
	}
	var pubAny any
	if block.Type == "CERTIFICATE" {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("证书解析失败: %w", err)
		}
		pubAny = cert.PublicKey
	} else {
		var err error
		pubAny, err = x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("alipay: 公钥解析失败: %w", err)
		}
	}
	k, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("alipay: 公钥非 RSA")
	}
	return k, nil
}

// normalizePEM 将可能被压缩成单行的 PEM（含 \n 转义或缺失换行）还原为标准 PEM。
// 支付宝控制台导出的密钥常带换行，这里容错处理：若已含 BEGIN 标记则原样；
// 纯 base64 单行则按 blockType（"PRIVATE KEY"/"PUBLIC KEY"）还原为 64 列 PEM。
func normalizePEM(s, blockType string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "-----BEGIN") {
		return s
	}
	// 尝试把 base64 大写/小写单行还原为 64 列 PEM（常见复制粘贴情形）。
	// 仅当看起来是纯 base64 时处理。
	clean := strings.ReplaceAll(s, " ", "")
	if len(clean) < 100 {
		return s
	}
	// chunk64 每列自带换行，END 前无需再补
	return "-----BEGIN " + blockType + "-----\n" + chunk64(clean) + "-----END " + blockType + "-----"
}

func chunk64(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i += 64 {
		end := i + 64
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
		b.WriteString("\n")
	}
	return b.String()
}
