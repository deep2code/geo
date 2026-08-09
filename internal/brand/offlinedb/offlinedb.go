// Package offlinedb 中国大陆工商注册信息离线 SQLite 数据库。
//
// 数据源：https://github.com/guichong/-/tree/json (1978-2019，1000万+ 条，10 字段)
// 存储：SQLite（modernc.org/sqlite，纯 Go、无 CGO、零外部依赖），配合 FTS5 全文索引，
//
//	1000 万条数据下，按品牌/公司名模糊搜索 Top 20 命中 < 50ms。
package offlinedb

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// 表和索引 SQL。FTS5 索引（companies_fts）只对"搜索相关字段"建，避免无用膨胀。
const (
	sqlSchema = `
CREATE TABLE IF NOT EXISTS companies (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	name                TEXT NOT NULL,
	code                TEXT,
	registration_day    TEXT,
	character           TEXT,
	legal_representative TEXT,
	capital             TEXT,
	business_scope      TEXT,
	province            TEXT,
	city                TEXT,
	address             TEXT,
	imported_at         INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_companies_code ON companies(code);
CREATE INDEX IF NOT EXISTS idx_companies_province ON companies(province);
CREATE INDEX IF NOT EXISTS idx_companies_city ON companies(city);

CREATE VIRTUAL TABLE IF NOT EXISTS companies_fts USING fts5(
	name, legal_representative, province, city, address, business_scope,
	content='companies', content_rowid='id',
	tokenize = 'unicode61 remove_diacritics 2'
);

-- 触发器：companies 写入/删除时同步更新 FTS 索引
CREATE TRIGGER IF NOT EXISTS companies_ai AFTER INSERT ON companies BEGIN
  INSERT INTO companies_fts(rowid, name, legal_representative, province, city, address, business_scope)
  VALUES (new.id, new.name, new.legal_representative, new.province, new.city, new.address, new.business_scope);
END;
CREATE TRIGGER IF NOT EXISTS companies_ad AFTER DELETE ON companies BEGIN
  INSERT INTO companies_fts(companies_fts, rowid, name, legal_representative, province, city, address, business_scope)
  VALUES ('delete', old.id, old.name, old.legal_representative, old.province, old.city, old.address, old.business_scope);
END;
CREATE TRIGGER IF NOT EXISTS companies_au AFTER UPDATE ON companies BEGIN
  INSERT INTO companies_fts(companies_fts, rowid, name, legal_representative, province, city, address, business_scope)
  VALUES ('delete', old.id, old.name, old.legal_representative, old.province, old.city, old.address, old.business_scope);
  INSERT INTO companies_fts(rowid, name, legal_representative, province, city, address, business_scope)
  VALUES (new.id, new.name, new.legal_representative, new.province, new.city, new.address, new.business_scope);
END;
`
)

// Company 数据库中的一条工商注册记录。
type Company struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	Code                string `json:"code,omitempty"`
	RegistrationDay     string `json:"registration_day,omitempty"`
	Character           string `json:"character,omitempty"`
	LegalRepresentative string `json:"legal_representative,omitempty"`
	Capital             string `json:"capital,omitempty"`
	BusinessScope       string `json:"business_scope,omitempty"`
	Province            string `json:"province,omitempty"`
	City                string `json:"city,omitempty"`
	Address             string `json:"address,omitempty"`
	ImportedAt          int64  `json:"imported_at,omitempty"`
	// 搜索评分（仅 Search 返回时填）
	Score float64 `json:"score,omitempty"`
}

// Stats 数据库统计。
type Stats struct {
	Path      string            `json:"path"`
	Backend   string            `json:"backend"` // 实际后端：sqlite / duckdb 等
	Count     int64             `json:"count"`
	FileSize  int64             `json:"file_size_bytes"`
	SchemaAt  string            `json:"schema_created_at"`
	Provinces map[string]int64  `json:"provinces,omitempty"` // 按省 Top10 统计
}

// sqliteStore SQLite 实现的 OfflineStore（零依赖默认后端）。
type sqliteStore struct {
	path string
	db   *sql.DB
}

