package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const sqlSchemaSQLite = `
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

// sqliteStore SQLite 实现的审计历史存储（零依赖默认后端）。
type sqliteStore struct {
	path string
	db   *sql.DB
}

// Open 打开/创建 SQLite 历史数据库并完成 schema 初始化。
// 保持与原实现完全一致的签名：path 为空时使用默认路径 ~/.local/share/geo/geo_brand_history.db。
//
// 返回值类型：兼容别名 DB（= Store）。上游代码无需任何修改。
func Open(path string) (Store, error) {
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
		return nil, fmt.Errorf("history: 打开 SQLite 失败: %w", err)
	}
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	if _, err := sqldb.Exec(sqlSchemaSQLite); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("history: SQLite schema 初始化失败: %w", err)
	}
	return &sqliteStore{path: path, db: sqldb}, nil
}

// Close 关闭底层数据库。
func (d *sqliteStore) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Path 返回 SQLite 文件路径。
func (d *sqliteStore) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Save 写入一条审计快照。
func (d *sqliteStore) Save(ctx context.Context, r Record) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	if r.Generated == 0 {
		r.Generated = TimeNow().Unix()
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
		return 0, fmt.Errorf("history/sqlite: 写入失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (d *sqliteStore) List(ctx context.Context, brandName string, limit int) ([]Record, error) {
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
		return nil, fmt.Errorf("history/sqlite: List 失败: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
			&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
			&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
			&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount); err != nil {
			return nil, fmt.Errorf("history/sqlite: 扫描行失败: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *sqliteStore) Latest(ctx context.Context, brandName string) (*Record, error) {
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history/sqlite: Latest 失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

func (d *sqliteStore) GetByID(ctx context.Context, id int64) (*Record, error) {
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
		return nil, fmt.Errorf("history/sqlite: GetByID 失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

func (d *sqliteStore) Brands(ctx context.Context) ([]string, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT brand_name FROM audit_history ORDER BY brand_name`)
	if err != nil {
		return nil, fmt.Errorf("history/sqlite: Brands 失败: %w", err)
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

func (d *sqliteStore) Stats(ctx context.Context) (Stats, error) {
	if d == nil || d.db == nil {
		return Stats{Backend: "sqlite"}, nil
	}
	var s Stats
	s.Path = d.path
	s.Backend = "sqlite"
	var oldest, newest sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT
		COUNT(*), COUNT(DISTINCT brand_name),
		MIN(generated_at), MAX(generated_at)
		FROM audit_history`).Scan(&s.Records, &s.Brands, &oldest, &newest)
	if err != nil && err != sql.ErrNoRows {
		return s, fmt.Errorf("history/sqlite: Stats 失败: %w", err)
	}
	s.OldestAt = oldest.Int64
	s.NewestAt = newest.Int64
	if fi, err := os.Stat(d.path); err == nil {
		s.FileSize = fi.Size()
	}
	return s, nil
}

func (d *sqliteStore) Clear(ctx context.Context) error {
	if d == nil || d.db == nil {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM audit_history`)
	if err != nil {
		return fmt.Errorf("history/sqlite: Clear 失败: %w", err)
	}
	_, _ = d.db.ExecContext(ctx, `VACUUM`)
	return nil
}

func (d *sqliteStore) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	cutoff := TimeNow().AddDate(0, 0, -days).Unix()
	res, err := d.db.ExecContext(ctx, `DELETE FROM audit_history WHERE generated_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("history/sqlite: DeleteOlderThan 失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// 编译期接口符合性断言（保证 sqliteStore 完整实现 Store）。
var _ Store = (*sqliteStore)(nil)
