package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"my-geo/internal/config"
	"my-geo/internal/dbprovider"
)

// validLogLevels / validLogFormats 合法的日志配置取值。
var (
	validLogLevels   = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validLogFormats  = map[string]bool{"text": true, "json": true}
	hexColorRe       = regexp.MustCompile(`^#?[0-9a-fA-F]{3,8}$`)
	moduleDSNEnabled = []struct {
		mod dbprovider.ModuleKind
	}{
		{mod: dbprovider.ModuleOfflineCompanies},
		{mod: dbprovider.ModuleAuditHistory},
		{mod: dbprovider.ModuleChinaCheckCache},
	}
)

// ConfigCheck 对属性、参数、环境变量、DSN、规则集等做静态校验，返回问题清单。
//
// rulesPath 为可选的外部规则集文件路径（来自 --rules / GEO_RULES），非空时额外校验其可加载且合法。
// 本函数只读环境变量与文件，不修改任何状态。
func ConfigCheck(rulesPath string) []CheckResult {
	results := make([]CheckResult, 0, 16)
	results = append(results, checkLogLevel())
	results = append(results, checkLogFormat())
	results = append(results, checkPort())
	results = append(results, checkBudget())
	results = append(results, checkAuthBoot())
	results = append(results, checkAuthEnabled())
	results = append(results, checkLLMConfig())
	results = append(results, checkEngineKeys())
	results = append(results, checkDSNs())
	results = append(results, checkWhitelabel())
	results = append(results, checkSchedulerConfig())
	results = append(results, checkRuleset(rulesPath))
	return results
}

// checkLogLevel 校验 GEO_LOG_LEVEL 取值。
func checkLogLevel() CheckResult {
	res := CheckResult{Name: "日志级别", Category: CategoryConfig}
	v := strings.TrimSpace(config.Env("GEO_LOG_LEVEL", ""))
	if v == "" {
		res.Status = SeverityInfo
		res.Message = "未设置，使用默认 info"
		return res
	}
	if !validLogLevels[strings.ToLower(v)] {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("GEO_LOG_LEVEL=%q 非法（应为 debug/info/warn/error）", v)
		return res
	}
	res.Status = SeverityOK
	res.Message = "合法：" + v
	return res
}

// checkLogFormat 校验 GEO_LOG_FORMAT 取值。
func checkLogFormat() CheckResult {
	res := CheckResult{Name: "日志格式", Category: CategoryConfig}
	v := strings.TrimSpace(config.Env("GEO_LOG_FORMAT", ""))
	if v == "" {
		res.Status = SeverityInfo
		res.Message = "未设置，使用默认 text（K8s 环境自动 json）"
		return res
	}
	if !validLogFormats[strings.ToLower(v)] {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("GEO_LOG_FORMAT=%q 非法（应为 text/json）", v)
		return res
	}
	res.Status = SeverityOK
	res.Message = "合法：" + v
	return res
}

// checkPort 校验服务端口可解析且在合法范围。
func checkPort() CheckResult {
	res := CheckResult{Name: "服务端口", Category: CategoryConfig}
	v := strings.TrimSpace(config.Env("GEO_PORT", ""))
	if v == "" {
		res.Status = SeverityInfo
		res.Message = "未设置，使用默认 8080"
		return res
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("GEO_PORT=%q 非法（应为 1–65535 的整数）", v)
		return res
	}
	res.Status = SeverityOK
	res.Message = "合法：" + v
	return res
}

// checkBudget 校验月度 LLM 预算可解析且非负。
func checkBudget() CheckResult {
	res := CheckResult{Name: "LLM 月度预算", Category: CategoryConfig}
	v := strings.TrimSpace(config.Env("GEO_LLM_BUDGET_USD", ""))
	if v == "" {
		res.Status = SeverityInfo
		res.Message = "未设置（不启用预算熔断）"
		return res
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("GEO_LLM_BUDGET_USD=%q 非法（应为非负浮点数）", v)
		return res
	}
	res.Status = SeverityOK
	res.Message = fmt.Sprintf("合法：%.2f USD", f)
	return res
}

// checkAuthBoot 复用启动期 fail-fast 校验（弱密钥/缺 DSN 等）。
func checkAuthBoot() CheckResult {
	res := CheckResult{Name: "账号体系/鉴权配置", Category: CategoryConfig}
	if err := config.Validate(); err != nil {
		res.Status = SeverityError
		res.Message = "鉴权配置未通过启动校验"
		res.Detail = err.Error()
		return res
	}
	res.Status = SeverityOK
	res.Message = "通过（auth/JWT/MCP Key 强度校验）"
	return res
}

// checkAuthEnabled 检查账号体系（JWT）是否启用；未启用时管理接口全部 403。
func checkAuthEnabled() CheckResult {
	res := CheckResult{Name: "账号体系（JWT）", Category: CategoryConfig}
	enabled := strings.EqualFold(strings.TrimSpace(config.Env("GEO_AUTH_ENABLED", "")), "true")
	if !enabled {
		res.Status = SeverityWarn
		res.Message = "未启用账号体系（GEO_AUTH_ENABLED=true）：API 匿名放行、管理接口（/api/v1/admin/*）全部 403。生产部署请启用并配置 GEO_JWT_SECRET + GEO_ADMIN_EMAIL/PASSWORD"
		return res
	}
	res.Status = SeverityOK
	res.Message = "已启用（JWT + RBAC，管理接口按角色鉴权）"
	return res
}