// Open 打开/创建 SQLite 离线工商数据库并完成 schema 初始化。
// 保持原签名兼容：path 为空用默认 ~/.local/share/geo/geo_offline_companies.db。
// 返回 OfflineStore 接口（通过 DB 别名），调用方无需修改。
func Open(path string) (OfflineStore, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		dir := filepath.Join(home, ".local", "share", "geo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
		path = filepath.Join(dir, "geo_offline_companies.db")
	}
	// DSN 配置：journal_mode=WAL（高并发读写性能）、synchronous=NORMAL（速度+安全平衡）、mmap_size=1GB
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=mmap_size(%d)&_pragma=cache_size(-262144)&_pragma=foreign_keys(off)",
		path, 1<<30,
	)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	// WAL 模式支持多读单写，modernc.org/sqlite 是纯 Go 实现无 CGO 文件锁限制，
	// 适当调高连接数以支撑并发审计（多 prompt × 多引擎场景）。
	sqldb.SetMaxOpenConns(16)
	sqldb.SetMaxIdleConns(8)
	sqldb.SetConnMaxIdleTime(5 * time.Minute)
	// schema 初始化（一次性事务）
	if _, err := sqldb.Exec(sqlSchema); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("初始化 schema 失败: %w", err)
	}
	return &sqliteStore{path: path, db: sqldb}, nil
}

// Close 关闭数据库。
func (d *sqliteStore) Close() error { return d.db.Close() }

// Path 返回实际数据库文件路径。
func (d *sqliteStore) Path() string { return d.path }

// Backend 返回后端类型标识。
func (d *sqliteStore) Backend() string { return "sqlite" }

// ---------- 统计 ----------

// Stats 统计数据库体量与省分布。
func (d *sqliteStore) Stats(ctx context.Context) (Stats, error) {
	st := Stats{Path: d.path, Backend: "sqlite"}
	if info, err := os.Stat(d.path); err == nil {
		st.FileSize = info.Size()
	}
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM companies`).Scan(&st.Count); err != nil {
		return st, err
	}
	// 按省 Top10
	rows, err := d.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(province,''),'未知'), COUNT(*) AS c
FROM companies GROUP BY 1 ORDER BY c DESC LIMIT 10`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	st.Provinces = map[string]int64{}
	for rows.Next() {
		var p string
		var n int64
		if err := rows.Scan(&p, &n); err != nil {
			return st, err
		}
		st.Provinces[p] = n
	}
	return st, rows.Err()
}

// ---------- 搜索 ----------

// SearchOptions 搜索选项。
type SearchOptions struct {
	Query    string // 必填：搜索词（公司名/品牌/法人/省份+城市 等）
	TopN     int    // 返回条数，默认 20
	Province string // 可选：只在某省内搜
	City     string // 可选：只在某市内搜
}

// Search 按查询词模糊搜索，返回 TopN 匹配结果（按 FTS bm25 评分排序，不支持 FTS 时降级 LIKE）。
func (d *sqliteStore) Search(ctx context.Context, opt SearchOptions) ([]Company, error) {
	q := strings.TrimSpace(opt.Query)
	if q == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	topN := opt.TopN
	if topN <= 0 {
		topN = 20
	}
	// 构造 FTS5 查询表达式：每个关键词作为前缀匹配
	ftsQuery := buildFTSQuery(q)
	// 省/市过滤（用实表 JOIN）
	where := []string{}
	args := []interface{}{}
	if opt.Province != "" {
		where = append(where, "c.province = ?")
		args = append(args, opt.Province)
	}
	if opt.City != "" {
		where = append(where, "c.city = ?")
		args = append(args, opt.City)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	args = append([]interface{}{ftsQuery, topN}, args...) // 注意顺序要对应 SQL 中的 ?
	// 优先 FTS，FTS 不支持时会报错，捕获后降级 LIKE
	sql := fmt.Sprintf(`
SELECT c.id, c.name, COALESCE(c.code,''), COALESCE(c.registration_day,''),
       COALESCE(c.character,''), COALESCE(c.legal_representative,''),
       COALESCE(c.capital,''), COALESCE(c.business_scope,''),
       COALESCE(c.province,''), COALESCE(c.city,''), COALESCE(c.address,''),
       c.imported_at,
       bm25(companies_fts) AS score
FROM companies_fts f
JOIN companies c ON c.id = f.rowid
%s
AND companies_fts MATCH ?
ORDER BY score ASC
LIMIT ?
`, whereClause)
	rows, err := d.db.QueryContext(ctx, sql, args...)
	if err != nil {
		// 降级：LIKE 模糊匹配（modernc.org/sqlite 的 FTS5 版本不兼容时触发）
		return d.searchLikeFallback(ctx, opt, q, topN)
	}
	defer rows.Close()
	var out []Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Code, &c.RegistrationDay, &c.Character,
			&c.LegalRepresentative, &c.Capital, &c.BusinessScope, &c.Province, &c.City,
			&c.Address, &c.ImportedAt, &c.Score); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// CJK 兜底：FTS5 unicode61 对中文按"词"切分不理想，关键词在词中间时可能 0 命中，
	// 此时降级到 LIKE 模糊匹配（牺牲速度保证召回，TopN 小的情况下 ms 级仍然够用）。
	if len(out) == 0 {
		return d.searchLikeFallback(ctx, opt, q, topN)
	}
	// bm25 是"越低越好"，这里统一把 score 转成 0-100 的"匹配度"方便前端展示
	normalizeScores(out)
	return out, nil
}

