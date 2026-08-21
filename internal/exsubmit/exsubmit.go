// Package exsubmit 外部系统提交的大模型对话采集与分析。
//
// 外部系统（如浏览器插件、Chat 前端、第三方应用）通过接口提交「大模型名称 / 问题 /
// 大模型回答 / 会话分享链接」，本模块负责：
//
//   - 入库：Save 落盘到 external_submissions 表（status=pending）。
//   - 定时分析：Worker 后台扫描 pending 记录，抽取结构化结论（情感 / 主题 /
//     来源域名 / 实体提及 / 摘要），写回 status=analyzed（失败则 failed）。
//   - 查询：List / ListPending / Stats 供管理后台与排查使用。
//
// 表结构由 deploy/initdb/schema.sql 初始化，应用内不内嵌迁移。
package exsubmit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"my-geo/internal/dbprovider"

	_ "github.com/go-sql-driver/mysql"
)


// Submission 一次外部提交（含分析结论）。
type Submission struct {
	ID            int64    `json:"id"`
	ModelName     string   `json:"model_name"`
	Question      string   `json:"question"`
	Answer        string   `json:"answer"`
	ShareLink     string   `json:"share_link"`
	Status        string   `json:"status"` // pending / analyzed / failed
	Summary       string   `json:"summary"`
	Sentiment     string   `json:"sentiment"`     // positive / neutral / negative
	Category      string   `json:"category"`
	Mentions      []string `json:"mentions"`       // 被提及的实体
	SourceDomains []string `json:"source_domains"` // 回答内引用的来源域名
	AnalysisJSON  string   `json:"analysis_json"`  // 完整结构化结果（JSON 文本）
	ErrorMsg      string   `json:"error_msg"`
	WorkspaceID   string   `json:"workspace_id,omitempty"`
	CreatedAt     int64    `json:"created_at"`
	AnalyzedAt    int64    `json:"analyzed_at"`
}

// Store 外部提交存储接口。
type Store interface {
	Close() error
	Path() string
	// Save 写入一条新提交，返回自增 ID。
	Save(ctx context.Context, sub *Submission) (int64, error)
	// ListPending 取最多 limit 条待分析记录（status='pending'）。
	ListPending(ctx context.Context, limit int) ([]*Submission, error)
	// Get 按 ID 查询。
	Get(ctx context.Context, id int64) (*Submission, error)
	// List 按状态过滤列出（status 为空=全部；limit<=0 默认 50）。
	List(ctx context.Context, status string, limit int) ([]*Submission, error)
	// UpdateAnalysis 写回分析结果并置状态。analysis 为 nil 时代表分析失败（写 errorMsg）。
	UpdateAnalysis(ctx context.Context, id int64, analysis *Analysis, status, errMsg string, analyzedAt int64) error
	// Stats 统计总数 / 待分析 / 已分析。
	Stats(ctx context.Context) (total, pending, analyzed int64, err error)
}

// mysqlStore MySQL 实现。
type mysqlStore struct {
	path string
	db   *sql.DB
}

