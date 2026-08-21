package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("写 .env 失败: %v", err)
	}
	return path
}

func TestLoadDotEnv(t *testing.T) {
	path := writeEnvFile(t, `
# 注释行
GEO_LLM_KEY=sk-test-123456
export GEO_LLM_BASE="https://api.example.com/v1"
GEO_LLM_MODEL='qwen-plus'
GEO_PORT=9999 # 行内注释
EMPTY_VALUE=
BAD_LINE_NO_EQUALS
`)
	// 预置同名环境变量，验证"已存在优先、不覆盖"
	t.Setenv("GEO_LLM_KEY", "sk-keep-me")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	cases := map[string]string{
		"GEO_LLM_KEY":    "sk-keep-me", // 已存在 → 不覆盖
		"GEO_LLM_BASE":   "https://api.example.com/v1",
		"GEO_LLM_MODEL":  "qwen-plus",
		"GEO_PORT":       "9999",
		"EMPTY_VALUE":    "", // 空值也设置（取不到时为空串，不影响）
		"BAD_LINE_NO_EQUALS": "",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "not-exist.env")); err != nil {
		t.Fatalf("文件不存在应返回 nil, got %v", err)
	}
}

func TestValidateAuthEnabledRequiresDSN(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "true")
	t.Setenv("GEO_MYSQL_DSN", "")
	t.Setenv("GEO_JWT_SECRET", "")
	t.Setenv("GEO_MCP_API_KEY", "")
	err := Validate()
	if err == nil || !strings.Contains(err.Error(), "GEO_MYSQL_DSN") {
		t.Fatalf("应报缺少 DSN 错误, got %v", err)
	}
}

func TestValidateWeakJWTSecret(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "false")
	t.Setenv("GEO_JWT_SECRET", "short")
	t.Setenv("GEO_MCP_API_KEY", "")
	err := Validate()
	if err == nil || !strings.Contains(err.Error(), "GEO_JWT_SECRET") {
		t.Fatalf("应报弱 JWT secret 错误, got %v", err)
	}
}

func TestValidateWeakMCPKey(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "false")
	t.Setenv("GEO_JWT_SECRET", "")
	t.Setenv("GEO_MCP_API_KEY", "12345")
	err := Validate()
	if err == nil || !strings.Contains(err.Error(), "GEO_MCP_API_KEY") {
		t.Fatalf("应报弱 MCP key 错误, got %v", err)
	}
}

func TestValidateOK(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "false")
	t.Setenv("GEO_JWT_SECRET", "")
	t.Setenv("GEO_MCP_API_KEY", "")
	if err := Validate(); err != nil {
		t.Fatalf("默认配置应通过校验, got %v", err)
	}
}

func TestValidateStrongSecretOK(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "true")
	t.Setenv("GEO_MYSQL_DSN", "geo:pass@tcp(127.0.0.1:3306)/geo")
	t.Setenv("GEO_JWT_SECRET", strings.Repeat("s", 40))
	t.Setenv("GEO_MCP_API_KEY", strings.Repeat("k", 20))
	if err := Validate(); err != nil {
		t.Fatalf("强密钥 + DSN 应通过校验, got %v", err)
	}
}

func TestUnquote(t *testing.T) {
	cases := map[string]string{
		`"abc"`:    "abc",
		`'abc'`:    "abc",
		`abc`:      "abc",
		`"a"b"`:    `a"b`,
		`""`:       "",
	}
	for in, want := range cases {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}
