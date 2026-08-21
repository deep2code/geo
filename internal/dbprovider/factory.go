package dbprovider

import (
	"strings"

	"my-geo/internal/config"
)

// ModuleKind 功能模块枚举（对应 *_ENABLED 开关；DSN 统一使用 GEO_MYSQL_DSN）。
type ModuleKind string

const (
	ModuleOfflineCompanies ModuleKind = "offline_companies"
	ModuleAuditHistory     ModuleKind = "audit_history"
	ModuleChinaCheckCache  ModuleKind = "chinacheck_cache"
	// ModuleBilling 计费 / 订阅 / 订单 / 异步任务队列表（与账号体系同库）。
	ModuleBilling ModuleKind = "billing"
	// ModuleSourceStudy 引擎来源偏好研究（大模型引用来源的记录与历史趋势，与审计历史同库）。
	ModuleSourceStudy ModuleKind = "source_study"
	// ModuleExternalSubmissions 外部系统提交的大模型对话采集与分析（与审计历史同库）。
	ModuleExternalSubmissions ModuleKind = "external_submissions"
)

// DSNFor 返回模块对应的 MySQL DSN：全项目统一使用 GEO_MYSQL_DSN（单库架构，只配一个）。
// 未配置时返回 ""，由具体实现模块使用内置默认 DSN。
func DSNFor(mod ModuleKind) string {
	return strings.TrimSpace(config.Env("GEO_MYSQL_DSN", ""))
}

// PathFor 别名，等价 DSNFor（保持对 server.go 旧调用签名的兼容）。
func PathFor(mod ModuleKind) string { return DSNFor(mod) }

// EnabledFor *_ENABLED 开关；默认 true。
func EnabledFor(mod ModuleKind) bool {
	suffix := ""
	switch mod {
	case ModuleOfflineCompanies:
		suffix = "GEO_OFFLINE_DB_ENABLED"
	case ModuleAuditHistory:
		suffix = "GEO_HISTORY_DB_ENABLED"
	case ModuleChinaCheckCache:
		suffix = "GEO_CHINACHECK_CACHE_ENABLED"
	case ModuleBilling:
		suffix = "GEO_BILLING_DB_ENABLED"
	case ModuleSourceStudy:
		suffix = "GEO_SOURCE_DB_ENABLED"
	case ModuleExternalSubmissions:
		suffix = "GEO_EXTERNAL_DB_ENABLED"
	}
	v := config.Env(suffix, "true")
	return !(strings.EqualFold(v, "false") || strings.EqualFold(v, "0") || strings.EqualFold(v, "off"))
}
