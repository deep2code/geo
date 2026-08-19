package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postRegister 构造一个 POST /api/v1/auth/register 请求并执行。
func postRegister(h HandlerSet, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Register(rec, req)
	return rec
}

// TestRegister_ClosedByDefault 注册通道默认关闭（GEO_ALLOW_REGISTER 缺省 false）。
// 无需数据库：AllowRegister=false 时在触碰 store 之前即返回 403。
func TestRegister_ClosedByDefault(t *testing.T) {
	svc := &Service{enabled: true} // store 为 nil 无妨：关闭分支不触碰 DB
	h := NewHandlerSet(svc)        // 未传 WithAllowRegister → 默认关闭
	rec := postRegister(h, `{"email":"a@b.com","password":"StrongPass1!"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("期望 403，实际 %d", rec.Code)
	}
	var resp AuthNResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Code != "REGISTRATION_CLOSED" {
		t.Fatalf("期望 REGISTRATION_CLOSED，实际 %q", resp.Code)
	}
}

// TestRegister_AllowRegisterTrue 开关打开时不返回"通道关闭"。
// 测试用 store=nil 的 Service：正常流程会走到 CreateUser（nil store 触发 panic），
// 用 recover 验证"开关确实放行到了 DB 层"而不是被 403 拦截。
func TestRegister_AllowRegisterTrue(t *testing.T) {
	svc := &Service{enabled: true}
	h := NewHandlerSet(svc, WithAllowRegister(true))
	var panicked bool
	func() {
		defer func() {
			if recover() != nil {
				panicked = true // 走到了 CreateUser（DB 层）→ 开关生效
			}
		}()
		rec := postRegister(h, `{"email":"a@b.com","password":"StrongPass1!"}`)
		if rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "REGISTRATION_CLOSED") {
			t.Fatal("AllowRegister=true 但仍返回 REGISTRATION_CLOSED")
		}
	}()
	if !panicked {
		t.Fatal("AllowRegister=true 未放行到 CreateUser（开关可能未生效）")
	}
}

// TestRegister_AuthDisabled 账号体系未启用 → 503。
func TestRegister_AuthDisabled(t *testing.T) {
	h := NewHandlerSet(nil) // Svc nil → 503 AUTH_DISABLED
	rec := postRegister(h, `{}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503，实际 %d", rec.Code)
	}
}
