package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const sqlSchemaMySQL = `
CREATE TABLE IF NOT EXISTS audit_history (
	id                    BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
	workspace_id          VARCHAR(255),
	brand_name            VARCHAR(255) NOT NULL,
	generated_at          BIGINT       NOT NULL,
	score                 DOUBLE       NOT NULL,
	grade                 VARCHAR(255) NOT NULL,
	tier                  VARCHAR(255) NOT NULL,
	entity_completeness   DOUBLE       NOT NULL DEFAULT 0,
	mention_rate          DOUBLE       NOT NULL DEFAULT 0,
	citation_rate         DOUBLE       NOT NULL DEFAULT 0,
	share_of_voice        DOUBLE       NOT NULL DEFAULT 0,
	citation_position     DOUBLE       NOT NULL DEFAULT 0,
	sentiment             DOUBLE       NOT NULL DEFAULT 0,
	entity_recognition    DOUBLE       NOT NULL DEFAULT 0,
	content_gaps_count    INT          NOT NULL DEFAULT 0,
	competitor_count      INT          NOT NULL DEFAULT 0,
	negative_count        INT          NOT NULL DEFAULT 0,
	action_count          INT          NOT NULL DEFAULT 0,
	report_json           MEDIUMTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE INDEX idx_history_ws_brand_time ON audit_history(workspace_id, brand_name, generated_at);
CREATE INDEX idx_history_ws_time ON audit_history(workspace_id, generated_at);
CREATE INDEX idx_history_brand_time ON audit_history(brand_name, generated_at);
CREATE INDEX idx_history_time ON audit_history(generated_at);
`

// mysqlStore MySQL 实现的审计历史存储。
type mysqlStore struct {
	path string
	db   *sql.DB
}

// Open 打开/创建 MySQL 历史数据库并完成 schema 初始化。
// path 语义：非空时 path 即为 DSN；空时走 GEO_HISTORY_MYSQL_DSN 环境变量或默认 DSN。
//
// 返回值类型：兼容别名 DB（= Store）。上游代码无需任何修改。
func Open(path string) (Store, error) {
	var dsn string
	if path != "" {
		dsn = path
	} else {
		if envDSN := os.Getenv("GEO_HISTORY_MYSQL_DSN"); envDSN != "" {
			dsn = envDSN
		} else {
			dsn = "geo_history:geo_history_pass@tcp(127.0.0.1:3306)/geo_history?parseTime=true&charset=utf8mb4&loc=Local&collation=utf8mb4_unicode_ci&tls=false"
		}
	}
	sqldb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("history: 打开 MySQL 失败: %w", err)
	}
	sqldb.SetMaxOpenConns(128)
	sqldb.SetMaxIdleConns(32)
	sqldb.SetConnMaxLifetime(30 * time.Minute)
	if _, err := sqldb.Exec("SET NAMES utf8mb4, sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION', innodb_strict_mode=ON"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("history: MySQL 初始化会话失败: %w", err)
	}
	stmts := strings.Split(sqlSchemaMySQL, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := runDDL(sqldb, stmt); err != nil {
			sqldb.Close()
			return nil, fmt.Errorf("history: MySQL schema 初始化失败: %w", err)
		}
	}
	return &mysqlStore{path: dsn, db: sqldb}, nil
}

// runDDL 执行一条 DDL；索引重复等"已存在"错误静默处理。
func runDDL(db *sql.DB, ddl string) error {
	_, err := db.Exec(ddl)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "duplicate key name") || strings.Contains(msg, "already exists") {
		return nil
	}
	return err
}

// columnExists 判断 MySQL 表中是否存在指定列。
func columnExists(db *sql.DB, table string, column string) (bool, error) {
	var cnt int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`,
		table, column,
	).Scan(&cnt)
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// Close 关闭底层数据库。
func (d *mysqlStore) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// Path 返回当前 DSN 字符串（作为 MySQL 后端的 Path 表示）。
func (d *mysqlStore) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// wsScope 构造 workspace_id 过滤子句；wid 为空时不过滤（兼容旧行为）。
func wsScope(wid string) (string, interface{}) {
	if wid == "" {
		return "", nil
	}
	return " AND workspace_id = ?", wid
}

// Save 写入一条审计快照。
func (d *mysqlStore) Save(ctx context.Context, r Record) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	if r.Generated == 0 {
		r.Generated = TimeNow().Unix()
	}
	// 若调用方未显式给 Record.WorkspaceID，则自动继承 context 中的 workspace（兼容旧代码）。
	if r.WorkspaceID == "" {
		r.WorkspaceID = WorkspaceFromContext(ctx)
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO audit_history(
		workspace_id, brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sqlNullStr(r.WorkspaceID), r.BrandName, r.Generated, r.Score, r.Grade, r.Tier,
		r.EntityCompleteness, r.MentionRate, r.CitationRate, r.ShareOfVoice,
		r.CitationPosition, r.Sentiment, r.EntityRecognition,
		r.ContentGaps, r.CompetitorCount, r.NegativeCount, r.ActionCount,
		r.ReportJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("history/mysql: 写入失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (d *mysqlStore) List(ctx context.Context, brandName string, limit int) ([]Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	q := `SELECT
		id, COALESCE(workspace_id,''), brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count
		FROM audit_history WHERE brand_name = ?` + wsClause + `
		ORDER BY generated_at DESC
		LIMIT ?`
	args := []interface{}{brandName}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: List 失败: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
			&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
			&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
			&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount); err != nil {
			return nil, fmt.Errorf("history/mysql: 扫描行失败: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *mysqlStore) Latest(ctx context.Context, brandName string) (*Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	q := `SELECT
		id, COALESCE(workspace_id,''), brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
		FROM audit_history WHERE brand_name = ?` + wsClause + `
		ORDER BY generated_at DESC
		LIMIT 1`
	args := []interface{}{brandName}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	var r Record
	var reportJSON sql.NullString
	err := d.db.QueryRowContext(ctx, q, args...).Scan(
		&r.ID, &r.WorkspaceID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
		&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
		&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
		&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount,
		&reportJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history/mysql: Latest 失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

func (d *mysqlStore) GetByID(ctx context.Context, id int64) (*Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	var r Record
	var reportJSON sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT
		id, COALESCE(workspace_id,''), brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count,
		report_json
		FROM audit_history WHERE id = ?`, id).Scan(
		&r.ID, &r.WorkspaceID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
		&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
		&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
		&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount,
		&reportJSON)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: GetByID 失败: %w", err)
	}
	r.ReportJSON = reportJSON.String
	return &r, nil
}

