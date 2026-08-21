// Package sourcestudy 引擎来源偏好研究：记录每个大模型（引擎）在历次审计中
// 引用了哪些来源（域名/站点），并提供排行、历史趋势、引擎间对比三类分析。
//
// 设计：
//   - 采集：每次品牌审计完成后，把 results[].citations 的 URL 提取规范化域名，
//     append-only 写入 engine_source_citations 表（幂等：同 record+result+url 只插一次）。
//   - 分析：TopSources（按引擎/品牌聚合来源排行）、Trend（来源引用随时间变化）、
//     EngineCompare（各引擎 Top 来源并排对比）。
//   - 历史：cited_at 存审计时间，可按时间范围过滤 → 支撑"某引擎最近 90 天更偏好哪些来源"。
//
// 表结构由 deploy/initdb/schema.sql 初始化，应用内不内嵌迁移。
package sourcestudy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"my-geo/internal/dbprovider"

	_ "github.com/go-sql-driver/mysql"
)

// CitationRec 一次审计中单条引用的来源记录（append-only 时序）。
type CitationRec struct {
	ID             int64  `json:"id"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	Engine         string `json:"engine"`          // 大模型引擎，如 chatgpt / kimi / glm
	SourceDomain   string `json:"source_domain"`   // 规范化来源域名，如 zhihu.com
	SourceCategory string `json:"source_category"` // review_site/docs/social/news/blog/video/other
	BrandName      string `json:"brand_name"`      // 本次审计的品牌
	Prompt         string `json:"prompt,omitempty"` // 触发该引用的查询词
	RecordID       int64  `json:"record_id"`       // 关联 audit_history.id
	ResultIndex    int    `json:"result_index"`    // 结果下标
	CitationURL    string `json:"citation_url"`    // 原始引用 URL
	CitedAt        int64  `json:"cited_at"`        // 审计时间（unix 秒）
}

// SourceStat 来源排行统计（按域名聚合）。
type SourceStat struct {
	SourceDomain   string  `json:"source_domain"`
	Category       string  `json:"category"`
	CitationCount  int     `json:"citation_count"`  // 被引次数
	PromptCount    int     `json:"prompt_count"`    // 覆盖的不同查询词数
	BrandCount     int     `json:"brand_count"`     // 涉及的品牌数
	SharePercent   float64 `json:"share_percent"`   // 占该引擎总引用比例（%）
	LastCitedAt    int64   `json:"last_cited_at"`   // 最近一次被引用时间
}

// TrendPoint 来源引用时间趋势（按天聚合）。
type TrendPoint struct {
	Date          string `json:"date"` // YYYY-MM-DD（本地时区）
	CitationCount int    `json:"citation_count"`
}

// EngineSource 引擎维度来源偏好（对比用）。
type EngineSource struct {
	Engine        string      `json:"engine"`
	TotalCitations int        `json:"total_citations"`
	TopSources    []SourceStat `json:"top_sources"`
}

// StudyFilter 研究查询过滤条件。
type StudyFilter struct {
	Engine      string // 空 = 全部引擎
	BrandName   string // 空 = 全部品牌
	Category    string // 空 = 全部类别
	Domain      string // 空 = 全部来源（趋势接口按此过滤单一来源）
	Days        int    // 近 N 天（0 = 不限制）
	Limit       int    // Top N（<=0 默认 10）
	WorkspaceID string
}

// Store 引擎来源偏好存储接口。
type Store interface {
	Close() error
	Path() string
	// Record 批量写入一次审计的引用来源（幂等：同 record+result+url 只插一次）。
	Record(ctx context.Context, recs []CitationRec) error
	// TopSources 来源排行（按引擎/品牌/时间过滤聚合）。
	TopSources(ctx context.Context, f StudyFilter) ([]SourceStat, error)
	// Trend 某引擎（可再按来源/品牌过滤）的引用时间趋势。
	Trend(ctx context.Context, f StudyFilter) ([]TrendPoint, error)
	// EngineCompare 各引擎 Top 来源对比。
	EngineCompare(ctx context.Context, f StudyFilter) ([]EngineSource, error)
	// Clear 清空全部记录（管理后台维护用）。
	Clear(ctx context.Context) error
}

// mysqlStore MySQL 实现。
type mysqlStore struct {
	path string
	db   *sql.DB
}

// Open 打开引擎来源偏好 MySQL 库并连接。
//
// path 语义：非空时 path 即为 DSN；空时按回退链解析：
// GEO_SOURCE_MYSQL_DSN → GEO_HISTORY_MYSQL_DSN → 默认 geo 主库 DSN。
func Open(path string) (Store, error) {
	var dsn string
	if path != "" {
		dsn = path
	} else {
		if envDSN := os.Getenv("GEO_SOURCE_MYSQL_DSN"); envDSN != "" {
			dsn = envDSN
		} else if histDSN := os.Getenv("GEO_HISTORY_MYSQL_DSN"); histDSN != "" {
			dsn = histDSN
		} else {
			dsn = "geo:geoPass@tcp(127.0.0.1:3306)/geo?parseTime=true&charset=utf8mb4&loc=Local&collation=utf8mb4_unicode_ci"
		}
	}
	dsn = dbprovider.NormalizeMySQLDSN(dsn)
	sqldb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("sourcestudy: 打开 MySQL 失败: %w", err)
	}
	dbprovider.ConfigurePool(sqldb, "default")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("sourcestudy: MySQL ping 失败: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, "SET NAMES utf8mb4"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("sourcestudy: MySQL 初始化会话失败: %w", err)
	}
	return &mysqlStore{path: dsn, db: sqldb}, nil
}

// Close 关闭连接。
func (d *mysqlStore) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Path 返回 DSN 字符串（脱敏前）。
func (d *mysqlStore) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Record 批量写入（INSERT IGNORE：唯一键 record_id+result_index+citation_url 防重复）。
func (d *mysqlStore) Record(ctx context.Context, recs []CitationRec) error {
	if d == nil || d.db == nil || len(recs) == 0 {
		return nil
	}
	const q = `INSERT IGNORE INTO engine_source_citations(
		workspace_id, engine, source_domain, source_category, brand_name, prompt,
		record_id, result_index, citation_url, cited_at
	) VALUES(?,?,?,?,?,?,?,?,?,?)`
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sourcestudy: 事务开启失败: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("sourcestudy: 预编译失败: %w", err)
	}
	defer stmt.Close()
	for _, r := range recs {
		if r.SourceDomain == "" || r.Engine == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			sqlNullStr(r.WorkspaceID), r.Engine, r.SourceDomain, r.SourceCategory,
			r.BrandName, r.Prompt, r.RecordID, r.ResultIndex, r.CitationURL, r.CitedAt,
		); err != nil {
			return fmt.Errorf("sourcestudy: 写入失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sourcestudy: 提交失败: %w", err)
	}
	return nil
}

// TopSources 按引擎/品牌/时间过滤聚合来源排行。
func (d *mysqlStore) TopSources(ctx context.Context, f StudyFilter) ([]SourceStat, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 10
	}
	q := `SELECT source_domain, MAX(source_category), COUNT(*) AS citation_count,
		COUNT(DISTINCT prompt) AS prompt_count, COUNT(DISTINCT brand_name) AS brand_count,
		MAX(cited_at) AS last_cited_at
		FROM engine_source_citations WHERE 1=1` + filterClause(f) + `
		GROUP BY source_domain ORDER BY citation_count DESC, source_domain LIMIT ?`
	rows, err := d.db.QueryContext(ctx, q, append(filterArgs(f), limit)...)
	if err != nil {
		return nil, fmt.Errorf("sourcestudy: TopSources 查询失败: %w", err)
	}
	defer rows.Close()
	var out []SourceStat
	total := 0
	stats := make([]SourceStat, 0, 8)
	for rows.Next() {
		var s SourceStat
		if err := rows.Scan(&s.SourceDomain, &s.Category, &s.CitationCount, &s.PromptCount, &s.BrandCount, &s.LastCitedAt); err != nil {
			return nil, fmt.Errorf("sourcestudy: TopSources 扫描失败: %w", err)
		}
		stats = append(stats, s)
		total += s.CitationCount
	}
	if total > 0 {
		for i := range stats {
			stats[i].SharePercent = float64(int(float64(stats[i].CitationCount)*10000/float64(total)+0.5)) / 100
		}
	}
	out = stats
	if len(out) > limit {
		out = out[:limit]
	}
	return out, rows.Err()
}

// Trend 按天聚合某引擎（可再按来源/品牌过滤）的引用趋势。
func (d *mysqlStore) Trend(ctx context.Context, f StudyFilter) ([]TrendPoint, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	q := `SELECT DATE_FORMAT(FROM_UNIXTIME(cited_at), '%Y-%m-%d') AS d, COUNT(*)
		FROM engine_source_citations WHERE 1=1` + filterClause(f) + `
		GROUP BY d ORDER BY d ASC`
	rows, err := d.db.QueryContext(ctx, q, filterArgs(f)...)
	if err != nil {
		return nil, fmt.Errorf("sourcestudy: Trend 查询失败: %w", err)
	}
	defer rows.Close()
	var out []TrendPoint
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.Date, &p.CitationCount); err != nil {
			return nil, fmt.Errorf("sourcestudy: Trend 扫描失败: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EngineCompare 各引擎 Top 来源对比。
func (d *mysqlStore) EngineCompare(ctx context.Context, f StudyFilter) ([]EngineSource, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 10
	}
	// 每个引擎的总引用数。
	totQ := `SELECT engine, COUNT(*) FROM engine_source_citations WHERE 1=1` +
		engineBrandClause(f) + ` GROUP BY engine ORDER BY engine`
	rows, err := d.db.QueryContext(ctx, totQ, engineBrandArgs(f)...)
	if err != nil {
		return nil, fmt.Errorf("sourcestudy: EngineCompare 总数查询失败: %w", err)
	}
	totals := map[string]int{}
	engineOrder := []string{}
	for rows.Next() {
		var eng string
		var n int
		if err := rows.Scan(&eng, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("sourcestudy: EngineCompare 总数扫描失败: %w", err)
		}
		totals[eng] = n
		engineOrder = append(engineOrder, eng)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]EngineSource, 0, len(engineOrder))
	for _, eng := range engineOrder {
		ef := f
		ef.Engine = eng
		top, err := d.TopSources(ctx, ef)
		if err != nil {
			return nil, err
		}
		out = append(out, EngineSource{Engine: eng, TotalCitations: totals[eng], TopSources: top})
	}
	return out, nil
}

// Clear 清空全部来源记录（维护用）。
func (d *mysqlStore) Clear(ctx context.Context) error {
	if d == nil || d.db == nil {
		return nil
	}
	if _, err := d.db.ExecContext(ctx, "TRUNCATE TABLE engine_source_citations"); err != nil {
		return fmt.Errorf("sourcestudy: Clear 失败: %w", err)
	}
	return nil
}

// --- SQL 片段与参数辅助 ---

// filterClause 组合 engine/brand/category/domain/时间过滤（TopSources 与 Trend 共用）。
func filterClause(f StudyFilter) string {
	var parts []string
	if f.Engine != "" {
		parts = append(parts, " AND engine = ?")
	}
	if f.BrandName != "" {
		parts = append(parts, " AND brand_name = ?")
	}
	if f.Category != "" {
		parts = append(parts, " AND source_category = ?")
	}
	if f.Domain != "" {
		parts = append(parts, " AND source_domain = ?")
	}
	if f.Days > 0 {
		parts = append(parts, " AND cited_at >= UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL ? DAY))")
	}
	return strings.Join(parts, "")
}

// filterArgs 与 filterClause 对应的参数列表。
func filterArgs(f StudyFilter) []interface{} {
	var args []interface{}
	if f.Engine != "" {
		args = append(args, f.Engine)
	}
	if f.BrandName != "" {
		args = append(args, f.BrandName)
	}
	if f.Category != "" {
		args = append(args, f.Category)
	}
	if f.Domain != "" {
		args = append(args, f.Domain)
	}
	if f.Days > 0 {
		args = append(args, f.Days)
	}
	return args
}

// engineBrandClause 仅按引擎/品牌过滤（EngineCompare 总数查询用）。
func engineBrandClause(f StudyFilter) string {
	var parts []string
	if f.BrandName != "" {
		parts = append(parts, " AND brand_name = ?")
	}
	if f.Category != "" {
		parts = append(parts, " AND source_category = ?")
	}
	if f.Days > 0 {
		parts = append(parts, " AND cited_at >= UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL ? DAY))")
	}
	return strings.Join(parts, "")
}

func engineBrandArgs(f StudyFilter) []interface{} {
	var args []interface{}
	if f.BrandName != "" {
		args = append(args, f.BrandName)
	}
	if f.Category != "" {
		args = append(args, f.Category)
	}
	if f.Days > 0 {
		args = append(args, f.Days)
	}
	return args
}

func sqlNullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
