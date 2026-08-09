// Package offlinedb 中国大陆工商注册信息离线存储。
//
// 按功能特征选型：
//   - 规模：千万级行（1978-2019 历史数据）
//   - 工作负载：90%+ 全文检索查询（品牌/公司/法人/地址模糊匹配），低频批量导入
//   - 写入：仅导入时批量 INSERT，日常查询为只读
//
// 后端：
//   - SQLite（默认零依赖）—— 纯 Go modernc.org/sqlite + FTS5，千万级 <50ms/次 Top20
//   - DuckDB（生产推荐，需 libduckdb）—— 列式存储 + 并行执行，1000 万级全表聚合更快
package offlinedb

import (
	"context"
)

// OfflineStore 离线工商库抽象接口。
//
// 上游（brand.Engine / CLI / server handler）仅依赖此接口，
// 便于切换不同后端实现（SQLite/DuckDB/MySQL 等）。
type OfflineStore interface {
	// Close 关闭存储（关闭后其它方法行为未定义）。
	Close() error
	// Path 返回实际文件路径或 DSN 描述串（用于日志/统计）。
	Path() string
	// Backend 返回后端类型标识（如 "sqlite" / "duckdb"），用于统计接口返回。
	Backend() string

	// Stats 统计记录数、文件大小、省分布 Top10。
	Stats(ctx context.Context) (Stats, error)
	// Provinces 返回所有省份列表（用于前端筛选项）。
	Provinces(ctx context.Context) ([]string, error)

	// Search 按查询词模糊搜索（FTS/全文优先，失败降级 LIKE）。
	Search(ctx context.Context, opt SearchOptions) ([]Company, error)

	// ImportJSONFile 导入单个 JSON 文件（支持 JSON 数组 / JSON 对象包数组 / JSONL，自动识别）。
	ImportJSONFile(ctx context.Context, path string, batchSize int) (ImportResult, error)
	// ImportDir 递归导入目录下所有 .json 文件。
	ImportDir(ctx context.Context, dir string, batchSize int) (ImportResult, error)

	// Clear 清空 companies 表（连同索引），回收磁盘空间。
	Clear(ctx context.Context) error
}

// --- 兼容别名：保持对外 API 不变 ---

// DB 为与旧调用方完全兼容保留的别名。历史上它是具体的 *sqliteDB 结构体，
// 现在它等价于 OfflineStore 接口，可承载任何后端实现。
type DB = OfflineStore
