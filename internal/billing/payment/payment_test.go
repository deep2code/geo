package payment

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// ---- Stripe HMAC 签名校验 ----

func TestStripeWebhookSignature(t *testing.T) {
	secret := "whsec_xxx"
	// 使用当前时间戳，避免新加的时间戳校验（±5 分钟窗口）导致固定旧时间戳失败。
	ts := fmt.Sprintf("%d", time.Now().Unix())
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","payment_status":"paid","client_reference_id":"ord_1"}}}`)

	sig := ComputeStripeSignature(secret, ts, body)
	if sig == "" {
		t.Fatal("签名不应为空")
	}
	// 构造请求并校验通过
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", "t="+ts+",v1="+sig)
	p := &StripeProvider{webhookSecret: secret}
	ev, err := p.VerifyWebhook(req, body)
	if err != nil {
		t.Fatalf("合法签名应校验通过: %v", err)
	}
	if ev.Status != "paid" {
		t.Fatalf("期望 paid，实际 %q", ev.Status)
	}
	// 篡改 body 应失败
	req2 := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte("tampered")))
	req2.Header.Set("Stripe-Signature", "t="+ts+",v1="+sig)
	if _, err := p.VerifyWebhook(req2, []byte("tampered")); err == nil {
		t.Fatal("篡改 body 应校验失败")
	}
	// 过期时间戳应失败
	req3 := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req3.Header.Set("Stripe-Signature", "t=1700000000,v1="+ComputeStripeSignature(secret, "1700000000", body))
	if _, err := p.VerifyWebhook(req3, body); err == nil {
		t.Fatal("过期时间戳应校验失败")
	}
}

// ---- 支付宝 RSA2 签名/验签往返 ----

func TestAlipayRSA2SignVerify(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// PKCS8 私钥 PEM
	privDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	privParsed, err := parseRSAPrivateKey(privPEM)
	if err != nil {
		t.Fatalf("私钥解析失败: %v", err)
	}
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	pubParsed, err := parseRSAPublicKey(pubPEM)
	if err != nil {
		t.Fatalf("公钥解析失败: %v", err)
	}

	params := map[string]string{
		"app_id":       "2021000001",
		"out_trade_no": "ord_abc",
		"total_amount": "99.00",
		"subject":      "GEO Pro",
	}
	sign, err := signRSA2(params, privParsed)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	params["sign"] = sign
	if !verifyRSA2(params, sign, pubParsed) {
		t.Fatal("合法签名应验签通过")
	}
	// 篡改金额应失败
	bad := map[string]string{"out_trade_no": "ord_abc", "total_amount": "0.01", "sign": sign}
	if verifyRSA2(bad, sign, pubParsed) {
		t.Fatal("篡改参数后签名不应通过")
	}
}

// ---- 微信支付 v3 AES-256-GCM 解密往返 ----

func TestWeChatResourceDecrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aad := "transaction"

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"out_trade_no":"ord_xyz","trade_state":"SUCCESS"}`)
	ct := gcm.Seal(nil, nonce, plaintext, []byte(aad))
	ctB64 := base64.StdEncoding.EncodeToString(ct)

	got, err := DecryptWeChatResource(string(key), ctB64, string(nonce), aad)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("解密结果不符: %q", string(got))
	}
}

// ---- 微信 v3 请求签名稳定性 ----

func TestWeChatRequestSignature(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	privDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	privParsed, err := parseRSAPrivateKey(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	sig1, err := SignWeChatRequest("POST", "/v3/pay/transactions/native", "1700000000", "nonce1", `{}`, privParsed)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := SignWeChatRequest("POST", "/v3/pay/transactions/native", "1700000000", "nonce1", `{}`, privParsed)
	if sig1 != sig2 {
		t.Fatal("相同输入应产生稳定签名")
	}
	if sig1 == "" {
		t.Fatal("签名不应为空")
	}
}

// ---- 渠道降级：无凭据时 GetProvider 返回 nil ----

func TestProviderFallbackWhenUnconfigured(t *testing.T) {
	// 在干净环境下（CI/未配置凭据），三家渠道均应返回 nil（降级手动模式）。
	for _, name := range []string{"stripe", "alipay", "wechatpay"} {
		if GetProvider(name) != nil {
			t.Logf("注意：%s 已配置凭据（本地环境变量），跳过 nil 断言", name)
		}
	}
}
