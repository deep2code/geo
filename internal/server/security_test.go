package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSafeURL 校验白标/外链 URL 协议白名单：仅 http/https 放行，阻断
// javascript:/data:/ftp:/相对协议等可被用于 XSS 或 SSRF 的入口。
func TestSafeURL(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"https://example.com/a", true},
		{"http://example.com", true},
		{"HTTPS://Example.COM/x", true},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>", false},
		{"ftp://example.com", false},
		{"//evil.com/x", false}, // 无 scheme，被视为相对路径
		{"", false},
		{"   ", false},
	}
	for _, c := range cases {
		got, ok := safeURL(c.in)
		if ok != c.ok {
			t.Errorf("safeURL(%q) ok=%v, want %v", c.in, ok, c.ok)
		}
		if ok && got != strings.TrimSpace(c.in) {
			t.Errorf("safeURL(%q) 返回 %q, want %q", c.in, got, strings.TrimSpace(c.in))
		}
	}
}

// TestWriteInternalErrorNoLeak 验证内部错误响应只返回通用提示，
// 绝不泄露 DSN / 密码 / 主机 / 堆栈等实现细节（P0-6 脱敏要求）。
func TestWriteInternalErrorNoLeak(t *testing.T) {
	rec := httptest.NewRecorder()
	leaky := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused (user=root password=SuperSecret123)")
	writeInternalError(rec, leaky, "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var er ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(er.Error, "内部错误") {
		t.Errorf("缺少通用错误提示: %q", er.Error)
	}
	body := rec.Body.String()
	for _, leak := range []string{"SuperSecret123", "password=", "127.0.0.1:3306", "connection refused"} {
		if strings.Contains(body, leak) {
			t.Errorf("响应泄露了内部细节 %q: %s", leak, body)
		}
	}
}

// TestWriteInternalErrorWithMsg 验证带上下文 msg 时提示拼接正确。
func TestWriteInternalErrorWithMsg(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, errors.New("boom"), "生成报告")
	var er ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&er)
	if er.Error != "生成报告失败，请稍后重试" {
		t.Errorf("msg 拼接错误: %q", er.Error)
	}
}

// TestRequireDataAdminRequiresAuth 验证账号体系未启用（authSvc == nil）时，
// 数据管理类接口一律 403（无角色注入），不允许匿名全权访问。
func TestRequireDataAdminRequiresAuth(t *testing.T) {
	s := &Server{} // authSvc == nil → 账号体系未启用
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/data/clear", nil)
	if s.requireDataAdmin(rec, req) {
		t.Fatal("账号体系未启用时 requireDataAdmin 应拒绝")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "未登录") {
		t.Errorf("应提示未登录, 实际: %s", rec.Body.String())
	}
}
