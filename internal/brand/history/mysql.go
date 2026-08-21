package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"my-geo/internal/dbprovider"

	_ "github.com/go-sql-driver/mysql"
)

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
			dsn = "geo:geoPass@tcp(127.0.0.1:3306)/geo?parseTime=true&charset=utf8mb4&loc=Local&collation=utf8mb4_unicode_ci"
		}
	}
	dsn = dbprovider.NormalizeMySQLDSN(dsn)
	sqldb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("history: 打开 MySQL 失败: %w", err)
	}
	dbprovider.ConfigurePool(sqldb, "default")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("history: MySQL ping 失败: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, "SET NAMES utf8mb4, sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION', innodb_strict_mode=ON"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("history: MySQL 初始化会话失败: %w", err)
	}
	// 表结构由 deploy/initdb 初始化（02-schema.sql），应用内不再内嵌 migration。
	return &mysqlStore{path: dsn, db: sqldb}, nil
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
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("history/mysql: 读取自增 ID 失败: %w", err)
	}
	return id, nil
}

func (d *mysqlStore) List(ctx context.Context, brandName string, limit, offset int) ([]Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	// 使用 LIMIT ? OFFSET ? 把分页下推到 MySQL，避免取出全部后再内存切片。
	q := `SELECT
		id, COALESCE(workspace_id,''), brand_name, generated_at, score, grade, tier,
		entity_completeness, mention_rate, citation_rate, share_of_voice,
		citation_position, sentiment, entity_recognition,
		content_gaps_count, competitor_count, negative_count, action_count
		FROM audit_history WHERE brand_name = ?` + wsClause + `
		ORDER BY generated_at DESC
		LIMIT ? OFFSET ?`
	args := []interface{}{brandName}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	args = append(args, limit, offset)
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

// LatestForBrands 一次查询多个品牌各自的最新记录（P1-5：替代逐品牌 Latest 的 N+1）。
// 用 JOIN (SELECT brand_name, MAX(id) ... GROUP BY brand_name) 下推聚合，
// 单条 SQL 完成，网络往返从 O(N) 降到 O(1)。brandNames 为空时直接返回空。
func (d *mysqlStore) LatestForBrands(ctx context.Context, brandNames []string) ([]Record, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	if len(brandNames) == 0 {
		return nil, nil
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(brandNames)), ",")
	args := make([]interface{}, 0, len(brandNames)+1)
	for _, b := range brandNames {
		args = append(args, b)
	}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	q := `SELECT
		h.id, COALESCE(h.workspace_id,''), h.brand_name, h.generated_at, h.score, h.grade, h.tier,
		h.entity_completeness, h.mention_rate, h.citation_rate, h.share_of_voice,
		h.citation_position, h.sentiment, h.entity_recognition,
		h.content_gaps_count, h.competitor_count, h.negative_count, h.action_count,
		h.report_json
		FROM audit_history h
		JOIN (
			SELECT brand_name, MAX(id) AS max_id
			FROM audit_history
			WHERE brand_name IN (` + placeholders + `)` + wsClause + `
			GROUP BY brand_name
		) t ON h.id = t.max_id`
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: LatestForBrands 失败: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var reportJSON sql.NullString
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.BrandName, &r.Generated, &r.Score, &r.Grade, &r.Tier,
			&r.EntityCompleteness, &r.MentionRate, &r.CitationRate, &r.ShareOfVoice,
			&r.CitationPosition, &r.Sentiment, &r.EntityRecognition,
			&r.ContentGaps, &r.CompetitorCount, &r.NegativeCount, &r.ActionCount,
			&reportJSON); err != nil {
			return nil, fmt.Errorf("history/mysql: LatestForBrands 扫描失败: %w", err)
		}
		r.ReportJSON = reportJSON.String
		out = append(out, r)
	}
	return out, rows.Err()
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

// UpdateReport 原地更新记录的报告快照与标量（人工修正后重算落库）。
func (d *mysqlStore) UpdateReport(ctx context.Context, id int64, r Record) error {
	if d == nil || d.db == nil {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `UPDATE audit_history SET
		score=?, grade=?, tier=?, entity_completeness=?,
		mention_rate=?, citation_rate=?, share_of_voice=?, citation_position=?,
		sentiment=?, entity_recognition=?,
		content_gaps_count=?, competitor_count=?, negative_count=?, action_count=?,
		report_json=?
		WHERE id=?`,
		r.Score, r.Grade, r.Tier, r.EntityCompleteness,
		r.MentionRate, r.CitationRate, r.ShareOfVoice, r.CitationPosition,
		r.Sentiment, r.EntityRecognition,
		r.ContentGaps, r.CompetitorCount, r.NegativeCount, r.ActionCount,
		r.ReportJSON, id,
	)
	if err != nil {
		return fmt.Errorf("history/mysql: 更新记录 %d 失败: %w", id, err)
	}
	return nil
}

func (d *mysqlStore) Brands(ctx context.Context) ([]string, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	wid := WorkspaceFromContext(ctx)
	wsClause, wsArg := wsScope(wid)
	// 用 GROUP BY 代替 DISTINCT：对于带 (workspace_id, brand_name) 索引的列，MySQL 可直接走
	// 索引有序扫描，结果天然有序，因此还能省一次 filesort（比 DISTINCT + ORDER BY 更快）。
	q := `SELECT brand_name FROM audit_history WHERE 1=1` + wsClause + ` GROUP BY brand_name ORDER BY brand_name`
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
	cutoffEnd := now.AddDate(0, 0, 1).Unix()

	// 关键优化：把"每行一次网络往返 + Go 层按天 map 聚合"改为服务端 GROUP BY 聚合：
	//   SELECT DATE(FROM_UNIXTIME(generated_at)) day, COUNT(*), AVG(score)
	//   WHERE generated_at BETWEEN [start, end) AND workspace_id=?
	//   GROUP BY day
	// 网络传输从 O(N 审计行) 降到 O(days)，典型 30 天查询只需扫描 <=30 行结果。
	q := `
		SELECT
			DATE(FROM_UNIXTIME(generated_at)) AS day,
			COUNT(*) AS cnt,
			IFNULL(AVG(score), -1) AS avg_score
		FROM audit_history
		WHERE generated_at >= ? AND generated_at < ?` + wsClause + `
		GROUP BY day`
	args := []interface{}{cutoffStart, cutoffEnd}
	if wsArg != nil {
		args = append(args, wsArg)
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("history/mysql: DailyCounts 查询失败: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var date string
		var count int64
		var avgScore float64
		if err := rows.Scan(&date, &count, &avgScore); err != nil {
			return nil, fmt.Errorf("history/mysql: DailyCounts 扫描失败: %w", err)
		}
		if i, ok := keyMap[date]; ok {
			out[i].Count = count
			if count > 0 {
				out[i].AvgScore = avgScore
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("history/mysql: DailyCounts 行迭代失败: %w", err)
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
