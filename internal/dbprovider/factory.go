package dbprovider

import (
	"fmt"
	"os"
	"strings"

	"my-geo/internal/config"
)

// ===== 模块到环境变量 + 默认后端的映射（单一事实来源）=====

// ModuleKind 功能模块枚举（与上述选型表一一对应）。
type ModuleKind string

const (
	// ModuleOfflineCompanies 离线工商库（千万级行 + 全文检索）。
	ModuleOfflineCompanies ModuleKind = "offline_companies"
	// ModuleAuditHistory 审计历史时序库。
	ModuleAuditHistory ModuleKind = "audit_history"
	// ModuleChinaCheckCache China-Check 查询缓存（K/V + TTL）。
	ModuleChinaCheckCache ModuleKind = "chinacheck_cache"
)

// moduleConfig 各模块的默认后端 + 环境变量名映射。
var moduleConfig = map[ModuleKind]struct {
	// typeEnv 后端类型环境变量。
	typeEnv string
	// pathEnv 路径/DSN 环境变量。
	pathEnv string
	// defaultType 未设置时的零依赖默认后端。
	defaultType Type
}{
	ModuleOfflineCompanies: {
		typeEnv:     "GEO_OFFLINE_DB_TYPE",
		pathEnv:     "GEO_OFFLINE_DB_PATH",
		defaultType: TypeSQLite,
	},
	ModuleAuditHistory: {
		typeEnv:     "GEO_HISTORY_DB_TYPE",
		pathEnv:     "GEO_HISTORY_DB_PATH",
		defaultType: TypeSQLite,
	},
	ModuleChinaCheckCache: {
		typeEnv:     "GEO_CHINACHECK_CACHE_TYPE",
		pathEnv:     "GEO_CHINACHECK_CACHE_PATH",
		defaultType: TypeJSONL,
	},
}

// ===== 解析辅助 =====

// ParseType 按模块枚举读取环境变量，解析出期望的后端类型。
// 环境变量为空或未知值 → 返回默认零依赖后端（不报错）。
//
// 可选后端值：
//   - OfflineCompanies: sqlite / duckdb
//   - AuditHistory:     sqlite / mysql
//   - ChinaCheckCache:  jsonl / redis
func ParseType(mod ModuleKind) Type {
	cfg, ok := moduleConfig[mod]
	if !ok {
		return TypeSQLite // 未知模块兜底
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(cfg.typeEnv)))
	switch mod {
	case ModuleOfflineCompanies:
		switch raw {
		case "", string(TypeSQLite):
			return TypeSQLite
		case string(TypeDuckDB):
			return TypeDuckDB
		}
	case ModuleAuditHistory:
		switch raw {
		case "", string(TypeSQLite):
			return TypeSQLite
		case string(TypeMySQL):
			return TypeMySQL
		}
	case ModuleChinaCheckCache:
		switch raw {
		case "", string(TypeJSONL):
			return TypeJSONL
		case string(TypeRedis):
			return TypeRedis
		}
	}
	// 未知值 → 回退默认
	fmt.Fprintf(os.Stderr, "[dbprovider 警告] %s 的 %s=%q 未识别，使用默认 %s\n",
		mod, cfg.typeEnv, raw, cfg.defaultType)
	return cfg.defaultType
}

// PathFor 读取模块对应的路径/DSN 环境变量，返回空串表示用各模块默认路径。
func PathFor(mod ModuleKind) string {
	cfg, ok := moduleConfig[mod]
	if !ok {
		return ""
	}
	return config.Env(cfg.pathEnv, "")
}

// EnabledFor 读取对应模块的 *_ENABLED 开关；未设置默认 true。
func EnabledFor(mod ModuleKind) bool {
	suffix := ""
	switch mod {
	case ModuleOfflineCompanies:
		suffix = "GEO_OFFLINE_DB_ENABLED"
	case ModuleAuditHistory:
		suffix = "GEO_HISTORY_DB_ENABLED"
	case ModuleChinaCheckCache:
		suffix = "GEO_CHINACHECK_CACHE_ENABLED"
	}
	v := config.Env(suffix, "true")
	return !(strings.EqualFold(v, "false") || strings.EqualFold(v, "0") || strings.EqualFold(v, "off"))
}

// Resolve 综合解析模块后端类型：先按环境变量，再判断是否需要外部依赖，
// 再返回"实际使用类型 + 是否回退了"。调用方可用实际类型创建实例、用回退标志打印告警。
func Resolve(mod ModuleKind) (actual Type, fellBack bool) {
	want := ParseType(mod)
	if want.RequiresExternal() {
		// 外部依赖暂时假设不可用（编译 tag 或运行时探测由具体实现负责），
		// 这里先告知调用方"请求了外部后端，但可能需要降级"。
		// 具体是否真的降级，由构造器自行检测后决定并可再次调用 Fallback()。
		return want, false
	}
	return want, false
}

// Describe 返回用于日志打印的模块后端描述。
func Describe(mod ModuleKind) string {
	t := ParseType(mod)
	p := PathFor(mod)
	if p == "" {
		p = "<default>"
	}
	return fmt.Sprintf("module=%s backend=%s path=%s", mod, t, p)
}
