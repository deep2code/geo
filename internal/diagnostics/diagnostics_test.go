package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"my-geo/internal/config"
	"my-geo/pkg/geo"
)

// newTestEngine 构建无 LLM 的测试引擎（业务探针走规则化路径，不依赖网络）。
func newTestEngine() *geo.Engine {
	return geo.New()
}

func TestParseMySQLDSN(t *testing.T) {
	host, port, err := parseMySQLDSN("geo:pass@tcp(127.0.0.1:3306)/geo_offline?parseTime=true")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if host != "127.0.0.1" || port != "3306" {
		t.Fatalf("解析结果错误: host=%q port=%q", host, port)
	}
	// 非法 DSN 应返回错误
	if _, _, err := parseMySQLDSN("not-a-dsn"); err == nil {
		t.Fatal("非法 DSN 应返回错误")
	}
}

func TestOverall(t *testing.T) {
	if got := Overall(nil); got != SeverityOK {
		t.Fatalf("空结果应为 ok，实际 %s", got)
	}
	if got := Overall([]CheckResult{{Status: SeverityInfo}, {Status: SeverityOK}}); got != SeverityOK {
		t.Fatalf("仅 info/ok 应为 ok，实际 %s", got)
	}
	if got := Overall([]CheckResult{{Status: SeverityOK}, {Status: SeverityWarn}}); got != SeverityWarn {
		t.Fatalf("含 warn 应为 warn，实际 %s", got)
	}
	if got := Overall([]CheckResult{{Status: SeverityWarn}, {Status: SeverityError}}); got != SeverityError {
		t.Fatalf("含 error 应为 error，实际 %s", got)
	}
}

func TestCountBySeverity(t *testing.T) {
	m := CountBySeverity([]CheckResult{
		{Status: SeverityOK}, {Status: SeverityOK},
		{Status: SeverityInfo}, {Status: SeverityWarn}, {Status: SeverityError},
	})
	if m[SeverityOK] != 2 || m[SeverityInfo] != 1 || m[SeverityWarn] != 1 || m[SeverityError] != 1 {
		t.Fatalf("计数错误: %+v", m)
	}
}

// TestBusinessHealthOffline 在无 LLM / 无 DB 环境下，评分/分析/优化探针应为 ok，
// LLM 探针应为 info（跳过），DB 探针应为 info（未配置 DSN → 内置默认）或 warn。
func TestBusinessHealthOffline(t *testing.T) {
	engine := newTestEngine()
	results := BusinessHealth(context.Background(), engine)
	if len(results) == 0 {
		t.Fatal("业务探针不应为空")
	}
	byName := map[string]CheckResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	for _, name := range []string{"评分管线", "内容分析管线", "优化管线"} {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("缺少业务探针 %q", name)
		}
		if r.Status == SeverityError {
			t.Fatalf("业务探针 %q 不应为 error（离线环境）: %s", name, r.Message)
		}
	}
	// LLM 未配置 → info 跳过
	if r := byName["LLM 改写业务"]; r.Status != SeverityInfo {
		t.Fatalf("无 LLM 时应跳过(info)，实际 %s", r.Status)
	}
}

func TestConfigCheckDefault(t *testing.T) {
	// 清空相关环境变量，验证默认态不产生 error。
	clearEnvForConfig()
	results := ConfigCheck("")
	byName := map[string]CheckResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if r, ok := byName["账号体系/鉴权配置"]; ok && r.Status == SeverityError {
		t.Fatalf("默认无 auth 配置不应 error: %s", r.Detail)
	}
	// 合法白标色校验通过
	os.Setenv("GEO_WL_PRIMARY_COLOR", "#3B82F6")
	withColor := ConfigCheck("")
	var found bool
	for _, r := range withColor {
		if r.Name == "白标主题色" {
			found = true
			if r.Status == SeverityError || r.Status == SeverityWarn {
				t.Fatalf("合法 hex 颜色不应告警: %s", r.Message)
			}
		}
	}
	if !found {
		t.Fatal("缺少白标主题色检查")
	}
}

func TestConfigCheckBadPort(t *testing.T) {
	clearEnvForConfig()
	os.Setenv("GEO_PORT", "not-a-port")
	results := ConfigCheck("")
	for _, r := range results {
		if r.Name == "服务端口" && r.Status != SeverityWarn {
			t.Fatalf("非法端口应为 warn，实际 %s", r.Status)
		}
	}
}

func TestConfigCheckRuleset(t *testing.T) {
	// 写入一个合法规则集临时文件
	dir := t.TempDir()
	path := dir + "/rules.json"
	rs := config.DefaultRuleSet()
	b, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	results := ConfigCheck(path)
	var ok bool
	for _, r := range results {
		if r.Name == "外部规则集" {
			ok = true
			if r.Status == SeverityError {
				t.Fatalf("合法规则集不应 error: %s", r.Detail)
			}
		}
	}
	if !ok {
		t.Fatal("缺少外部规则集检查")
	}
}

func TestSelfCheckRender(t *testing.T) {
	engine := newTestEngine()
	report := SelfCheck(context.Background(), engine, "")
	if report.Overall == "" {
		t.Fatal("总体状态不应为空")
	}
	if report.Runtime.GoVersion == "" {
		t.Fatal("运行时信息不应为空")
	}
	// text 渲染不报错
	var sb strings.Builder
	report.RenderText(&sb)
	if !strings.Contains(sb.String(), "系统自检") {
		t.Fatal("text 渲染缺少标题")
	}
	// json 渲染可解析且含 overall
	b, err := report.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	var again SelfCheckReport
	if err := json.Unmarshal(b, &again); err != nil {
		t.Fatal(err)
	}
	if again.Overall != report.Overall {
		t.Fatal("json 往返不一致")
	}
}

// ---- 测试辅助 ----

func clearEnvForConfig() {
	for _, k := range []string{
		"GEO_LOG_LEVEL", "GEO_LOG_FORMAT", "GEO_PORT", "GEO_LLM_BUDGET_USD",
		"GEO_AUTH_ENABLED", "GEO_AUTH_MYSQL_DSN", "GEO_JWT_SECRET", "GEO_MCP_API_KEY",
		"GEO_ADMIN_KEY", "GEO_LLM_KEY", "GEO_LLM_BASE", "GEO_LLM_MODEL",
		"GEO_WL_PRIMARY_COLOR", "GEO_SCHEDULER_ENABLED", "GEO_SCHEDULER_CONFIG",
		"GEO_OFFLINE_DB_ENABLED", "GEO_HISTORY_DB_ENABLED", "GEO_CHINACHECK_CACHE_ENABLED",
	} {
		os.Unsetenv(k)
	}
	// 清空所有引擎 Key 环境变量
	for _, env := range config.AllEngineEnvKeys() {
		os.Unsetenv(env)
	}
}
