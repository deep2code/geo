package config

import (
	"os"
	"strings"
	"testing"
)

func TestEnvIgnoresEnvWithoutDB(t *testing.T) {
	// 未初始化 DB 时：非引导项只读 DB（初始化后才有效），环境变量不再参与
	const k = "GEO_TEST_ORDER"
	os.Unsetenv(k)
	defer os.Unsetenv(k)
	if got := Env(k, "fallback"); got != "fallback" {
		t.Fatalf("无 DB 且无值时应返回 fallback，得到 %q", got)
	}
	os.Setenv(k, "envvalue")
	if got := Env(k, "fallback"); got != "fallback" {
		t.Fatalf("非引导项应忽略环境变量（只读 DB），得到 %q", got)
	}
}

func TestEnvBootstrapReadsEnv(t *testing.T) {
	// 引导类（数据库连接/管理员/AUTH/JWT）：环境变量仍生效（避免后台未开启时死锁）
	os.Setenv("GEO_AUTH_ENABLED", "true")
	defer os.Unsetenv("GEO_AUTH_ENABLED")
	if got := Env("GEO_AUTH_ENABLED", "false"); got != "true" {
		t.Fatalf("引导项应读环境变量，得到 %q", got)
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
	// 已登记的 key：无 DB 时非引导项来源为 default（不读环境变量）
	os.Setenv("GEO_ALLOW_REGISTER", "true")
	items = ListSettings()
	found := false
	for _, it := range items {
		if it.Key == "GEO_ALLOW_REGISTER" {
			found = true
			if it.Source != "default" || it.Value != "false" {
				t.Fatalf("非引导项应忽略环境变量显示默认值，得到 %s/%s", it.Source, it.Value)
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
	for _, k := range []string{"GEO_MYSQL_DSN", "GEO_ADMIN_EMAIL", "GEO_ADMIN_PASSWORD"} {
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
	// JWT_SECRET / AUTH_ENABLED 为引导类（2026-08-21 起：运行参数只读 DB 后，
	// 账号体系开关与签名密钥必须走环境变量引导，否则未启用 AUTH 时后台 403 无法从 DB 开启）
	for _, s := range settings.catalog {
		if s.Key == "GEO_JWT_SECRET" {
			if !s.IsBootstrap {
				t.Fatal("GEO_JWT_SECRET 应为引导类（IsBootstrap=true，环境变量引导）")
			}
			if !s.RequiresRestart {
				t.Fatal("GEO_JWT_SECRET 应标注 RequiresRestart")
			}
		}
		if s.Key == "GEO_AUTH_ENABLED" && !s.IsBootstrap {
			t.Fatal("GEO_AUTH_ENABLED 应为引导类（IsBootstrap=true，环境变量引导）")
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
	// envBool 解析：非引导项只读 DB（无 DB 时用默认值；DB 覆盖生效）
	os.Unsetenv("GEO_TEST_WEB")
	if !envBool("GEO_TEST_WEB", true) {
		t.Fatal("默认值 true 应生效")
	}
	// 模拟 DB 覆盖（同包注入 overrides + loaded）
	settings.mu.Lock()
	settings.loaded = true
	settings.overrides["GEO_TEST_WEB"] = "false"
	settings.mu.Unlock()
	defer func() {
		settings.mu.Lock()
		settings.loaded = false
		delete(settings.overrides, "GEO_TEST_WEB")
		settings.mu.Unlock()
	}()
	if envBool("GEO_TEST_WEB", true) {
		t.Fatal("DB 覆盖 false 应生效")
	}
	os.Unsetenv("GEO_TEST_WEB")
}
