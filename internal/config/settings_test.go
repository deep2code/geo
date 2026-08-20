package config

import (
	"os"
	"strings"
	"testing"
)

func TestEnvFallbackOrder_NoDB(t *testing.T) {
	// 未初始化 DB 时：环境变量 > 默认值（与旧版行为一致）
	const k = "GEO_TEST_ORDER"
	os.Unsetenv(k)
	defer os.Unsetenv(k)
	if got := Env(k, "fallback"); got != "fallback" {
		t.Fatalf("无 env 时应返回 fallback，得到 %q", got)
	}
	os.Setenv(k, "envvalue")
	if got := Env(k, "fallback"); got != "envvalue" {
		t.Fatalf("env 优先于 fallback，得到 %q", got)
	}
}

func TestListSettingsSource(t *testing.T) {
	const k = "GEO_TEST_SOURCE"
	os.Unsetenv(k)
	defer os.Unsetenv(k)
	items := ListSettings()
	for _, it := range items {
		if it.Key == k {
			t.Fatalf("未知 key 不应出现在 catalog 中: %s", k)
		}
	}
	// 已登记的 key：env 优先
	os.Setenv("GEO_ALLOW_REGISTER", "true")
	items = ListSettings()
	found := false
	for _, it := range items {
		if it.Key == "GEO_ALLOW_REGISTER" {
			found = true
			if it.Source != "env" || it.Value != "true" {
				t.Fatalf("env 值应标记 source=env，得到 %s/%s", it.Source, it.Value)
			}
		}
	}
	if !found {
		t.Fatal("catalog 缺少 GEO_ALLOW_REGISTER")
	}
}

func TestCatalogUniqueAndBootstrap(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range settings.catalog {
		if seen[s.Key] {
			t.Fatalf("catalog 存在重复 key: %s", s.Key)
		}
		seen[s.Key] = true
	}
	// 例外清单（用户原则）：数据库连接 DSN + 初始管理员账号，其余全部放配置表。
	for _, k := range []string{"GEO_AUTH_MYSQL_DSN", "GEO_HISTORY_MYSQL_DSN", "GEO_ADMIN_EMAIL", "GEO_ADMIN_PASSWORD"} {
		if !seen[k] {
			t.Fatalf("catalog 缺少引导变量 %s", k)
		}
	}
	// 引导项必须同时在 catalog 中标记 IsBootstrap（管理后台只读）
	for _, s := range settings.catalog {
		if isBootstrapKey(s.Key) && !s.IsBootstrap {
			t.Fatalf("%s 应标记 IsBootstrap（例外变量管理后台只读）", s.Key)
		}
	}
	// JWT_SECRET 非例外：不应 bootstrap，管理后台可改（需重启）
	for _, s := range settings.catalog {
		if s.Key == "GEO_JWT_SECRET" {
			if s.IsBootstrap {
				t.Fatal("GEO_JWT_SECRET 不应是 bootstrap（非例外，放配置表）")
			}
			if !s.RequiresRestart {
				t.Fatal("GEO_JWT_SECRET 应标注 RequiresRestart")
			}
		}
	}
}

func isBootstrapKey(k string) bool {
	switch k {
	case "GEO_ADMIN_EMAIL", "GEO_ADMIN_PASSWORD":
		return true
	}
	return strings.HasSuffix(k, "_MYSQL_DSN") || strings.HasSuffix(k, "_MYSQL_DB")
}

func TestEnvEngineKeysPresent(t *testing.T) {
	// 引擎 key 应全部登记在 catalog（管理后台可改）
	for _, k := range []string{"GEO_OPENAI_KEY", "GEO_DEEPSEEK_KEY", "GEO_DOUBAO_KEY", "GEO_ERNIE_KEY"} {
		found := false
		for _, s := range settings.catalog {
			if s.Key == k {
				found = true
				if !s.IsSecret {
					t.Fatalf("%s 应标记 secret", k)
				}
			}
		}
		if !found {
			t.Fatalf("catalog 缺少引擎 key %s", k)
		}
	}
}

func TestWebSearchConfigKeys(t *testing.T) {
	// webSearchEnvKey 推导
	if got := webSearchEnvKey("GEO_OPENAI_KEY"); got != "GEO_OPENAI_WEB_SEARCH" {
		t.Fatalf("webSearchEnvKey(GEO_OPENAI_KEY) = %s", got)
	}
	if got := webSearchEnvKey("GEO_DEEPSEEK_KEY"); got != "GEO_DEEPSEEK_WEB_SEARCH" {
		t.Fatalf("webSearchEnvKey(GEO_DEEPSEEK_KEY) = %s", got)
	}
	// catalog 已登记各引擎 WEB_SEARCH 项
	for _, k := range []string{"GEO_OPENAI_WEB_SEARCH", "GEO_GEMINI_WEB_SEARCH", "GEO_CLAUDE_WEB_SEARCH",
		"GEO_DEEPSEEK_WEB_SEARCH", "GEO_QWEN_WEB_SEARCH", "GEO_KIMI_WEB_SEARCH", "GEO_GLM_WEB_SEARCH"} {
		found := false
		for _, s := range settings.catalog {
			if s.Key == k {
				found = true
				if s.Type != "bool" {
					t.Fatalf("%s 类型应为 bool", k)
				}
			}
		}
		if !found {
			t.Fatalf("catalog 缺少 %s", k)
		}
	}
	// envBool 解析
	os.Unsetenv("GEO_TEST_WEB")
	if !envBool("GEO_TEST_WEB", true) {
		t.Fatal("默认值 true 应生效")
	}
	os.Setenv("GEO_TEST_WEB", "false")
	if envBool("GEO_TEST_WEB", true) {
		t.Fatal("false 应覆盖默认")
	}
	os.Unsetenv("GEO_TEST_WEB")
}
