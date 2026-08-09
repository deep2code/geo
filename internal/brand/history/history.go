// Package history 品牌可见度审计时间序列存储（SQLite）。
//
// 每次 Audit 调用 Save 写入一行快照，前端通过 List 查询趋势数据。
// 数据文件默认 ~/.local/share/geo/geo_brand_history.db。
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Record 一次审计快照的标量字段（用于趋势图表）。
type Record struct {
	ID        int64   `json:"id"`
	BrandName string  `json:"brand_name"`
	Generated int64   `json:"generated_at"` // unix 秒
	Score     float64 `json:"score"`
	Grade     string  `json:"grade"`
	Tier      string  `json:"tier"`

	// 实体完备度
	EntityCompleteness float64 `json:"entity_completeness_score"`

	// 6 维评分明细
	MentionRate       float64 `json:"mention_rate"`
	CitationRate      float64 `json:"citation_rate"`
	ShareOfVoice      float64 `json:"share_of_voice"`
	CitationPosition  float64 `json:"citation_position"`
	Sentiment         float64 `json:"sentiment"`
	EntityRecognition float64 `json:"entity_recognition"`

	// 汇总计数
	ContentGaps     int `json:"content_gaps_count"`
	CompetitorCount int `json:"competitor_count"`
	NegativeCount   int `json:"negative_count"`
	ActionCount     int `json:"action_count"`

	// 完整报告 JSON（用于前端回溯单次审计详情）
	ReportJSON string `json:"report_json,omitempty"`
}

// Stats 历史库统计信息。
type Stats struct {
	Path      string `json:"path"`
	Records   int64  `json:"records"`
	Brands    int64  `json:"brands"`
	FileSize  int64  `json:"file_size_bytes"`
	OldestAt  int64  `json:"oldest_at,omitempty"`
	NewestAt  int64  `json:"newest_at,omitempty"`
}

const sqlSchema = `
CREATE TABLE IF NOT EXISTS audit_history (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	brand_name            TEXT    NOT NULL,
	generated_at          INTEGER NOT NULL,
	score                 REAL    NOT NULL,
	grade                 TEXT    NOT NULL,
	tier                  TEXT    NOT NULL,
	entity_completeness   REAL    NOT NULL DEFAULT 0,
	mention_rate          REAL    NOT NULL DEFAULT 0,
	citation_rate         REAL    NOT NULL DEFAULT 0,
	share_of_voice        REAL    NOT NULL DEFAULT 0,
	citation_position     REAL    NOT NULL DEFAULT 0,
	sentiment             REAL    NOT NULL DEFAULT 0,
	entity_recognition    REAL    NOT NULL DEFAULT 0,
	content_gaps_count    INTEGER NOT NULL DEFAULT 0,
	competitor_count      INTEGER NOT NULL DEFAULT 0,
	negative_count        INTEGER NOT NULL DEFAULT 0,
	action_count          INTEGER NOT NULL DEFAULT 0,
	report_json           TEXT
);
CREATE INDEX IF NOT EXISTS idx_history_brand_time ON audit_history(brand_name, generated_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_time ON audit_history(generated_at DESC);
`

// DB 品牌审计历史数据库（并发安全，database/sql 内部池化）。
type DB struct {
	path string
	db   *sql.DB
}

// Open 打开/创建历史数据库并完成 schema 初始化。
// path 为空时使用默认路径 ~/.local/share/geo/geo_brand_history.db。
func Open(path string) (*DB, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("history: 获取用户目录失败: %w", err)
		}
		path = filepath.Join(home, ".local", "share", "geo", "geo_brand_history.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("history: 创建数据目录失败: %w", err)
	}
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=mmap_size(1073741824)&_pragma=cache_size(-262144)&_pragma=foreign_keys(off)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("history: 打开数据库失败: %w", err)
	}
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	if _, err := sqldb.Exec(sqlSchema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("history: 初始化 schema 失败: %w", err)
	}
	return &DB{path: path, db: sqldb}, nil
}