func (d *mysqlStore) Brands(ctx context.Context) ([]string, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	q := `SELECT DISTINCT brand_name FROM audit_history WHERE 1=1` + wsClause + ` ORDER BY brand_name`
	var args []interface{}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: Brands 失败: %w", err)
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

func (d *mysqlStore) Stats(ctx context.Context) (Stats, error) {
	if d == nil || d.db == nil {
		return Stats{Backend: "mysql"}, nil
	}
	var s Stats
	s.Path = d.path
	s.Backend = "mysql"
	var oldest, newest sql.NullInt64
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	q := `SELECT
		COUNT(*), COUNT(DISTINCT brand_name),
		MIN(generated_at), MAX(generated_at)
		FROM audit_history WHERE 1=1` + wsClause
	var args []interface{}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	err := d.db.QueryRowContext(ctx, q, args...).Scan(&s.Records, &s.Brands, &oldest, &newest)
	if err != nil && err != sql.ErrNoRows {
		return s, fmt.Errorf("history/mysql: Stats 失败: %w", err)
	}
	s.OldestAt = oldest.Int64
	s.NewestAt = newest.Int64
	return s, nil
}

func (d *mysqlStore) Clear(ctx context.Context) error {
	if d == nil || d.db == nil {
		return nil
	}
	wid := WorkspaceFromContext(ctx)
	if wid == "" {
		if _, err := d.db.ExecContext(ctx, `DELETE FROM audit_history`); err != nil {
			return fmt.Errorf("history/mysql: Clear 失败: %w", err)
		}
		return nil
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM audit_history WHERE workspace_id = ?`, wid); err != nil {
		return fmt.Errorf("history/mysql: Clear 失败: %w", err)
	}
	return nil
}

func (d *mysqlStore) DailyCounts(ctx context.Context, days int) ([]DailyBucket, error) {
	if days <= 0 {
		days = 30
	}
	now := TimeNow()
	loc := now.Location()

	// 构造从"今天 - (days-1)"到"今天"的日期桶，全填 0。
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	out := make([]DailyBucket, 0, days)
	keyMap := make(map[string]int, days) // date -> index
	for i := 0; i < days; i++ {
		t := start.AddDate(0, 0, i)
		date := t.Format("2006-01-02")
		out = append(out, DailyBucket{Date: date, Count: 0, AvgScore: -1})
		keyMap[date] = i
	}

	if d == nil || d.db == nil {
		return out, nil
	}

	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	cutoffStart := start.Unix()
	q := `
		SELECT generated_at, score FROM audit_history
		WHERE generated_at >= ? AND generated_at < ?` + wsClause
	args := []interface{}{cutoffStart, now.AddDate(0, 0, 1).Unix()}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: DailyCounts 查询失败: %w", err)
	}
	defer rows.Close()

	type agg struct {
		sum   float64
		count int64
	}
	buckets := make(map[string]*agg, days)

	for rows.Next() {
		var ga int64
		var score float64
		if err := rows.Scan(&ga, &score); err != nil {
			return nil, fmt.Errorf("history/mysql: DailyCounts 扫描失败: %w", err)
		}
		date := time.Unix(ga, 0).In(loc).Format("2006-01-02")
		b, ok := buckets[date]
		if !ok {
			b = &agg{}
			buckets[date] = b
		}
		b.count++
		b.sum += score
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("history/mysql: DailyCounts 行迭代失败: %w", err)
	}

	for date, a := range buckets {
		if i, ok := keyMap[date]; ok {
			out[i].Count = a.count
			if a.count > 0 {
				out[i].AvgScore = a.sum / float64(a.count)
			}
		}
	}
	return out, nil
}

func (d *mysqlStore) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	cutoff := TimeNow().AddDate(0, 0, -days).Unix()
	wid := WorkspaceFromContext(ctx)
	var (
		res sql.Result
		err error
	)
	if wid == "" {
		res, err = d.db.ExecContext(ctx, `DELETE FROM audit_history WHERE generated_at < ?`, cutoff)
	} else {
		res, err = d.db.ExecContext(ctx, `DELETE FROM audit_history WHERE generated_at < ? AND workspace_id = ?`, cutoff, wid)
	}
	if err != nil {
		return 0, fmt.Errorf("history/mysql: DeleteOlderThan 失败: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func sqlNullStr(s string) interface{} {
	if s == "" {
		return sql.NullString{String: "", Valid: false}
	}
	return s
}

// 编译期接口符合性断言（保证 mysqlStore 完整实现 Store）。
var _ Store = (*mysqlStore)(nil)
