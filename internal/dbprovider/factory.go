package dbprovider

import (
	"fmt"
	"os"
	"strings"

	"my-geo/internal/config"
)

// ModuleKind 功能模块枚举（与 MySQL DSN 环境变量一一映射）。
type ModuleKind string

const (
	ModuleOfflineCompanies ModuleKind = "offline_companies"
	ModuleAuditHistory     ModuleKind = "audit_history"
	ModuleChinaCheckCache  ModuleKind = "chinacheck_cache"
	// ModuleBilling 计费 / 订阅 / 订单 / 异步任务队列表。
	// 默认复用 GEO_AUTH_MYSQL_DSN（与账号体系同库，便于订阅关联工作区），
	// 也可通过 GEO_BILLING_MYSQL_DSN 独立部署。
	ModuleBilling ModuleKind = "billing"
	// ModuleSourceStudy 引擎来源偏好研究（大模型引用来源的记录与历史趋势）。
	// 表与审计历史同库（geo 主库）；默认回退 GEO_HISTORY_MYSQL_DSN。
	ModuleSourceStudy ModuleKind = "source_study"
)

// moduleConfig：各模块的 DSN 环境变量。
// 所有模块默认后端 = TypeMySQL。
var moduleConfig = map[ModuleKind]struct {
	typeEnv     string // *_DB_TYPE / *_CACHE_TYPE（保留兼容，值必须为 mysql 或 redis）
	dsnEnv      string // *_MYSQL_DSN（优先）
	oldPathEnv  string // 兼容 *_DB_PATH / *_CACHE_PATH（若值形如 user:pass@tcp(...) 视为 DSN 直接用）
	defaultType Type
}{
	ModuleOfflineCompanies: {
		typeEnv:     "GEO_OFFLINE_DB_TYPE",
		dsnEnv:      "GEO_OFFLINE_MYSQL_DSN",
		oldPathEnv:  "GEO_OFFLINE_DB_PATH",
		defaultType: TypeMySQL,
	},
	ModuleAuditHistory: {
		typeEnv:     "GEO_HISTORY_DB_TYPE",
		dsnEnv:      "GEO_HISTORY_MYSQL_DSN",
		oldPathEnv:  "GEO_HISTORY_DB_PATH",
		defaultType: TypeMySQL,
	},
	ModuleChinaCheckCache: {
		typeEnv:     "GEO_CHINACHECK_CACHE_TYPE",
		dsnEnv:      "GEO_CHINACHECK_MYSQL_DSN",
		oldPathEnv:  "GEO_CHINACHECK_CACHE_PATH",
		defaultType: TypeMySQL,
	},
	ModuleBilling: {
		typeEnv:     "GEO_BILLING_DB_TYPE",
		dsnEnv:      "GEO_BILLING_MYSQL_DSN",
		oldPathEnv:  "GEO_BILLING_DB_PATH",
		defaultType: TypeMySQL,
	},
	ModuleSourceStudy: {
		typeEnv:     "GEO_SOURCE_DB_TYPE",
		dsnEnv:      "GEO_SOURCE_MYSQL_DSN",
		oldPathEnv:  "GEO_SOURCE_DB_PATH",
		defaultType: TypeMySQL,
	},
}

// ParseType 按模块解析后端类型。
// 目前可选项：
//   - OfflineCompanies/AuditHistory: 仅 mysql（其他值告警并回退 mysql）
//   - ChinaCheckCache: mysql / redis
func ParseType(mod ModuleKind) Type {
	cfg, ok := moduleConfig[mod]
	if !ok {
		return TypeMySQL
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(cfg.typeEnv)))
	switch mod {
	case ModuleOfflineCompanies, ModuleAuditHistory, ModuleBilling, ModuleSourceStudy:
		switch raw {
		case "", string(TypeMySQL):
			return TypeMySQL
		}
	case ModuleChinaCheckCache:
		switch raw {
		case "", string(TypeMySQL):
			return TypeMySQL
		case string(TypeRedis):
			return TypeRedis
		}
	}
	fmt.Fprintf(os.Stderr, "[dbprovider 警告] %s=%q 未识别或不再支持，已回退到 mysql。\n",
		cfg.typeEnv, raw)
	return TypeMySQL
}

// DSNFor 返回模块对应的 MySQL DSN（优先 *_MYSQL_DSN，其次旧 *_PATH 变量）。
// 两者都为空时，由具体实现模块使用内置默认 DSN。
func DSNFor(mod ModuleKind) string {
	cfg, ok := moduleConfig[mod]
	if !ok {
		return ""
	}
	if d := strings.TrimSpace(config.Env(cfg.dsnEnv, "")); d != "" {
		return d
	}
	// 兼容：旧 PATH env 若形如 "xxx@tcp(...)"，视为直接 DSN
	if p := strings.TrimSpace(config.Env(cfg.oldPathEnv, "")); p != "" {
		if strings.Contains(p, "@tcp(") || strings.Contains(p, "mysql:") {
			return p
		}
	}
	return ""
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
	}
	v := config.Env(suffix, "true")
	return !(strings.EqualFold(v, "false") || strings.EqualFold(v, "0") || strings.EqualFold(v, "off"))
}

// Resolve 返回实际后端类型 + 是否发生回退。
func Resolve(mod ModuleKind) (actual Type, fellBack bool) {
	return ParseType(mod), false
}

// Describe 返回日志描述。
func Describe(mod ModuleKind) string {
	t := ParseType(mod)
	p := DSNFor(mod)
	if p == "" {
		p = "<default>"
	} else {
		// 脱敏密码
		if idx := strings.Index(p, ":"); idx > 0 && idx < strings.Index(p, "@") {
			p = p[:strings.Index(p, ":")] + ":***" + p[strings.Index(p, "@"):]
		}
	}
	return fmt.Sprintf("module=%s backend=%s dsn=%s", mod, t, p)
}