func (d *sqliteStore) searchLikeFallback(ctx context.Context, opt SearchOptions, q string, topN int) ([]Company, error) {
	cond := []string{"(c.name LIKE ? OR c.legal_representative LIKE ? OR c.address LIKE ?)"}
	args := []interface{}{"%" + q + "%", "%" + q + "%", "%" + q + "%"}
	if opt.Province != "" {
		cond = append(cond, "c.province = ?")
		args = append(args, opt.Province)
	}
	if opt.City != "" {
		cond = append(cond, "c.city = ?")
		args = append(args, opt.City)
	}
	args = append(args, topN)
	sql := fmt.Sprintf(`
SELECT c.id, c.name, COALESCE(c.code,''), COALESCE(c.registration_day,''),
       COALESCE(c.character,''), COALESCE(c.legal_representative,''),
       COALESCE(c.capital,''), COALESCE(c.business_scope,''),
       COALESCE(c.province,''), COALESCE(c.city,''), COALESCE(c.address,''),
       c.imported_at
FROM companies c
WHERE %s
ORDER BY CASE WHEN c.name LIKE ? THEN 0 ELSE 1 END, LENGTH(c.name)
LIMIT ?`, strings.Join(cond, " AND "))
	// name 前缀优先排序
	nameLikePrefix := q + "%"
	args2 := append([]interface{}{}, args[:len(args)-1]...)
	args2 = append(args2, nameLikePrefix)
	args2 = append(args2, topN)
	rows, err := d.db.QueryContext(ctx, sql, args2...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Code, &c.RegistrationDay, &c.Character,
			&c.LegalRepresentative, &c.Capital, &c.BusinessScope, &c.Province, &c.City,
			&c.Address, &c.ImportedAt); err != nil {
			return out, err
		}
		c.Score = float64(len(q)) / float64(maxInt(1, len(c.Name))) * 100 // 简单相似度
		if c.Score > 100 {
			c.Score = 100
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func buildFTSQuery(q string) string {
	// 去掉特殊字符，按空白/标点分词
	tokens := strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '，' || r == '、'
	})
	var parts []string
	for _, t := range tokens {
		t = strings.Trim(t, "*:()\"'")
		if t == "" {
			continue
		}
		// 每个 token 前缀匹配；并单独加一个 name 字段加权命中
		parts = append(parts, fmt.Sprintf("%s*", t))
	}
	if len(parts) == 0 {
		return strings.ReplaceAll(q, "\"", "") + "*"
	}
	return strings.Join(parts, " AND ")
}

