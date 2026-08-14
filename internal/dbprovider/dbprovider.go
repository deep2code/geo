// Package dbprovider 统一按功能模块选择 MySQL 后端的工厂层（仅 MySQL）。
//
// 原则：
//  1. 本工程已移除 SQLite/JSONL/DuckDB，所有持久化模块统一使用 MySQL；
//  2. 每个模块使用独立的 DSN 环境变量（也可复用同一个 MySQL 实例 + 不同库名）；
//  3. 仅保留 Redis 作为 K/V 高性能可选后端（MySQL 是 chinacheck_cache 默认）。
package dbprovider

// Type 数据库后端类型（现在仅 MySQL + Redis 有效）。
type Type string

const (
	// TypeAuto 默认。
	TypeAuto Type = ""
	// TypeMySQL MySQL 8.0+（唯一持久化默认后端）。
	TypeMySQL Type = "mysql"
	// TypeRedis Redis（仅 chinacheck_cache 的可选后端）。
	TypeRedis Type = "redis"
)

// Known 已知后端列表（用于 UI/验证）。
var Known = []Type{TypeMySQL, TypeRedis}

// String 返回小写形式。
func (t Type) String() string { return string(t) }

// RequiresExternal 是否需要外部服务/二进制依赖（MySQL/Redis 都需要外部服务）。
func (t Type) RequiresExternal() bool {
	switch t {
	case TypeMySQL, TypeRedis:
		return true
	}
	return false
}

// Fallback 保留接口（只有一个后端，返回自身即可）。
func (t Type) Fallback() Type {
	if t == TypeRedis {
		return TypeMySQL // Redis 连不上时 chinacheck_cache 可回退 MySQL
	}
	return TypeMySQL
}
