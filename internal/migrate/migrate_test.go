package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestSplitStatements(t *testing.T) {
	content := `
-- 注释行
CREATE TABLE IF NOT EXISTS t1 (id INT);
# 另一注释
CREATE INDEX idx_t1 ON t1(id);
CREATE TABLE t2 (id INT)

`
	stmts := splitStatements(content)
	if len(stmts) != 3 {
		t.Fatalf("应切出 3 条语句, got %d: %v", len(stmts), stmts)
	}
	if !strings.HasPrefix(stmts[0], "CREATE TABLE") {
		t.Fatalf("语句 0 应为 CREATE TABLE, got %q", stmts[0])
	}
	if !strings.Contains(stmts[2], "t2") {
		t.Fatalf("语句 2 应包含 t2, got %q", stmts[2])
	}
}

func TestMigrateUnknownDir(t *testing.T) {
	db, err := sql.Open("mysql", "root:@tcp(127.0.0.1:3306)/")
	if err != nil {
		t.Skipf("跳过：无法打开 MySQL (%v)", err)
	}
	defer db.Close()
	if _, err := Migrate(context.Background(), db, "no-such-dir"); err == nil {
		t.Fatal("未知目录应返回错误")
	}
}

// openTestDB 创建一次性测试库并返回连接与清理函数；MySQL 不可用时跳过。
func openTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	rootDSN := strings.TrimSpace(os.Getenv("GEO_TEST_MYSQL_ROOT_DSN"))
	if rootDSN == "" {
		rootDSN = "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true&tls=false"
	}
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Skipf("跳过 migrate 集成测试：无法打开 MySQL (%v)", err)
	}
	if err := root.Ping(); err != nil {
		root.Close()
		t.Skipf("跳过 migrate 集成测试：MySQL 不可用 (%v)", err)
	}
	dbName := fmt.Sprintf("geo_migrate_test_%d_%s", os.Getpid(), time.Now().Format("150405.000000"))
	if _, err := root.Exec("CREATE DATABASE IF NOT EXISTS `" + dbName + "` DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"); err != nil {
		root.Close()
		t.Skipf("跳过 migrate 集成测试：建库失败 (%v)", err)
	}
	db, err := sql.Open("mysql", injectDB(rootDSN, dbName))
	if err != nil {
		root.Close()
		t.Fatalf("打开测试库失败: %v", err)
	}
	cleanup := func() {
		db.Close()
		_, _ = root.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")
		root.Close()
	}
	return db, cleanup
}

func injectDB(rootDSN, dbName string) string {
	// root DSN 形如 user:pass@tcp(host:port)/?params → 替换路径为 dbName
	slash := strings.Index(rootDSN, "/")
	if slash < 0 {
		return rootDSN
	}
	base := rootDSN[:slash+1]
	params := ""
	if q := strings.Index(rootDSN[slash+1:], "?"); q >= 0 {
		params = rootDSN[slash+1+q:]
	}
	return base + dbName + params
}

func TestMigrateAppliesAndIdempotent(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 首次应用：应返回 0001_init.sql
	applied, err := Migrate(ctx, db, "auth")
	if err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	if len(applied) != 1 || applied[0] != "0001_init.sql" {
		t.Fatalf("应恰好应用 0001_init.sql, got %v", applied)
	}

	// 验证表已创建
	for _, tbl := range []string{"users", "workspaces", "memberships", "refresh_tokens", "admin_audit_log"} {
		var cnt int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?",
			tbl).Scan(&cnt); err != nil {
			t.Fatalf("查询表 %s: %v", tbl, err)
		}
		if cnt != 1 {
			t.Fatalf("表 %s 应存在", tbl)
		}
	}

	// 再次应用：幂等，不重复
	applied2, err := Migrate(ctx, db, "auth")
	if err != nil {
		t.Fatalf("二次迁移失败: %v", err)
	}
	if len(applied2) != 0 {
		t.Fatalf("重复迁移应无新应用, got %v", applied2)
	}

	// 校验 schema_migrations 记录
	var versions int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatalf("查询 schema_migrations: %v", err)
	}
	if versions != 1 {
		t.Fatalf("schema_migrations 应有 1 条记录, got %d", versions)
	}
}

func TestMigrateAllDirs(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, dir := range []string{"auth", "offline", "history", "chinacheck"} {
		applied, err := Migrate(ctx, db, dir)
		if err != nil {
			t.Fatalf("迁移 %s 失败: %v", dir, err)
		}
		if len(applied) != 1 {
			t.Fatalf("迁移 %s 应恰好应用 1 个文件, got %v", dir, applied)
		}
	}
}
