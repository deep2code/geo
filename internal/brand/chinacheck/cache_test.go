package chinacheck

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

func cacheTestRootDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GEO_TEST_MYSQL_ROOT_DSN"))
	if dsn == "" {
		dsn = "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true&tls=false"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("跳过 chinacheck 缓存测试：MySQL 不可用 (%v)", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("跳过 chinacheck 缓存测试：MySQL ping 失败 (GEO_TEST_MYSQL_ROOT_DSN err=%v)", err)
	}
	return dsn
}

func cacheInjectDB(dsn, db string) string {
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

// openTestCache 为每个测试创建一个临时 MySQL 数据库并返回 CacheStore + 清理函数。
func openTestCache(t *testing.T, opts ...CacheOption) (CacheStore, func()) {
	t.Helper()
	rootDSN := cacheTestRootDSN(t)
	dbName := fmt.Sprintf("geo_cc_test_%d_%s", os.Getpid(), strings.ToLower(time.Now().Format("150405000000")))
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatalf("打开 root 连接: %v", err)
	}
	if _, err := root.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", dbName)); err != nil {
		root.Close()
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	dsn := cacheInjectDB(rootDSN, dbName)
	ca, err := NewCache(dsn, opts...)
	if err != nil {
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		root.Close()
		t.Fatalf("NewCache: %v", err)
	}
	cleanup := func() {
		// 尽力关闭（接口没 Close，就交给 GC），然后删掉测试库
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		root.Close()
	}
	return ca, cleanup
}

func TestCache_BasicRoundTrip(t *testing.T) {
	ca, cleanup := openTestCache(t, WithTTL(time.Hour), WithMaxItems(100))
	defer cleanup()
	dsn := ca.Path()
	if dsn == "" {
		t.Fatalf("Path() 应为 MySQL DSN，得到空")
	}
	st := ca.Stats()
	if st.Backend != "mysql" {
		t.Fatalf("backend 应为 mysql, got %q", st.Backend)
	}
	if st.Count != 0 {
		t.Fatalf("fresh cache should be empty, got %d", st.Count)
	}

	// SetSearch + GetSearch
	sr := &SearchResult{Total: 3, Companies: []CompanyHit{
		{CompanyID: "1", NameZh: "A有限公司", RegistrationNo: "CODE1"},
		{CompanyID: "2", NameZh: "B有限公司"},
	}}
	if err := ca.SetSearch("zh", "腾讯", 5, sr); err != nil {
		t.Fatalf("SetSearch: %v", err)
	}
	got, ok := ca.GetSearch("zh", "腾讯", 5)
	if !ok {
		t.Fatalf("GetSearch 应当命中")
	}
	if got.Total != 3 || len(got.Companies) != 2 || got.Companies[0].CompanyID != "1" {
		t.Fatalf("GetSearch 数据不对: %+v", got)
	}
	// 不同 limit / lang 不互通
	if _, ok := ca.GetSearch("zh", "腾讯", 10); ok {
		t.Fatalf("不同 limit 不应命中")
	}
	if _, ok := ca.GetSearch("en", "腾讯", 5); ok {
		t.Fatalf("不同 lang 不应命中")
	}

	// SetSnapshot + GetSnapshot（双 key）
	snap := &SnapshotResponse{
		CompanyID: "1",
		Snapshot: &Snapshot{
			CompanyName:         "A有限公司",
			CreditCode:          "CODE1",
			LegalRepresentative: "张三",
		},
	}
	if err := ca.SetSnapshot("1", "腾讯A", snap); err != nil {
		t.Fatalf("SetSnapshot: %v", err)
	}
	if got, ok := ca.GetSnapshot("1", ""); !ok || got.CompanyID != "1" || got.Snapshot.LegalRepresentative != "张三" {
		t.Fatalf("GetSnapshot by ID 失败: ok=%v got=%+v", ok, got)
	}
	if got, ok := ca.GetSnapshot("", "腾讯A"); !ok || got.Snapshot.CreditCode != "CODE1" {
		t.Fatalf("GetSnapshot by query 失败: ok=%v got=%+v", ok, got)
	}
	if _, ok := ca.GetSnapshot("999", ""); ok {
		t.Fatalf("未知 ID 不应命中")
	}

	// 重新打开同一个数据库（模拟重启），仍能读取
	ca2, err := NewCache(dsn, WithMaxItems(100))
	if err != nil {
		t.Fatalf("重载缓存失败: %v", err)
	}
	st2 := ca2.Stats()
	if st2.Count < 3 {
		t.Fatalf("重载后条目太少: %d", st2.Count)
	}
	if sr2, ok := ca2.GetSearch("zh", "腾讯", 5); !ok || sr2.Total != 3 {
		t.Fatalf("重载后 search 不命中")
	}
	if sp2, ok := ca2.GetSnapshot("1", ""); !ok || sp2.Snapshot.LegalRepresentative != "张三" {
		t.Fatalf("重载后 snapshot 不命中")
	}

	// Clear
	if err := ca2.Clear(); err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if ca2.Stats().Count != 0 {
		t.Fatalf("Clear 后应为 0")
	}
}

func TestCache_TTLExpire(t *testing.T) {
	ca, cleanup := openTestCache(t, WithTTL(500*time.Millisecond), WithMaxItems(100))
	defer cleanup()

	if err := ca.SetSearch("zh", "q1", 5, &SearchResult{Total: 1}); err != nil {
		t.Fatalf("SetSearch: %v", err)
	}
	if _, ok := ca.GetSearch("zh", "q1", 5); !ok {
		t.Fatalf("未过期时应命中")
	}
	// 等待 TTL 过期
	time.Sleep(900 * time.Millisecond)
	if _, ok := ca.GetSearch("zh", "q1", 5); ok {
		t.Fatalf("过期后不应命中")
	}
}

func TestCache_EvictionAndCompact(t *testing.T) {
	ca, cleanup := openTestCache(t, WithMaxItems(5), WithTTL(time.Hour))
	defer cleanup()
	maxN := 5

	// 写入 N+3 条：触发淘汰
	for i := 0; i < maxN+3; i++ {
		sr := &SearchResult{Total: i}
		if err := ca.SetSearch("zh", string(rune('A'+i)), 5, sr); err != nil {
			t.Fatalf("SetSearch %d: %v", i, err)
		}
	}
	st := ca.Stats()
	if st.Count != maxN {
		t.Fatalf("淘汰后条目应为 %d, got %d", maxN, st.Count)
	}
	checks := []struct {
		q   string
		hit bool
	}{
		{"A", false}, {"B", false}, {"C", false},
		{"D", true}, {"E", true}, {"F", true}, {"G", true}, {"H", true},
	}
	for _, c := range checks {
		_, ok := ca.GetSearch("zh", c.q, 5)
		if ok != c.hit {
			t.Errorf("query=%s hit=%v expected %v", c.q, ok, c.hit)
		}
	}

	// 触发 Compact（清理过期 + 表空间优化，MySQL 上 OPTIMIZE 会同步统计）
	for i := 0; i < maxN; i++ {
		sr := &SearchResult{Total: i}
		if err := ca.SetSearch("zh", "dupkey", i+1, sr); err != nil {
			t.Fatalf("dup SetSearch: %v", err)
		}
	}
	beforeCompact := ca.Stats()
	if beforeCompact.Count == 0 {
		t.Fatalf("多次写入后不应为空")
	}
	if err := ca.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	afterCompact := ca.Stats()
	if afterCompact.Count > maxN {
		t.Fatalf("压缩后条目 %d > max %d", afterCompact.Count, maxN)
	}
}
