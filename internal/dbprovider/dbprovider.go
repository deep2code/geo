// Package dbprovider 统一按功能模块选择 MySQL 后端的工厂层（仅 MySQL + Redis）。
//
// 原则：
//  1. 本工程已移除 SQLite/JSONL/DuckDB，所有持久化模块统一使用 MySQL；
//  2. 每个模块使用独立的 DSN 环境变量（也可复用同一个 MySQL 实例 + 不同库名）；
//  3. 仅保留 Redis 作为 K/V 高性能可选后端（MySQL 是 chinacheck_cache 默认）。
package dbprovider

import (
	"database/sql"
	"strings"
	"time"
)

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

// NormalizeMySQLDSN 为 DSN 幂等追加性能与安全性参数（缺省即最佳实践）：
//
//   - interpolateParams=true  客户端就地替换占位符，避免 PREPARE/EXECUTE 2 次 roundtrip
//   - multiStatements=true   一次执行多条 SQL（schema 初始化 / OPTIMIZE 批处理）
//   - maxAllowedPacket=64M   允许存储 MEDIUMTEXT / MEDIUMBLOB 最大 16MB
//   - timeout=10s            连接握手超时，避免 DNS/网络 hang
//   - readTimeout=30s        单次读取超时，防 SQL 挂起
//   - writeTimeout=30s       单次写入超时，防大事务/大导入卡死
//   - parseTime + charset=utf8mb4 + loc=Local + tls=preferred
//
// 已存在的同名参数不会被覆盖，方便用户自定义（例如企业生产 tls=true 场景）。
// tls=preferred：若服务端支持 TLS 则加密传输；不支持时回退明文（兼容本地内网
// 未启用 TLS 的 MySQL），比强制 tls=false 更安全。生产环境建议显式配置 tls=true。
func NormalizeMySQLDSN(raw string) string {
	if raw == "" {
		return raw
	}
	required := []struct{ k, v string }{
		{"parseTime", "true"},
		{"charset", "utf8mb4"},
		{"loc", "Local"},
		{"interpolateParams", "true"},
		{"multiStatements", "true"},
		{"maxAllowedPacket", "67108864"},
		{"tls", "preferred"},
		{"timeout", "10s"},
		{"readTimeout", "30s"},
		{"writeTimeout", "30s"},
	}
	qStart := strings.Index(raw, "?")
	if qStart < 0 {
		parts := make([]string, 0, len(required))
		for _, r := range required {
			parts = append(parts, r.k+"="+r.v)
		}
		return raw + "?" + strings.Join(parts, "&")
	}
	base := raw[:qStart]
	query := raw[qStart+1:]
	has := map[string]bool{}
	pairs := []string{}
	if query != "" {
		pairs = strings.Split(query, "&")
	}
	for _, p := range pairs {
		if p == "" {
			continue
		}
		if eq := strings.Index(p, "="); eq > 0 {
			has[strings.ToLower(p[:eq])] = true
		}
	}
	for _, r := range required {
		if !has[strings.ToLower(r.k)] {
			pairs = append(pairs, r.k+"="+r.v)
		}
	}
	// 过滤空对
	outPairs := pairs[:0]
	for _, p := range pairs {
		if p != "" {
			outPairs = append(outPairs, p)
		}
	}
	return base + "?" + strings.Join(outPairs, "&")
}

// ConfigurePool 按 Go + MySQL 连接池最佳实践设置 *sql.DB 参数。
// profile 预设：
//   - "auth"     账号/权限：读多写少，低并发
//   - "cache"    chinacheck/Redis 的 MySQL 回退：短 KV + TTL，高 QPS 但小事务
//   - "default"  审计历史：中等并发，偶尔批量导入
//   - "offline"  离线库（companies）：导入期写密集、查询期读密集
//
// 关键参数依据（参考 go-sql-driver/mysql FAQ、GORM Performance 文档、
// 开源项目 openseo / hraftdb / etcd 的 RDBMS 实践）：
//   - MaxOpenConns  过大反而在 MySQL 端触发 mutex / innodb_thread_concurrency 反压
//   - MaxIdleConns  与 MaxOpen 接近，避免频繁新建/关闭（TCP 握手比查询还贵）
//   - MaxLifetime   30m，低于 MySQL wait_timeout(默认 28800s) 一个数量级，强制轮转
//   - MaxIdleTime   10m，避免大量 sleep 连接占位消耗 MySQL open_files_limit
func ConfigurePool(db *sql.DB, profile string) {
	if db == nil {
		return
	}
	switch profile {
	case "auth":
		db.SetMaxOpenConns(32)
		db.SetMaxIdleConns(16)
		db.SetConnMaxLifetime(30 * time.Minute)
		db.SetConnMaxIdleTime(10 * time.Minute)
	case "cache":
		db.SetMaxOpenConns(64)
		db.SetMaxIdleConns(32)
		db.SetConnMaxLifetime(30 * time.Minute)
		db.SetConnMaxIdleTime(10 * time.Minute)
	case "offline":
		// 导入/扫描期并发高，设大些
		db.SetMaxOpenConns(128)
		db.SetMaxIdleConns(64)
		db.SetConnMaxLifetime(1 * time.Hour)
		db.SetConnMaxIdleTime(15 * time.Minute)
	default: // audit history
		db.SetMaxOpenConns(128)
		db.SetMaxIdleConns(64)
		db.SetConnMaxLifetime(30 * time.Minute)
		db.SetConnMaxIdleTime(10 * time.Minute)
	}
}
