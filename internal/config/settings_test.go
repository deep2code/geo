package config

import (
	"os"
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
		// bootstrap 必须同时是 secret（连接串/密钥类），避免误标
		if s.IsBootstrap && !s.IsSecret {
			t.Fatalf("bootstrap 项 %s 应为 secret 类", s.Key)
		}
	}
	// 引导变量清单核对
	for _, k := range []string{"GEO_AUTH_MYSQL_DSN", "GEO_JWT_SECRET"} {
		if !seen[k] {
			t.Fatalf("catalog 缺少引导变量 %s", k)
		}
	}
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
