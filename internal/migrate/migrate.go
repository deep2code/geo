// Package migrate 提供零第三方依赖的极简数据库迁移工具。
//
// 设计取舍（与 MySQL 语义对齐）：
//   - 每个业务库一个迁移目录：sql/<db>/NNNN_description.sql；
//   - 迁移文件按文件名升序应用；已应用的版本记录在 schema_migrations 表（幂等）；
//   - 每个迁移文件在 MySQL 中**不是**事务性的——MySQL DDL 会隐式提交，
//     无法回滚。因此迁移语句必须幂等（CREATE TABLE IF NOT EXISTS / CREATE INDEX
//     IF NOT EXISTS 或容忍"已存在"错误），失败时不会记录版本，下次启动自动重试；
//   - 校验已应用版本的 SHA-256 checksum：迁移文件事后被修改会立即报错，
//     防止"同一版本两种内容"的静默漂移。
//
// 用法：
//
//	err := migrate.Migrate(ctx, db, "auth") // 应用 sql/auth 下的迁移
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"
)

//go:embed sql
var sqlFS embed.FS

// 迁移记录表：与业务表同库，避免跨库依赖。
const createMigrationTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    VARCHAR(255) PRIMARY KEY,
	checksum   CHAR(64)  NOT NULL COMMENT '迁移文件 SHA-256',
	applied_at BIGINT    NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// Migrate 对 db 应用 dir 目录下所有未执行的迁移，返回本次应用的版本列表。
//
// dir 必须是 migrate 包内嵌 sql 目录下的子目录名（如 "auth" / "offline" / "history" / "chinacheck"）。
func Migrate(ctx context.Context, db *sql.DB, dir string) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("migrate: db 为 nil")
	}
	entries, err := fs.ReadDir(sqlFS, "sql/"+dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: 读取迁移目录 %q: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, nil
	}

	if _, err := db.ExecContext(ctx, createMigrationTable); err != nil {
		return nil, fmt.Errorf("migrate: 创建 schema_migrations 表: %w", err)
	}

	// 已应用版本 → checksum
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	var appliedNow []string
	for _, f := range files {
		content, err := sqlFS.ReadFile("sql/" + dir + "/" + f)
		if err != nil {
			return nil, fmt.Errorf("migrate: 读取 %s: %w", f, err)
		}
		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])

		if prev, ok := applied[f]; ok {
			if prev != checksum {
				return nil, fmt.Errorf("migrate: 迁移 %s 已应用但内容被修改（checksum 不匹配）。"+
					"禁止原地修改历史迁移，请新增 NNNN_*.sql", f)
			}
			continue // 已应用且未改动
		}

		if err := applyFile(ctx, db, f, string(content)); err != nil {
			return nil, fmt.Errorf("migrate: 应用 %s 失败: %w", f, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (?, ?, ?)`,
			f, checksum, time.Now().UnixMilli()); err != nil {
			return nil, fmt.Errorf("migrate: 记录版本 %s: %w", f, err)
		}
		appliedNow = append(appliedNow, f)
		slog.Info("migrate applied",
			slog.String("db", dir),
			slog.String("version", f),
		)
	}
	return appliedNow, nil
}

// appliedVersions 返回已应用版本 → checksum 映射。
func appliedVersions(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: 查询已应用版本: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			return nil, fmt.Errorf("migrate: 扫描版本记录: %w", err)
		}
		out[v] = c
	}
	return out, rows.Err()
}

// applyFile 逐条执行迁移文件中的 SQL 语句。
//
// 按分号切分语句（约定：迁移文件内字符串字面量不含分号）；
// 任何一条失败立即中止，不记录版本，下次启动重试。
func applyFile(ctx context.Context, db *sql.DB, name, content string) error {
	for _, stmt := range splitStatements(content) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: 语句执行失败: %w\nSQL: %s", name, err, truncateSQL(stmt))
		}
	}
	return nil
}

// splitStatements 按分号切分 SQL，过滤空语句与整行注释。
func splitStatements(content string) []string {
	var out []string
	for _, s := range strings.Split(content, ";") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// 跳过纯注释行块（-- 或 #）
		lines := strings.Split(s, "\n")
		kept := make([]string, 0, len(lines))
		for _, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") {
				continue
			}
			kept = append(kept, ln)
		}
		body := strings.TrimSpace(strings.Join(kept, "\n"))
		if body != "" {
			out = append(out, body)
		}
	}
	return out
}

// truncateSQL 截断 SQL 用于错误信息（防止超长语句刷屏）。
func truncateSQL(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[截断]"
}
