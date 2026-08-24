package payment

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"my-geo/internal/config"
)

// paymentHTTPClient 支付渠道共享的带超时 HTTP 客户端。
// http.DefaultClient 无超时，上游挂起时会永久占用 handler goroutine。
var paymentHTTPClient = &http.Client{Timeout: 30 * time.Second}

// WeChatPayProvider 微信支付 v3 渠道（国内）。纯标准库实现。
//
// 凭据（缺任一项则渠道未启用）：
//   - GEO_WXPAY_MCH_ID        商户号
//   - GEO_WXPAY_APP_ID        应用 AppID
//   - GEO_WXPAY_API_V3_KEY     APIv3 密钥（32 字节，用于回调报文解密）
//   - GEO_WXPAY_SERIAL         商户 API 证书序列号
//   - GEO_WXPAY_PRIVATE_KEY    商户 API 私钥（PEM，PKCS1 或 PKCS8）
//   - GEO_WXPAY_WECHAT_PUBLIC_CERT 微信平台公钥（PEM，可选；用于回调签名验真）
type WeChatPayProvider struct {
	mchID      string
	appID      string
	apiV3Key   []byte
	serialNo   string
	privateKey *rsa.PrivateKey
	platCert   *rsa.PublicKey // 可选
}

func init() {
	register("wechatpay", func() Provider {
		mch := envOrEmpty("GEO_WXPAY_MCH_ID")
		app := envOrEmpty("GEO_WXPAY_APP_ID")
		v3 := envOrEmpty("GEO_WXPAY_API_V3_KEY")
		serial := envOrEmpty("GEO_WXPAY_SERIAL")
		privPEM := envOrEmpty("GEO_WXPAY_PRIVATE_KEY")
		if mch == "" || app == "" || v3 == "" || serial == "" || privPEM == "" {
			return nil
		}
		priv, err := parseRSAPrivateKey(privPEM)
		if err != nil {
			return nil
		}
		p := &WeChatPayProvider{
			mchID:      mch,
			appID:      app,
			apiV3Key:   []byte(v3),
			serialNo:   serial,
			privateKey: priv,
		}
		if certPEM := envOrEmpty("GEO_WXPAY_WECHAT_PUBLIC_CERT"); certPEM != "" {
			if pub, err := parseRSAPublicKey(certPEM); err == nil {
				p.platCert = pub
			} else {
				slog.Warn("wechatpay: 平台证书解析失败，回调验签将被跳过", slog.Any("error", err))
			}
		} else {
			slog.Warn("wechatpay: 未配置 GEO_WXPAY_WECHAT_PUBLIC_CERT，回调平台证书验签将被跳过（建议配置以满足微信官方安全要求）")
		}
		return p
	})
}

// Name 渠道标识。
func (p *WeChatPayProvider) Name() string { return "wechatpay" }

// CreateCheckout 创建 Native 支付（返回 code_url 供前端生成二维码）。
func (p *WeChatPayProvider) CreateCheckout(ctx context.Context, o Order, _ string) (*CheckoutResult, error) {
	body := map[string]any{
		"mchid":        p.mchID,
		"appid":        p.appID,
		"description":  "GEO " + o.Plan + " 订阅",
		"out_trade_no": o.ID,
		"notify_url":   config.Env("GEO_WXPAY_NOTIFY_URL", ""),
		"amount": map[string]any{
			"total":    o.AmountCents,
			"currency": "CNY",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.mch.weixin.qq.com/v3/pay/transactions/native", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	if err := p.signRequest(req, payload); err != nil {
		return nil, err
	}
	resp, err := paymentHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wechatpay: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wechatpay: 读取响应失败: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("wechatpay: HTTP %d: %s", resp.StatusCode, string(data))
	}
	var parsed struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("wechatpay: 解析响应失败: %w", err)
	}
	if parsed.CodeURL == "" {
		return nil, fmt.Errorf("wechatpay: 响应缺少 code_url")
	}
	return &CheckoutResult{
		URL:             parsed.CodeURL,
		ProviderOrderID: o.ID,
		Raw:             map[string]any{"code_url": parsed.CodeURL},
	}, nil
}