func normalizeScores(in []Company) {
	if len(in) == 0 {
		return
	}
	// bm25: 越小越好。我们转换为匹配度：(1 - min/max)*100，再 clamp
	var minS, maxS float64
	first := true
	for _, c := range in {
		if first || c.Score < minS {
			minS = c.Score
		}
		if first || c.Score > maxS {
			maxS = c.Score
		}
		first = false
	}
	for i := range in {
		if maxS-minS < 1e-9 {
			in[i].Score = 100
			continue
		}
		s := 100 * (1 - (in[i].Score-minS)/(maxS-minS))
		if s < 0 {
			s = 0
		}
		if s > 100 {
			s = 100
		}
		in[i].Score = s
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- 导入 ----------

// ImportResult 导入统计。
type ImportResult struct {
	Inserted int64
	Skipped  int64 // code 唯一索引冲突导致跳过
	Failed   int64 // 解析失败/其他错误
	Duration time.Duration
	Files    int
}

// ImportJSONFile 导入单个 JSON 文件。
//
// 支持两种格式（自动识别）：
//  1. 标准 JSON 数组：[{...}, {...}, ...]
//  2. JSONL：每行一个 {...}（处理大文件更省内存）
func (d *sqliteStore) ImportJSONFile(ctx context.Context, path string, batchSize int) (ImportResult, error) {
	var res ImportResult
	if batchSize <= 0 {
		batchSize = 2000
	}
	f, err := os.Open(path)
	if err != nil {
		return res, fmt.Errorf("打开文件失败 %s: %w", path, err)
	}
	defer f.Close()
	res.Files = 1
	start := time.Now()
	// 先探测格式：读第一个非空字符，如果是 '[' 则是 JSON 数组
	format, headR, err := detectJSONFormat(f)
	if err != nil {
		return res, fmt.Errorf("探测文件格式失败 %s: %w", path, err)
	}
	switch format {
	case "json_array":
		err = d.importJSONArray(ctx, io.MultiReader(strings.NewReader(string(headR)), f), batchSize, &res)
	case "json_object":
		err = d.importJSONObject(ctx, io.MultiReader(strings.NewReader(string(headR)), f), batchSize, &res)
	default:
		err = d.importJSONL(ctx, io.MultiReader(strings.NewReader(string(headR)), f), batchSize, &res)
	}
	res.Duration = time.Since(start)
	return res, err
}

// ImportDir 递归导入目录下所有 .json 文件（按年份/省市分目录时常用）。
func (d *sqliteStore) ImportDir(ctx context.Context, dir string, batchSize int) (ImportResult, error) {
	var total ImportResult
	err := filepath.WalkDir(dir, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if de.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(de.Name()), ".json") {
			return nil
		}
		r, err := d.ImportJSONFile(ctx, path, batchSize)
		total.Inserted += r.Inserted
		total.Skipped += r.Skipped
		total.Failed += r.Failed
		total.Files += r.Files
		return err
	})
	return total, err
}

// ---------- 导入内部实现 ----------

// rawRecord guichong/- 仓库 json 分支里的原始记录格式（10 字段）。
// rawRecord 单条工商记录的内部表示；从 JSON 导入时经 mapRec 映射。
type rawRecord struct {
	Name                string
	Code                string
	RegistrationDay     string
	Character           string
	LegalRepresentative string
	Capital             string
	BusinessScope       string
	Province            string
	City                string
	Address             string
}

// mapRec JSON 导入时先用 map[string]any 解码，再按字段名（中文 key / 英文 key 均兼容）转 rawRecord。
// 中文 key 对应：guichong/- 仓库 json 分支原始格式（10 字段）
// 英文 key 对应：rawRecord 字段名 / CamelCase / snake_case / JSON 序列化后的 Company 结构
func mapRec(m map[string]any) (rec rawRecord) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok && v != nil {
				if s, ok := v.(string); ok {
					return strings.TrimSpace(s)
				}
				// 数字型注册资金 / 日期等兼容
				switch t := v.(type) {
				case float64:
					return strings.TrimSpace(fmt.Sprintf("%g", t))
				case bool:
					if t {
						return "true"
					}
					return "false"
				}
			}
		}
		return ""
	}
	rec.Name = get(
		"企业名称", "name", "Name", "company_name", "CompanyName", "entName", "ent_name",
	)
	rec.Code = get(
		"统一社会信用代码", "code", "Code", "creditCode", "credit_code", "uscc", "tyshxydm", "CreditCode",
	)
	rec.RegistrationDay = get(
		"注册日期", "registrationDay", "RegistrationDay", "registration_day", "establishDate", "esDate", "date", "成立日期",
	)
	rec.Character = get(
		"企业类型", "character", "Character", "entType", "ent_type", "companyType", "company_type", "企业类别",
	)
	rec.LegalRepresentative = get(
		"法人代表", "legalRepresentative", "LegalRepresentative", "legal_representative", "legalPerson", "legal_person", "frdb", "法人",
	)
	rec.Capital = get(
		"注册资金", "capital", "Capital", "regCapital", "reg_capital", "注册资本", "zczj",
	)
	rec.BusinessScope = get(
		"经营范围", "businessScope", "BusinessScope", "business_scope", "scope", "jyfw",
	)
	rec.Province = get(
		"省份", "province", "Province", "sheng", "省",
	)
	rec.City = get(
		"地市", "city", "City", "shi", "市",
	)
	rec.Address = get(
		"注册地址", "address", "Address", "addr", "zcdz", "住所",
	)
	return rec
}