// Close 关闭数据库。
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Path 返回数据库文件路径。
func (d *DB) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Save 写入一条审计快照。reportJSON 可为空。
func (d *DB) Save(ctx context.Context, r Record) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	if r.Generated == 0 {
		r.Generated = time.Now().Unix()
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO audit_history(
		brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.BrandName, r.Generated, r.Score, r.Grade, r.Tier,
		r.EntityCompleteness, r.MentionRate, r.CitationRate, r.ShareOfVoice,
		r.CitationPosition, r.Sentiment, r.EntityRecognition,
		r.ContentGaps, r.CompetitorCount, r.NegativeCount, r.ActionCount,
		r.ReportJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("history: 写入审计快照失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// List 查询指定品牌的审计历史，按时间降序。limit<=0 表示默认 100。
func (d *DB) List(ctx context.Context, brandName string, limit int) ([]Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx, `SELECT
		id, brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count
		FROM audit_history WHERE brand_name = ?
		ORDER BY generated_at DESC
		LIMIT ?`, brandName, limit)
	if err != nil {
		return nil, fmt.Errorf("history: 查询历史失败: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
			&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
			&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
			&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount); err != nil {
			return nil, fmt.Errorf("history: 扫描行失败: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Latest 查询指定品牌最新一条审计记录的完整信息（含 report_json）。
//
// 用于报告导出等需要回溯单次审计详情的场景。无记录时返回 (nil, nil)。
func (d *DB) Latest(ctx context.Context, brandName string) (*Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	var r Record
	var reportJSON sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT
		id, brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
		FROM audit_history WHERE brand_name = ?
		ORDER BY generated_at DESC
		LIMIT 1`, brandName).Scan(
		&r.ID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
		&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
		&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
		&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount,
		&reportJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: 查询最新记录失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

// GetByID 查询单条审计记录的完整信息（含 report_json）。
func (d *DB) GetByID(ctx context.Context, id int64) (*Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	var r Record
	var reportJSON sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT
		id, brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
		FROM audit_history WHERE id = ?`, id).Scan(
		&r.ID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
		&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
		&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
		&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount,
		&reportJSON)
	if err != nil {
		return nil, fmt.Errorf("history: 查询记录失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

// Brands 列出所有有审计记录的品牌（用于前端下拉框）。
func (d *DB) Brands(ctx context.Context) ([]string, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT brand_name FROM audit_history ORDER BY brand_name`)
	if err != nil {
		return nil, fmt.Errorf("history: 查询品牌列表失败: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Stats 返回历史库统计信息。
func (d *DB) Stats(ctx context.Context) (Stats, error) {
	if d == nil || d.db == nil {
		return Stats{}, nil
	}
	var s Stats
	s.Path = d.path
	var oldest, newest sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT
		COUNT(*), COUNT(DISTINCT brand_name),
		MIN(generated_at), MAX(generated_at)
		FROM audit_history`).Scan(&s.Records, &s.Brands, &oldest, &newest)
	if err != nil && err != sql.ErrNoRows {
		return s, fmt.Errorf("history: 统计失败: %w", err)
	}
	s.OldestAt = oldest.Int64
	s.NewestAt = newest.Int64
	if fi, err := os.Stat(d.path); err == nil {
		s.FileSize = fi.Size()
	}
	return s, nil
}

// Clear 清空所有历史记录并 VACUUM。
func (d *DB) Clear(ctx context.Context) error {
	if d == nil || d.db == nil {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM audit_history`)
	if err != nil {
		return fmt.Errorf("history: 清空失败: %w", err)
	}
	_, _ = d.db.ExecContext(ctx, `VACUUM`)
	return nil
}

// DeleteOlderThan 删除指定天数之前的历史记录，返回删除条数。
func (d *DB) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	res, err := d.db.ExecContext(ctx, `DELETE FROM audit_history WHERE generated_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("history: 清理过期记录失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// --- JSON 辅助 ---

// MarshalReport 将完整 VisibilityReport 序列化为 JSON 字符串。
// 调用方传入任意结构体，这里只做 json.Marshal。
func MarshalReport(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