// Open 打开外部提交 MySQL 库并连接。
//
// path 语义：非空时 path 即为 DSN；空时读 GEO_MYSQL_DSN，缺省为内置默认 DSN。
func Open(path string) (Store, error) {
	var dsn string
	if path != "" {
		dsn = path
	} else if env := os.Getenv("GEO_MYSQL_DSN"); env != "" {
		dsn = env
	} else {
		dsn = "geo:geoPass@tcp(127.0.0.1:3306)/geo?parseTime=true&charset=utf8mb4&loc=Local&collation=utf8mb4_unicode_ci"
	}
	dsn = dbprovider.NormalizeMySQLDSN(dsn)
	sqldb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("exsubmit: 打开 MySQL 失败: %w", err)
	}
	dbprovider.ConfigurePool(sqldb, "default")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("exsubmit: MySQL ping 失败: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, "SET NAMES utf8mb4"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("exsubmit: MySQL 初始化会话失败: %w", err)
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

// Path 返回 DSN。
func (d *mysqlStore) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Save 写入新提交。
func (d *mysqlStore) Save(ctx context.Context, sub *Submission) (int64, error) {
	if d == nil || d.db == nil {
		return 0, fmt.Errorf("exsubmit: 存储未初始化")
	}
	const q = `INSERT INTO external_submissions(
		model_name, question, answer, share_link, status, workspace_id, created_at
	) VALUES(?,?,?,?,?,?,?)`
	res, err := d.db.ExecContext(ctx, q,
		sub.ModelName, sub.Question, sub.Answer, sub.ShareLink,
		ternaryStr(sub.Status == "", "pending", sub.Status), sub.WorkspaceID, sub.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("exsubmit: 写入提交失败: %w", err)
	}
	return res.LastInsertId()
}

// ListPending 取最多 limit 条待分析记录。
func (d *mysqlStore) ListPending(ctx context.Context, limit int) ([]*Submission, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT id, model_name, question, answer, share_link, status,
		summary, sentiment, category, mentions, source_domains, analysis_json,
		error_msg, workspace_id, created_at, analyzed_at
		FROM external_submissions WHERE status='pending' ORDER BY created_at ASC LIMIT ?`
	rows, err := d.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("exsubmit: 查询待分析失败: %w", err)
	}
	defer rows.Close()
	return scanSubs(rows)
}

// Get 按 ID 查询。
func (d *mysqlStore) Get(ctx context.Context, id int64) (*Submission, error) {
	const q = `SELECT id, model_name, question, answer, share_link, status,
		summary, sentiment, category, mentions, source_domains, analysis_json,
		error_msg, workspace_id, created_at, analyzed_at
		FROM external_submissions WHERE id=?`
	rows, err := d.db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("exsubmit: 查询失败: %w", err)
	}
	defer rows.Close()
	subs, err := scanSubs(rows)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	return subs[0], nil
}

// List 按状态过滤列出。
func (d *mysqlStore) List(ctx context.Context, status string, limit int) ([]*Submission, error) {
	if limit <= 0 {
		limit = 50
	}
	var q strings.Builder
	q.WriteString(`SELECT id, model_name, question, answer, share_link, status,
		summary, sentiment, category, mentions, source_domains, analysis_json,
		error_msg, workspace_id, created_at, analyzed_at
		FROM external_submissions`)
	args := []interface{}{}
	if status != "" {
		q.WriteString(" WHERE status=?")
		args = append(args, status)
	}
	q.WriteString(" ORDER BY created_at DESC LIMIT ?")
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("exsubmit: 列表查询失败: %w", err)
	}
	defer rows.Close()
	return scanSubs(rows)
}

// UpdateAnalysis 写回分析结果并置状态。
func (d *mysqlStore) UpdateAnalysis(ctx context.Context, id int64, analysis *Analysis, status, errMsg string, analyzedAt int64) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("exsubmit: 存储未初始化")
	}
	summary, sentiment, category, mentions, domains, analysisJSON := "", "", "", "null", "null", ""
	if analysis != nil {
		summary = analysis.Summary
		sentiment = analysis.Sentiment
		category = analysis.Category
		if b, err := json.Marshal(analysis.Mentions); err == nil {
			mentions = string(b)
		}
		if b, err := json.Marshal(analysis.SourceDomains); err == nil {
			domains = string(b)
		}
		if b, err := json.Marshal(analysis); err == nil {
			analysisJSON = string(b)
		}
	}
	const q = `UPDATE external_submissions SET status=?, summary=?, sentiment=?, category=?,
		mentions=?, source_domains=?, analysis_json=?, error_msg=?, analyzed_at=? WHERE id=?`
	if _, err := d.db.ExecContext(ctx, q,
		status, summary, sentiment, category, mentions, domains, analysisJSON,
		errMsg, analyzedAt, id); err != nil {
		return fmt.Errorf("exsubmit: 更新分析结果失败: %w", err)
	}
	return nil
}

// Stats 统计。
func (d *mysqlStore) Stats(ctx context.Context) (total, pending, analyzed int64, err error) {
	if d == nil || d.db == nil {
		return 0, 0, 0, fmt.Errorf("exsubmit: 存储未初始化")
	}
	if err = d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM external_submissions").Scan(&total); err != nil {
		return
	}
	if err = d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM external_submissions WHERE status='pending'").Scan(&pending); err != nil {
		return
	}
	if err = d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM external_submissions WHERE status='analyzed'").Scan(&analyzed); err != nil {
		return
	}
	return
}

// scanSubs 扫描多行提交记录（共享于 Get/List/ListPending）。
func scanSubs(rows *sql.Rows) ([]*Submission, error) {
	var out []*Submission
	for rows.Next() {
		var s Submission
		var mentions, domains sql.NullString
		if err := rows.Scan(
			&s.ID, &s.ModelName, &s.Question, &s.Answer, &s.ShareLink, &s.Status,
			&s.Summary, &s.Sentiment, &s.Category, &mentions, &domains, &s.AnalysisJSON,
			&s.ErrorMsg, &s.WorkspaceID, &s.CreatedAt, &s.AnalyzedAt,
		); err != nil {
			return nil, fmt.Errorf("exsubmit: 扫描失败: %w", err)
		}
		if mentions.Valid && mentions.String != "" && mentions.String != "null" {
			_ = json.Unmarshal([]byte(mentions.String), &s.Mentions)
		}
		if domains.Valid && domains.String != "" && domains.String != "null" {
			_ = json.Unmarshal([]	byte(domains.String), &s.SourceDomains)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
