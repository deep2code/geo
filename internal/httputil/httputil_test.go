package httputil

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]any{"ok": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestReadJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))
	var v struct{ A int `json:"a"` }
	if err := ReadJSON(r, &v); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if v.A != 1 {
		t.Fatalf("a = %d, want 1", v.A)
	}
}

func TestReadJSONEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	var v any
	if err := ReadJSON(r, &v); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("err = %v, want 空请求体错误", err)
	}
}

func TestReadJSONTooLarge(t *testing.T) {
	body := strings.Repeat("x", 2048)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var v any
	if err := ReadJSONLimit(r, &v, 1024); err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("err = %v, want 超限错误", err)
	}
}

func TestReadJSONInvalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{bad json`))
	var v any
	if err := ReadJSON(r, &v); err == nil || !strings.Contains(err.Error(), "解析失败") {
		t.Fatalf("err = %v, want 解析错误", err)
	}
}

func TestStripPort(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"1.2.3.4":      "1.2.3.4",
		"1.2.3.4:8080": "1.2.3.4",
		"[::1]:8080":   "::1",
		"[::1]":        "::1",
		"::1":          "::1",
	}
	for in, want := range cases {
		if got := StripPort(in); got != want {
			t.Errorf("StripPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	t.Setenv("GEO_TRUSTED_PROXIES", "127.0.0.1/32,10.0.0.0/8")

	// RemoteAddr 非可信 → 忽略转发头
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.5:9999"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(r); got != "203.0.113.5" {
		t.Errorf("非可信 RemoteAddr: got %q", got)
	}

	// RemoteAddr 可信 → 取 XFF 第一个非可信
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	if got := ClientIP(r); got != "1.2.3.4" {
		t.Errorf("XFF: got %q, want 1.2.3.4", got)
	}

	// XFF 全可信 → 取第一个
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if got := ClientIP(r); got != "10.0.0.1" {
		t.Errorf("XFF 全可信: got %q, want 10.0.0.1", got)
	}

	// 无 XFF → X-Real-IP
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	r.Header.Set("X-Real-IP", "203.0.113.9")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Errorf("X-Real-IP: got %q", got)
	}

	// 无转发头 → RemoteAddr
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	if got := ClientIP(r); got != "127.0.0.1" {
		t.Errorf("无转发头: got %q", got)
	}
}

func TestPageLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=3&limit=50", nil)
	page, limit := PageLimit(r, 20, 100)
	if page != 3 || limit != 50 {
		t.Fatalf("got (%d,%d), want (3,50)", page, limit)
	}

	r = httptest.NewRequest(http.MethodGet, "/?page=0&limit=-5", nil)
	page, limit = PageLimit(r, 20, 100)
	if page != 1 || limit != 20 {
		t.Fatalf("非法值回落: got (%d,%d), want (1,20)", page, limit)
	}

	r = httptest.NewRequest(http.MethodGet, "/?page=abc&limit=999", nil)
	page, limit = PageLimit(r, 20, 100)
	if page != 1 || limit != 100 {
		t.Fatalf("上限截断: got (%d,%d), want (1,100)", page, limit)
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	page, limit = PageLimit(r, 20, 100)
	if page != 1 || limit != 20 {
		t.Fatalf("缺省: got (%d,%d), want (1,20)", page, limit)
	}
}

func TestOffsetLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?offset=40&limit=10", nil)
	offset, limit := OffsetLimit(r, 20, 100)
	if offset != 40 || limit != 10 {
		t.Fatalf("got (%d,%d), want (40,10)", offset, limit)
	}

	// page 兼容换算
	r = httptest.NewRequest(http.MethodGet, "/?page=3&limit=10", nil)
	offset, limit = OffsetLimit(r, 20, 100)
	if offset != 20 || limit != 10 {
		t.Fatalf("page 换算: got (%d,%d), want (20,10)", offset, limit)
	}

	// offset 非法 → 回落 0；limit 超限截断
	r = httptest.NewRequest(http.MethodGet, "/?offset=-1&limit=9999", nil)
	offset, limit = OffsetLimit(r, 20, 100)
	if offset != 0 || limit != 100 {
		t.Fatalf("回落: got (%d,%d), want (0,100)", offset, limit)
	}
}

// 确保 ReadJSON 不会静默截断超限请求体（回归防护）。
func TestReadJSONNoSilentTruncation(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 64) // 不是合法 JSON，但必须报"过大"而非"解析失败"
	r := &http.Request{Body: io.NopCloser(bytes.NewReader(body))}
	var v any
	err := ReadJSONLimit(r, &v, 32)
	if err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("err = %v, want 超限错误", err)
	}
}