// signRequest 为请求注入微信 v3 鉴权头（RSA-SHA256 签名）。
func (p *WeChatPayProvider) signRequest(req *http.Request, body []byte) error {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomNonce()
	msg := strings.Join([]string{req.Method, req.URL.Path, ts, nonce, string(body)}, "\n") + "\n"
	sig, err := signSortedString(msg, p.privateKey)
	if err != nil {
		return err
	}
	auth := fmt.Sprintf("WECHATPAY2-SHA256-RSA2048 mchid=\"%s\",nonce_str=\"%s\",signature=\"%s\",timestamp=\"%s\",serial_no=\"%s\"",
		p.mchID, nonce, sig, ts, p.serialNo)
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

// VerifyWebhook 校验并解密微信支付 v3 回调。
// 回调体：{id, resource:{ciphertext, nonce, associated_data, original_type}}，
// ciphertext 用 APIv3 密钥做 AES-256-GCM 解密，得到含 out_trade_no / trade_state 的明文。
func (p *WeChatPayProvider) VerifyWebhook(r *http.Request, body []byte) (*WebhookEvent, error) {
	ts := r.Header.Get("Wechatpay-Timestamp")
	if ts == "" {
		return nil, fmt.Errorf("wechatpay: 缺少 Wechatpay-Timestamp 头")
	}
	tsUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("wechatpay: 时间戳格式非法")
	}
	if diff := time.Now().Unix() - tsUnix; diff < -300 || diff > 300 {
		return nil, fmt.Errorf("wechatpay: 回调时间戳已过期")
	}

	// 可选：微信平台证书验签（提升安全性；未配置证书时跳过，但记录警告）。
	if p.platCert != nil {
		if sig := r.Header.Get("Wechatpay-Signature"); sig != "" {
			nonce := r.Header.Get("Wechatpay-Nonce")
			msg := strings.Join([]string{ts, nonce, string(body)}, "\n") + "\n"
			if !verifyWeChatSignature(msg, sig, p.platCert) {
				return nil, fmt.Errorf("wechatpay: 平台证书签名校验失败")
			}
		}
	} else {
		slog.Warn("wechatpay: 未配置平台证书，跳过平台签名校验（生产环境建议配置 GEO_WECHATPAY_PLAT_CERT 以提升安全性）")
	}
	var cb struct {
		Resource struct {
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &cb); err != nil {
		return nil, fmt.Errorf("wechatpay: 解析回调失败: %w", err)
	}
	plain, err := p.decryptResource(cb.Resource.Ciphertext, cb.Resource.Nonce, cb.Resource.AssociatedData)
	if err != nil {
		return nil, err
	}
	var tx struct {
		OutTradeNo string `json:"out_trade_no"`
		TradeState string `json:"trade_state"`
	}
	if err := json.Unmarshal(plain, &tx); err != nil {
		return nil, fmt.Errorf("wechatpay: 解析明文失败: %w", err)
	}
	status := "pending"
	if tx.TradeState == "SUCCESS" {
		status = "paid"
	}
	return &WebhookEvent{
		Provider:        "wechatpay",
		OrderID:         tx.OutTradeNo,
		ProviderOrderID: tx.OutTradeNo,
		Status:          status,
		RawBody:         string(plain),
	}, nil
}

// decryptResource AES-256-GCM 解密微信回调密文。
func (p *WeChatPayProvider) decryptResource(ciphertextB64, nonce, aad string) ([]byte, error) {
	if len(p.apiV3Key) != 32 {
		return nil, fmt.Errorf("wechatpay: APIv3 密钥长度非法（应为 32 字节），实际 %d", len(p.apiV3Key))
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("wechatpay: 密文 base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(p.apiV3Key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceBytes := []byte(nonce)
	if len(nonceBytes) != gcm.NonceSize() {
		return nil, fmt.Errorf("wechatpay: nonce 长度非法")
	}
	var aadBytes []byte
	if aad != "" {
		aadBytes = []byte(aad)
	}
	plain, err := gcm.Open(nil, nonceBytes, ct, aadBytes)
	if err != nil {
		return nil, fmt.Errorf("wechatpay: GCM 解密失败（密钥或密文错误）: %w", err)
	}
	return plain, nil
}

// verifyWeChatSignature 用微信平台公钥验签（RSA-SHA256）。
func verifyWeChatSignature(msg, sigB64 string, cert *rsa.PublicKey) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	h := sha256.Sum256([]byte(msg))
	return rsa.VerifyPKCS1v15(cert, crypto.SHA256, h[:], sig) == nil
}

// randomNonce 生成 16 字节十六进制随机串（微信要求）。
func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// SignWeChatRequest 计算微信 v3 请求签名（供单测与本地联调复现）。
// 签名串为：METHOD\nURL\nTIMESTAMP\nNONCE\nBODY\n
func SignWeChatRequest(method, urlPath, timestamp, nonce, body string, key *rsa.PrivateKey) (string, error) {
	msg := strings.Join([]string{method, urlPath, timestamp, nonce, body}, "\n") + "\n"
	return signSortedString(msg, key)
}

// DecryptWeChatResource 导出 AES-GCM 解密（供单测：用已知 apiV3Key 加解密往返）。
func DecryptWeChatResource(apiV3Key, ciphertextB64, nonce, aad string) ([]byte, error) {
	p := &WeChatPayProvider{apiV3Key: []byte(apiV3Key)}
	return p.decryptResource(ciphertextB64, nonce, aad)
}
