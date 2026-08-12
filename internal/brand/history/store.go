// Package history 品牌可见度审计时间序列存储。
//
// 按功能特征选型：
//   - 写入：时序追加（每次 Audit 一条）+ JSON 列（完整报告快照）
//   - 查询：按品牌名+时间范围倒序、最新一条、统计、历史列表
//   - 规模：中小（< 百万行 / 年）
//
// 后端：
//   - SQLite（默认，零依赖） — Store 接口的 sqliteStore 实现（同文件，保持原 Open 函数兼容）
//   - MySQL（生产推荐，需要独立服务）— 编译 tag "geo_mysql" 启用
package history

import (
	"context"
	"time"
)

// Record 一次审计快照的标量字段（用于趋势图表）。
// 字段与原实现保持完全一致，调用方无需改动。
type Record struct {
	ID        int64   `json:"id"`
	BrandName string  `json:"brand_name"`
	Generated int64   `json:"generated_at"` // unix 秒
	Score     float64 `json:"score"`
	Grade     string  `json:"grade"`
	Tier      string  `json:"tier"`

	EntityCompleteness float64 `json:"entity_completeness_score"`

	MentionRate       float64 `json:"mention_rate"`
	CitationRate      float64 `json:"citation_rate"`
	ShareOfVoice      float64 `json:"share_of_voice"`
	CitationPosition  float64 `json:"citation_position"`
	Sentiment         float64 `json:"sentiment"`
	EntityRecognition float64 `json:"entity_recognition"`

	ContentGaps     int `json:"content_gaps_count"`
	CompetitorCount int `json:"competitor_count"`
	NegativeCount   int `json:"negative_count"`
	ActionCount     int `json:"action_count"`

	ReportJSON string `json:"report_json,omitempty"`
}

// Stats 历史库统计信息。
type Stats struct {
	Path      string `json:"path"`
	Records   int64  `json:"records"`
	Brands    int64  `json:"brands"`
	FileSize  int64  `json:"file_size_bytes,omitempty"` // 文件型后端填充，服务型留空
	OldestAt  int64  `json:"oldest_at,omitempty"`
	NewestAt  int64  `json:"newest_at,omitempty"`
	Backend   string `json:"backend,omitempty"` // 实际使用的后端类型（sqlite / mysql 等）
}

// Store 审计历史存储抽象接口。
//
// 所有返回值与调用语义与原 sqliteStore 保持完全一致，上游（brand.Engine、
// scheduler、server handler）无需改动。
type Store interface {
	// Close 关闭存储。关闭后其它方法行为未定义。
	Close() error
	// Path 用于日志/调试，服务型后端可返回 DSN 脱敏串或空。
	Path() string
	// Save 写入一条审计快照，返回新记录 ID。
	Save(ctx context.Context, r Record) (int64, error)
	// List 查询指定品牌的审计历史（按时间降序），limit<=0 表示默认 100。
	List(ctx context.Context, brandName string, limit int) ([]Record, error)
	// Latest 查询指定品牌最新一条审计记录（含完整 ReportJSON），无记录时返回 (nil,nil)。
	Latest(ctx context.Context, brandName string) (*Record, error)
	// GetByID 按 ID 取单条记录。
	GetByID(ctx context.Context, id int64) (*Record, error)
	// Brands 列出所有品牌名（用于下拉框）。
	Brands(ctx context.Context) ([]string, error)
	// Stats 返回历史库统计信息。
	Stats(ctx context.Context) (Stats, error)
	// DailyCounts 按天聚合计数（过去 days 天，含今天），返回按日期升序的桶。
	// 日期格式 YYYY-MM-DD（本地时区）。没有记录的日期仍然返回 count=0/avg_score=-1，
	// 方便前端直接画趋势线，避免 gap。
	DailyCounts(ctx context.Context, days int) ([]DailyBucket, error)
	// Clear 清空所有历史记录。
	Clear(ctx context.Context) error
	// DeleteOlderThan 删除 N 天之前的记录，返回删除条数。
	DeleteOlderThan(ctx context.Context, days int) (int64, error)
}

// DailyBucket 单天聚合计数（Dashboard 30 天趋势用）。
type DailyBucket struct {
	Date     string  `json:"date"`      // YYYY-MM-DD，本地时区
	Count    int64   `json:"count"`     // 当天审计记录条数
	AvgScore float64 `json:"avg_score"` // 当日平均分，-1 表示当天无记录
}

// --- 兼容函数（保持对外 API 不变）---

// DB 为与旧调用方完全兼容保留的别名。历史上它是具体的 *sqliteDB，
// 现在它等价于 Store 接口，可承载任何后端实现。
type DB = Store

// --- 辅助 ---

// MarshalReport 将可见度报告序列化为 JSON 字符串（给 Save 调用方使用）。
func MarshalReport(v interface{}) (string, error) {
	// 具体实现保留在单独的文件里，此处避免依赖循环
	return marshalJSON(v)
}

// TimeNow 便于测试替换。
var TimeNow = time.Now
