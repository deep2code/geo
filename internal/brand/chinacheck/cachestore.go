package chinacheck

import "time"

// CacheStore 工商查询结果持久化缓存的抽象接口。
//
// 数据特征：K/V 语义，单条目 TTL，读写比约 5:1，上限 ≤ 10 万条。
// 按功能选型：
//   - MySQL（默认，基于 MySQL 的持久化缓存）
//   - JSONL 文件（零依赖本地文件；纯 Go，供内部/调试使用）
type CacheStore interface {
	// Path/DSN 用于日志与统计，返回实际后端的地址或文件路径。
	Path() string
	// Backend 返回当前后端类型标识（mysql / redis 等）。
	Backend() string
	// Stats 缓存统计。
	Stats() CacheStats

	// GetSearch 按 lang/query/limit 取 SearchResult 缓存；未命中/过期返回 (nil,false)。
	GetSearch(lang, query string, limit int) (*SearchResult, bool)
	// SetSearch 写 SearchResult 缓存并持久化。
	SetSearch(lang, query string, limit int, v *SearchResult) error

	// GetSnapshot 取公司快照缓存（先按 companyID，回退到按 query）。
	GetSnapshot(companyID, query string) (*SnapshotResponse, bool)
	// SetSnapshot 写公司快照缓存（companyID 与 query 同时写，如提供）。
	SetSnapshot(companyID, query string, v *SnapshotResponse) error

	// Clear 清空所有缓存条目。
	Clear() error
	// Compact 压缩存储（文件型后端重写、去重、除过期；服务型可返回空实现 no-op）。
	Compact() error
}

// CacheStats 通用缓存统计结构。
type CacheStats struct {
	Backend      string `json:"backend"`
	File         string `json:"file,omitempty"`
	Count        int    `json:"count"`
	MaxItems     int    `json:"max_items"`
	TTLSeconds   int    `json:"ttl_seconds"`
	FileSizeByte int64  `json:"file_size_bytes,omitempty"`
	ServerAddr   string `json:"server_addr,omitempty"`
}

// 保持对外类型别名兼容（原 Cache 现在等价于 CacheStore 接口）。
type Cache = CacheStore

// CacheOption 为 JSONL/Redis 共用的泛型配置选项闭包。
// 具体后端收到无法识别的 Option 可直接忽略。
type CacheOption func(interface{})

// WithMaxItems 设置最大条目数上限（JSONL / MySQL 实现使用）。
func WithMaxItems(n int) CacheOption {
	return func(v interface{}) {
		if c, ok := v.(*jsonlStore); ok && n > 0 {
			c.maxItems = n
		}
		if c, ok := v.(*mysqlCacheStore); ok && n > 0 {
			c.maxItems = n
		}
	}
}

// WithTTL 设置单条目 TTL（MySQL / JSONL / Redis 实现均支持）。
func WithTTL(ttl time.Duration) CacheOption {
	return func(v interface{}) {
		if c, ok := v.(*jsonlStore); ok && ttl > 0 {
			c.ttl = ttl
		}
		if c, ok := v.(*mysqlCacheStore); ok && ttl > 0 {
			c.ttl = ttl
		}
	}
}

// --- 兼容构造入口 ---

// NewCache 创建 CacheStore 实现（固定 MySQL 后端）。
// 保持与旧签名完全一致：filePath 若非空直接当 DSN；否则 env `GEO_MYSQL_DSN`；
// 全缺省时用内置默认 DSN。
// 返回类型使用 Cache 别名（= CacheStore），调用方无需改动。
//
// 如需使用 JSONL 后端，请直接调用 newJSONLCache（同包内部使用）。
func NewCache(filePath string, opts ...CacheOption) (CacheStore, error) {
	return newMySQLCache(filePath, opts...)
}
