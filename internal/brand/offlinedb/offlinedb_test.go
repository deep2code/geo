package offlinedb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func offlineTestRootDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GEO_TEST_MYSQL_ROOT_DSN"))
	if dsn == "" {
		dsn = "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true&tls=false"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("跳过 offlinedb 测试：无法打开 MySQL (%v)", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("跳过 offlinedb 测试：MySQL 不可用 (ping err=%v)", err)
	}
	return dsn
}

func injectDSNDB(dsn, db string) string {
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

// smokeSampleJSON 模拟 guichong/- json 分支格式（字段名和原始一致）。
const smokeSampleJSON = `
[
  {
    "企业名称": "深圳市腾讯计算机系统有限公司",
    "统一社会信用代码": "91440300708461136T",
    "注册日期": "1998-11-11",
    "企业类型": "有限责任公司",
    "法人代表": "马化腾",
    "注册资金": "200万元人民币",
    "经营范围": "计算机软、硬件的设计、技术开发、销售（不含专营、专控、专卖商品及限制项目）；数据库及计算机网络服务；国内商业、物资供销业（不含专营、专控、专卖商品）；第二类增值电信业务中的信息服务业务。",
    "省份": "广东省",
    "地市": "深圳市",
    "注册地址": "深圳市南山区粤海街道"
  },
  {
    "企业名称": "阿里巴巴（中国）有限公司",
    "统一社会信用代码": "91330100799655058B",
    "注册日期": "2007-03-26",
    "企业类型": "有限责任公司（台港澳法人独资）",
    "法人代表": "张勇",
    "注册资金": "15400万美元",
    "经营范围": "服务：企业管理咨询、计算机软件、硬件、网络技术的技术开发、技术咨询、技术服务、成果转让，计算机系统集成。",
    "省份": "浙江省",
    "地市": "杭州市",
    "注册地址": "浙江省杭州市余杭区文一西路"
  },
  {
    "企业名称": "北京字节跳动网络技术有限公司",
    "统一社会信用代码": "91110108351558522B",
    "注册日期": "2012-03-09",
    "企业类型": "其他有限责任公司",
    "法人代表": "张一鸣",
    "注册资金": "30000万元人民币",
    "经营范围": "技术开发、技术推广、技术转让、技术咨询、技术服务；计算机技术培训；电脑动画设计；软件开发；销售自行开发后的产品。",
    "省份": "北京市",
    "地市": "北京市",
    "注册地址": "北京市海淀区知春路"
  }
]
`

func openTestDB(t *testing.T) (DB, func()) {
	t.Helper()
	rootDSN := offlineTestRootDSN(t)
	dbName := fmt.Sprintf("geo_offline_test_%d_%s", os.Getpid(), strings.ToLower(time.Now().Format("150405000000")))
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatalf("打开 root 连接: %v", err)
	}
	if _, err := root.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", dbName)); err != nil {
		root.Close()
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	dsn := injectDSNDB(rootDSN, dbName)
	odb, err := Open(dsn)
	if err != nil {
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		root.Close()
		t.Fatalf("Open: %v", err)
	}
	cleanup := func() {
		odb.Close()
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		root.Close()
	}
	return odb, cleanup
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

// TestImportJSONFile_Search 小样本：JSON 数组导入 -> 统计 -> 搜索命中。
func TestImportJSONFile_Search(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openTestDB(t)
	defer cleanup()

	st0, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats empty: %v", err)
	}
	if st0.Count != 0 {
		t.Fatalf("expected 0 records, got %d", st0.Count)
	}

	f := writeTempFile(t, "sample.json", smokeSampleJSON)
	res, err := db.ImportJSONFile(ctx, f, 50)
	if err != nil {
		t.Fatalf("ImportJSONFile: %v", err)
	}
	if res.Inserted != 3 {
		t.Fatalf("expected inserted=3, got %+v", res)
	}

	st, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Count != 3 {
		t.Fatalf("expected count=3, got %d", st.Count)
	}
	if _, ok := st.Provinces["广东省"]; !ok {
		t.Fatalf("省份统计缺失广东省: %+v", st.Provinces)
	}

	// 1. 精确前缀命中（深圳腾讯）
	out, err := db.Search(ctx, SearchOptions{Query: "腾讯", TopN: 5})
	if err != nil {
		t.Fatalf("Search 腾讯: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Search 腾讯 未命中任何记录，FULLTEXT(ngram) 全文索引可能未生效")
	}
	hit := out[0]
	if !strings.Contains(hit.Name, "腾讯") {
		t.Fatalf("首条非腾讯: %+v", hit)
	}
	if hit.Code != "91440300708461136T" {
		t.Fatalf("信用代码未保留: %+v", hit)
	}
	if hit.LegalRepresentative != "马化腾" {
		t.Fatalf("法人字段丢失: %+v", hit)
	}
	if hit.Score <= 0 {
		t.Fatalf("评分未填: %+v", hit)
	}

	// 2. 法人搜索
	out2, err := db.Search(ctx, SearchOptions{Query: "张一鸣", TopN: 3})
	if err != nil {
		t.Fatalf("Search 张一鸣: %v", err)
	}
	if len(out2) == 0 || !strings.Contains(out2[0].Name, "字节跳动") {
		t.Fatalf("法人搜索命中失败: %+v", out2)
	}

	// 3. Province 过滤
	out3, err := db.Search(ctx, SearchOptions{Query: "有限公司", Province: "浙江省", TopN: 10})
	if err != nil {
		t.Fatalf("Search with province: %v", err)
	}
	for _, r := range out3 {
		if r.Province != "浙江省" {
			t.Fatalf("Province 过滤失效: %+v", r)
		}
	}
	if len(out3) == 0 {
		t.Fatal("浙江过滤未命中阿里")
	}

	// 4. 省份枚举
	ps, err := db.Provinces(ctx)
	if err != nil {
		t.Fatalf("Provinces: %v", err)
	}
	found := map[string]bool{}
	for _, p := range ps {
		found[p] = true
	}
	for _, want := range []string{"广东省", "浙江省", "北京市"} {
		if !found[want] {
			t.Fatalf("Provinces 缺少 %s: %+v", want, ps)
		}
	}
}

