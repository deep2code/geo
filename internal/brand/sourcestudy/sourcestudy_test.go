package sourcestudy

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

// sourceStudyTestRootDSN 获取测试用 root DSN；MySQL 不可用时跳过。
func sourceStudyTestRootDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GEO_TEST_MYSQL_ROOT_DSN"))
	if dsn == "" {
		dsn = "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true&tls=false"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("跳过 sourcestudy 测试：无法打开 MySQL (%v)", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("跳过 sourcestudy 测试：MySQL 不可用 (ping err=%v)", err)
	}
	return dsn
}

// createSourceStudyTestTable 在指定库创建测试表（DDL 与 deploy/initdb/schema.sql 一致）。
func createSourceStudyTestTable(t *testing.T, db *sql.DB, dbName string) {
	t.Helper()
	// 安全校验：数据库名只允许字母、数字、下划线（防止 SQL 注入）
	for _, r := range dbName {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			t.Fatalf("非法数据库名 %q：只允许字母、数字、下划线", dbName)
		}
	}
	ddl := `CREATE TABLE IF NOT EXISTS engine_source_citations (
		id              BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
		workspace_id    VARCHAR(255),
		engine          VARCHAR(32)   NOT NULL,
		source_domain   VARCHAR(255)  NOT NULL,
		source_category VARCHAR(32)   NOT NULL DEFAULT 'other',
		brand_name      VARCHAR(255)  NOT NULL,
		prompt          VARCHAR(255)  NOT NULL DEFAULT '',
		record_id       BIGINT UNSIGNED NOT NULL,
		result_index    INT           NOT NULL DEFAULT 0,
		citation_url    VARCHAR(1024) NOT NULL,
		cited_at        BIGINT        NOT NULL,
		UNIQUE KEY uq_src_record_result_url (record_id, result_index, citation_url(255))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName)); err != nil {
		t.Fatalf("建库失败: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf("USE %s", dbName)); err != nil {
		t.Fatalf("切库失败: %v", err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
}

func TestMySQLStore(t *testing.T) {
	rootDSN := sourceStudyTestRootDSN(t)
	dbName := "test_sourcestudy_" + fmt.Sprint(time.Now().UnixNano()%1000000)
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName)) //nolint:errcheck

	createSourceStudyTestTable(t, root, dbName)

	// 打开模块存储（连到临时库）
	dsn := injectDB(rootDSN, dbName)
	store, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	// 两条审计记录（模拟 8/20、8/21 两次审计，chatgpt 与 kimi 两个引擎）。
	recs := []CitationRec{
		{Engine: "chatgpt", SourceDomain: "g2.com", SourceCategory: "review_site", BrandName: "Acme", Prompt: "best crm", RecordID: 1, ResultIndex: 0, CitationURL: "https://g2.com/a", CitedAt: 1787184000},
		{Engine: "chatgpt", SourceDomain: "zhihu.com", SourceCategory: "social", BrandName: "Acme", Prompt: "best crm", RecordID: 1, ResultIndex: 0, CitationURL: "https://zhihu.com/q/1", CitedAt: 1787184000},
		{Engine: "kimi", SourceDomain: "g2.com", SourceCategory: "review_site", BrandName: "Acme", Prompt: "best crm", RecordID: 1, ResultIndex: 0, CitationURL: "https://g2.com/a", CitedAt: 1787184000},
		{Engine: "chatgpt", SourceDomain: "g2.com", SourceCategory: "review_site", BrandName: "Acme", Prompt: "best crm", RecordID: 2, ResultIndex: 0, CitationURL: "https://g2.com/b", CitedAt: 1787270400},
		{Engine: "chatgpt", SourceDomain: "medium.com", SourceCategory: "blog", BrandName: "Acme", Prompt: "best crm", RecordID: 2, ResultIndex: 1, CitationURL: "https://medium.com/x", CitedAt: 1787270400},
	}
	// 幂等验证：重复调用不产生新行。
	if err := store.Record(ctx, recs); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(ctx, recs); err != nil {
		t.Fatal(err)
	}

	// 1. TopSources（全部引擎）：g2.com 3 次、zhihu/medium 各 1 次。
	top, err := store.TopSources(ctx, StudyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top[0].SourceDomain != "g2.com" || top[0].CitationCount != 3 {
		t.Fatalf("TopSources 全量错误: %+v", top)
	}
	if top[0].SharePercent != 60 { // 3/5
		t.Fatalf("share_percent 应为 60: %.2f", top[0].SharePercent)
	}

	// 2. 按引擎过滤：chatgpt 下 g2.com 2 次。
	topC, err := store.TopSources(ctx, StudyFilter{Engine: "chatgpt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(topC) != 3 || topC[0].CitationCount != 2 {
		t.Fatalf("TopSources(chatgpt) 错误: %+v", topC)
	}

	// 3. 按引擎 + limit。
	topL, err := store.TopSources(ctx, StudyFilter{Engine: "chatgpt", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(topL) != 1 {
		t.Fatalf("limit 应生效: %+v", topL)
	}

	// 4. Trend：chatgpt 8/20 与 8/21 各 1 条。
	trend, err := store.Trend(ctx, StudyFilter{Engine: "chatgpt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 2 {
		t.Fatalf("Trend 应有 2 天数据: %+v", trend)
	}
	// 按域名过滤 trend。
	trendG2, err := store.Trend(ctx, StudyFilter{Engine: "chatgpt", Domain: "g2.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trendG2) != 2 {
		t.Fatalf("g2.com 趋势应有 2 天: %+v", trendG2)
	}

	// 5. EngineCompare：chatgpt 与 kimi 各带 Top 来源。
	cmp, err := store.EngineCompare(ctx, StudyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp) != 2 {
		t.Fatalf("应有 2 个引擎: %+v", cmp)
	}
	for _, ec := range cmp {
		if ec.TotalCitations == 0 || len(ec.TopSources) == 0 {
			t.Fatalf("引擎 %s 应有总数与 Top 来源: %+v", ec.Engine, ec)
		}
	}

	// 6. Clear。
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.TopSources(ctx, StudyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("Clear 后应无数据: %+v", after)
	}
}

// injectDB 把 DSN 中的库名替换为目标库（仿 offlinedb 测试工具）。
func injectDB(dsn, db string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx < 0 {
		return dsn + db
	}
	rest := dsn[idx+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		return dsn[:idx+1] + db + "?" + rest[q+1:]
	}
	return dsn[:idx+1] + db
}