// checkLLMConfig 检查 OpenAI 兼容 LLM 的 base/key 一致性。
func checkLLMConfig() CheckResult {
	res := CheckResult{Name: "LLM 基础配置", Category: CategoryConfig}
	key := strings.TrimSpace(config.Env("GEO_LLM_KEY", ""))
	base := strings.TrimSpace(config.Env("GEO_LLM_BASE", ""))
	if key == "" && base != "" {
		res.Status = SeverityWarn
		res.Message = "配置了 GEO_LLM_BASE 但未配置 GEO_LLM_KEY，LLM 仍不可用"
		return res
	}
	if key == "" {
		res.Status = SeverityInfo
		res.Message = "未配置 LLM Key（评测/改写依赖 LLM 的能力不可用）"
		return res
	}
	res.Status = SeverityOK
	res.Message = "已配置 LLM Key" + ternaryStr(base != "", "（含自定义 BaseURL）", "")
	return res
}

// checkEngineKeys 统计各生成式引擎 API Key 配置情况。
func checkEngineKeys() CheckResult {
	res := CheckResult{Name: "引擎 API Key", Category: CategoryConfig}
	keys := config.AllEngineEnvKeys()
	configured := 0
	var names []string
	for engine, envKey := range keys {
		if strings.TrimSpace(os.Getenv(envKey)) != "" {
			configured++
			names = append(names, string(engine))
		}
	}
	if configured == 0 && strings.TrimSpace(config.Env("GEO_LLM_KEY", "")) == "" {
		res.Status = SeverityWarn
		res.Message = "未配置任何引擎/LLM Key，品牌审计与智能补全将不可用（仅规则化与离线能力）"
		return res
	}
	res.Status = SeverityOK
	res.Message = fmt.Sprintf("已配置 %d 个引擎 Key：%s", configured, strings.Join(names, ", "))
	return res
}

// checkDSNs 校验各 MySQL 模块的 DSN 格式（仅当模块启用且配置了 DSN）。
func checkDSNs() CheckResult {
	res := CheckResult{Name: "数据库 DSN 格式", Category: CategoryConfig}
	var bad []string
	for _, m := range moduleDSNEnabled {
		mod := m.mod
		if !dbprovider.EnabledFor(mod) {
			continue
		}
		dsn := dbprovider.DSNFor(mod)
		if dsn == "" {
			continue
		}
		if _, _, err := parseMySQLDSN(dsn); err != nil {
			bad = append(bad, dbModuleName(mod))
		}
	}
	if len(bad) > 0 {
		res.Status = SeverityWarn
		res.Message = "以下模块 DSN 格式异常，可能无法连接：" + strings.Join(bad, ", ")
		return res
	}
	res.Status = SeverityOK
	res.Message = "各启用模块的 DSN 格式合法"
	return res
}

// checkWhitelabel 校验白标主色是否为合法 hex 颜色。
func checkWhitelabel() CheckResult {
	res := CheckResult{Name: "白标主题色", Category: CategoryConfig}
	v := strings.TrimSpace(config.Env("GEO_WL_PRIMARY_COLOR", ""))
	if v == "" {
		res.Status = SeverityInfo
		res.Message = "未设置，使用默认 #3B82F6"
		return res
	}
	if !hexColorRe.MatchString(v) {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("GEO_WL_PRIMARY_COLOR=%q 非法（应为 hex 颜色，如 #3B82F6）", v)
		return res
	}
	res.Status = SeverityOK
	res.Message = "合法：" + v
	return res
}

// checkSchedulerConfig 校验定时审计配置（启用时配置文件须存在）。
func checkSchedulerConfig() CheckResult {
	res := CheckResult{Name: "定时审计配置", Category: CategoryConfig}
	enabled := strings.TrimSpace(config.Env("GEO_SCHEDULER_ENABLED", ""))
	if !(strings.EqualFold(enabled, "true") || enabled == "1" || strings.EqualFold(enabled, "on")) {
		res.Status = SeverityInfo
		res.Message = "未启用"
		return res
	}
	path := strings.TrimSpace(config.Env("GEO_SCHEDULER_CONFIG", ""))
	if path == "" {
		res.Status = SeverityWarn
		res.Message = "定时审计已启用但未配置 GEO_SCHEDULER_CONFIG，调度器为空"
		return res
	}
	if _, err := os.Stat(path); err != nil {
		res.Status = SeverityError
		res.Message = "调度配置文件不存在"
		res.Detail = fmt.Sprintf("GEO_SCHEDULER_CONFIG=%s: %v", path, err)
		return res
	}
	res.Status = SeverityOK
	res.Message = "配置文件存在：" + filepath.Clean(path)
	return res
}

// checkRuleset 校验外部规则集（若提供路径）可加载且合法。
func checkRuleset(rulesPath string) CheckResult {
	res := CheckResult{Name: "外部规则集", Category: CategoryConfig}
	if strings.TrimSpace(rulesPath) == "" {
		res.Status = SeverityInfo
		res.Message = "未指定（使用内置默认规则集）"
		return res
	}
	rs, err := config.LoadRuleSet(rulesPath)
	if err != nil {
		res.Status = SeverityError
		res.Message = "规则集加载失败"
		res.Detail = err.Error()
		return res
	}
	if err := rs.Validate(); err != nil {
		res.Status = SeverityError
		res.Message = "规则集校验失败"
		res.Detail = err.Error()
		return res
	}
	res.Status = SeverityOK
	res.Message = fmt.Sprintf("合法：%s v%s（%d 维度权重 / %d 策略系数）",
		rs.Name, rs.Version, len(rs.Weights), len(rs.StrategyEffectiveness))
	return res
}

// ternaryStr 返回条件为真时的 a，否则 b。
func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