// TestImportJSONL 验证 JSONL（每行一个对象）格式识别与导入。
func TestImportJSONL(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openTestDB(t)
	defer cleanup()

	// 两行：首行为对象、次行为另一个对象，最外层无 [ ] 数组包裹
	jsonl := `{"企业名称":"测试公司A","统一社会信用代码":"91000000A","注册日期":"2010-01-01","省份":"上海市","地市":"上海市"}
{"企业名称":"测试公司B","统一社会信用代码":"91000000B","注册日期":"2011-02-02","省份":"上海市","地市":"上海市"}
`
	f := writeTempFile(t, "x.jsonl", jsonl)
	res, err := db.ImportJSONFile(ctx, f, 100)
	if err != nil {
		t.Fatalf("ImportJSONL: %v", err)
	}
	if res.Inserted != 2 {
		t.Fatalf("JSONL inserted 2 期望，实际 %+v", res)
	}
	st, _ := db.Stats(ctx)
	if st.Count != 2 {
		t.Fatalf("JSONL stats count=2 期望，实际 %d", st.Count)
	}
}

// TestCreditCodeUniqueness 验证 INSERT OR IGNORE 对重复信用代码的跳过。
func TestCreditCodeUniqueness(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openTestDB(t)
	defer cleanup()

	payload := `[
  {"企业名称":"同一家公司-1","统一社会信用代码":"91SAME111","省份":"广东省"},
  {"企业名称":"同一家公司-2","统一社会信用代码":"91SAME111","省份":"广东省"},
  {"企业名称":"另一家","统一社会信用代码":"91OTHER00","省份":"广东省"}
]`
	f := writeTempFile(t, "dup.json", payload)
	res, err := db.ImportJSONFile(ctx, f, 100)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Inserted != 2 {
		t.Fatalf("期望 inserted=2（重复信用代码跳过 1 条），实际 %+v", res)
	}
	if res.Skipped < 1 {
		t.Fatalf("期望 skipped>=1，实际 %+v", res)
	}
}

// TestImportJSONObject 验证 {"erDataList": [...]} 包裹格式导入（guichong/- 仓库实际格式）。
func TestImportJSONObject(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openTestDB(t)
	defer cleanup()

	payload := `{"erDataList": [
  {"name":"测试包裹公司A","code":"91WRAP0001","registrationDay":"2019-01-01","province":"广东省","city":"深圳市"},
  {"name":"测试包裹公司B","code":"91WRAP0002","registrationDay":"2019-06-15","province":"北京市","city":"海淀区"}
]}`
	f := writeTempFile(t, "wrap.json", payload)
	res, err := db.ImportJSONFile(ctx, f, 100)
	if err != nil {
		t.Fatalf("ImportJSONObject: %v", err)
	}
	if res.Inserted != 2 {
		t.Fatalf("期望 inserted=2，实际 %+v", res)
	}
	// 搜索验证
	out, err := db.Search(ctx, SearchOptions{Query: "包裹", TopN: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("期望搜索命中 2 条，实际 %d", len(out))
	}
}

// TestClear 验证 Clear 删除所有记录 + Provinces 归零。
func TestClear(t *testing.T) {
	ctx := context.Background()
	db, cleanup := openTestDB(t)
	defer cleanup()

	f := writeTempFile(t, "x.json", smokeSampleJSON)
	if _, err := db.ImportJSONFile(ctx, f, 100); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if err := db.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	st, err := db.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Count != 0 {
		t.Fatalf("Clear 后期望 count=0，实际 %d", st.Count)
	}
}