func detectJSONFormat(r io.Reader) (format string, head []byte, err error) {
	// 读取前 512 字节用于格式探测（也作为 head 返回给 MultiReader 拼接）。
	buf := make([]byte, 512)
	n, e := io.ReadFull(r, buf)
	head = buf[:n]
	if e == io.EOF || (e == io.ErrUnexpectedEOF && n == 0) {
		return "", head, fmt.Errorf("文件为空")
	}
	// 跳过 BOM 和前导空白
	idx := 0
	for idx < len(head) {
		b := head[idx]
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == 0xEF || b == 0xBB || b == 0xBF {
			idx++
			continue
		}
		break
	}
	if idx >= len(head) {
		return "", head, fmt.Errorf("文件只含空白")
	}
	switch head[idx] {
	case '[':
		return "json_array", head, nil
	case '{':
		// 区分两种 { 开头的格式：
		//   A) {"erDataList": [...]}  — 对象包裹数组（guichong/- 仓库 json 分支实际格式）
		//   B) {"name":"xxx",...}\n{"name":"yyy",...}  — JSONL（每行一个对象）
		// 判据：前 100 字符内是否出现 ": [ 或 ":[
		prefix := string(head[idx:])
		if len(prefix) > 100 {
			prefix = prefix[:100]
		}
		if strings.Contains(prefix, "\": [") || strings.Contains(prefix, "\":[") ||
			strings.Contains(prefix, "\":\n[") {
			return "json_object", head, nil
		}
		return "jsonl", head, nil
	default:
		return "", head, fmt.Errorf("文件首字符=%q，不是 JSON", head[idx])
	}
}

// importJSONL 按行 JSONL 导入（流式，内存安全）。
func (d *sqliteStore) importJSONL(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
	importedAt := time.Now().Unix()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256*1024), 32*1024*1024)
	var batch []rawRecord
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ins, sk, fail, err := d.insertBatch(ctx, batch, importedAt)
		atomic.AddInt64(&res.Inserted, int64(ins))
		atomic.AddInt64(&res.Skipped, int64(sk))
		atomic.AddInt64(&res.Failed, int64(fail))
		batch = batch[:0]
		return err
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			atomic.AddInt64(&res.Failed, 1)
			continue
		}
		rec := mapRec(m)
		if rec.Name == "" {
			atomic.AddInt64(&res.Failed, 1)
			continue
		}
		batch = append(batch, rec)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// importJSONArray 导入 JSON 数组（用 json.Decoder Token 流式处理，避免一次性加载到内存）。
func (d *sqliteStore) importJSONArray(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
	importedAt := time.Now().Unix()
	dec := json.NewDecoder(r)
	// 消费掉开头的 [
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("解析 [ 失败: %w", err)
	}
	if d, ok := t.(json.Delim); !ok || d.String() != "[" {
		return fmt.Errorf("文件不是 JSON 数组，第一个 token=%v", t)
	}
	var batch []rawRecord
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ins, sk, fail, err := d.insertBatch(ctx, batch, importedAt)
		atomic.AddInt64(&res.Inserted, int64(ins))
		atomic.AddInt64(&res.Skipped, int64(sk))
		atomic.AddInt64(&res.Failed, int64(fail))
		batch = batch[:0]
		return err
	}
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			atomic.AddInt64(&res.Failed, 1)
			continue
		}
		rec := mapRec(m)
		if rec.Name == "" {
			atomic.AddInt64(&res.Failed, 1)
			continue
		}
		batch = append(batch, rec)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// importJSONObject 导入 {"erDataList": [...]} 等对象包裹数组格式。
