// Package dbprovider 统一按功能模块选择 MySQL 后端的工厂层。
//
// 原则：
//  1. 本工程所有持久化模块统一使用 MySQL 8.0+（无 SQLite/JSONL/DuckDB/Redis 后端）；
//  2. 每个模块使用独立的 DSN 环境变量（也可复用同一个 MySQL 实例 + 不同库名）；
//  3. 单库架构只需配置统一的 GEO_MYSQL_DSN，各模块专属 *_MYSQL_DSN 可选覆盖。
package dbprovider

import (
	"database/sql"
	"strings"
	"time"
)

// NormalizeMySQLDSN 为 DSN 幂等追加性能与安全性参数（缺省即最佳实践）：
//
//   - interpolateParams=true  客户端就地替换占位符，避免 PREPARE/EXECUTE 2 次 roundtrip
//   - maxAllowedPacket=64M   允许存储 MEDIUMTEXT / MEDIUMBLOB 最大 16MB
//   - timeout=10s            连接握手超时，避免 DNS/网络 hang
//   - readTimeout=30s        单次读取超时，防 SQL 挂起
//   - writeTimeout=30s       单次写入超时，防大事务/大导入卡死
//   - parseTime + charset=utf8mb4 + loc=Local + tls=preferred
//
// 注意：不注入 multiStatements=true —— 该参数放大 SQL 注入面，仅建表/导入路径
// 需要时由调用方在 DSN 中显式声明。业务查询一律走参数化单语句。
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
//   - "cache"    chinacheck 缓存：短 KV + TTL，高 QPS 但小事务
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