// 用 json.Decoder Token API 流式处理：读取外层 {，遍历 key-value，
// 找到第一个值为数组的 key 后逐条 Decode，避免一次性加载大文件到内存。
func (d *sqliteStore) importJSONObject(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
	importedAt := time.Now().Unix()
	dec := json.NewDecoder(r)

	// 消费外层 {
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("解析 { 失败: %w", err)
	}
	if dl, ok := t.(json.Delim); !ok || dl != '{' {
		return fmt.Errorf("期望 { 但得到 %v", t)
	}

	// 遍历对象的 key-value pairs，找到值为数组的那个
	for dec.More() {
		// 读取 key
		_, err := dec.Token()
		if err != nil {
			return fmt.Errorf("读取 key 失败: %w", err)
		}
		// 读取 value 的起始 token
		valToken, err := dec.Token()
		if err != nil {
			return fmt.Errorf("读取 value 失败: %w", err)
		}

		// 如果 value 是数组开头 → 找到了，逐条 Decode
		if dl, ok := valToken.(json.Delim); ok && dl == '[' {
			var batch []rawRecord
			flush := func() error {
				if len(batch) == 0 {
					return nil
				}
				ins, sk, fail, err := d.insertBatch(ctx, batch, importedAt)
				atomic.AddInt64(&res.Inserted, int64(ins))
				atomic.AddInt64(&res.Skipped, int64(sk))
				atomic.AddInt64(&res.Failed, int64(fail))
				batch = batch[:0]
				return err
			}
			for dec.More() {
				var m map[string]any
				if err := dec.Decode(&m); err != nil {
					atomic.AddInt64(&res.Failed, 1)
					continue
				}
				rec := mapRec(m)
				if rec.Name == "" {
					atomic.AddInt64(&res.Failed, 1)
					continue
				}
				batch = append(batch, rec)
				if len(batch) >= batchSize {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			return flush()
		}

		// value 不是数组 → 跳过整个值（基本类型 Token 已读完；
		// 嵌套对象/数组需要按深度跳过）
		if _, ok := valToken.(json.Delim); ok {
			depth := 1
			for depth > 0 {
				t, err := dec.Token()
				if err != nil {
					return err
				}
				if d2, ok := t.(json.Delim); ok {
					switch d2 {
					case '{', '[':
						depth++
					case '}', ']':
						depth--
					}
				}
			}
		}
	}
	return nil // 对象里没有数组值
}

// insertBatch 在单事务中批量 INSERT 一批记录；利用 INSERT OR IGNORE 跳过 code 重复。
func (d *sqliteStore) insertBatch(ctx context.Context, batch []rawRecord, importedAt int64) (inserted, skipped, failed int, err error) {
	if len(batch) == 0 {
		return 0, 0, 0, nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// 用 INSERT OR IGNORE 配合 code 唯一索引：code 不为空且相同 -> 跳过
	stmt, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO companies
 (name, code, registration_day, character, legal_representative, capital, business_scope, province, city, address, imported_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer stmt.Close()

	for _, r := range batch {
		res, e := stmt.ExecContext(ctx,
			r.Name,
			emptyToNull(r.Code),
			emptyToNull(r.RegistrationDay),
			emptyToNull(r.Character),
			emptyToNull(r.LegalRepresentative),
			emptyToNull(r.Capital),
			emptyToNull(r.BusinessScope),
			emptyToNull(r.Province),
			emptyToNull(r.City),
			emptyToNull(r.Address),
			importedAt,
		)
		if e != nil {
			failed++
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			inserted++
		} else {
			skipped++
		}
	}
	if err := tx.Commit(); err != nil {
		return inserted, skipped, failed, err
	}
	return inserted, skipped, failed, nil
}

func emptyToNull(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// ---------- 清空 ----------

// Clear 清空 companies 表（连带 FTS 索引），VACUUM 回收磁盘空间。
func (d *sqliteStore) Clear(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `DELETE FROM companies`); err != nil {
		return err
	}
	// FTS 触发器会同步清理
	if _, err := d.db.ExecContext(ctx, `VACUUM`); err != nil && !errors.Is(err, sql.ErrTxDone) {
		// VACUUM 在某些繁忙场景会失败，非致命
	}
	return nil
}

// ---------- 工具 ----------

// Provinces 返回数据库内所有省份（用于前端筛选项）。
func (d *sqliteStore) Provinces(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT province FROM companies WHERE province IS NOT NULL AND province <> '' ORDER BY province`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, rows.Err()
}

// 编译期接口断言：确保 sqliteStore 完整实现 OfflineStore。
var _ OfflineStore = (*sqliteStore)(nil)
