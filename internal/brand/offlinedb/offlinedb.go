// Package offlinedb 中国大陆工商注册信息离线 MySQL 数据库。
//
// 数据源：https://github.com/guichong/-/tree/json (1978-2019，1000万+ 条，10 字段)
// 存储：MySQL + FULLTEXT(ngram 中文分词) 全文索引，
//
//	1000 万条数据下，按品牌/公司名模糊搜索 Top 20 命中 < 50ms。
package offlinedb

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"my-geo/internal/dbprovider"

	_ "github.com/go-sql-driver/mysql"
)

const defaultMySQLDSN = "geo:geoPass@tcp(127.0.0.1:3306)/geo?parseTime=true&charset=utf8mb4&loc=Local&collation=utf8mb4_unicode_ci"

// Company 数据库中的一条工商注册记录。
type Company struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Code                string  `json:"code,omitempty"`
	RegistrationDay     string  `json:"registration_day,omitempty"`
	Character           string  `json:"character,omitempty"`
	LegalRepresentative string  `json:"legal_representative,omitempty"`
	Capital             string  `json:"capital,omitempty"`
	BusinessScope       string  `json:"business_scope,omitempty"`
	Province            string  `json:"province,omitempty"`
	City                string  `json:"city,omitempty"`
	Address             string  `json:"address,omitempty"`
	ImportedAt          int64   `json:"imported_at,omitempty"`
	Score               float64 `json:"score,omitempty"`
}

// Stats 数据库统计。
type Stats struct {
	Path      string           `json:"path"`
	Backend   string           `json:"backend"`
	Count     int64            `json:"count"`
	FileSize  int64            `json:"file_size_bytes"`
	SchemaAt  string           `json:"schema_created_at"`
	Provinces map[string]int64 `json:"provinces,omitempty"`
}

// mysqlStore MySQL 实现的 OfflineStore。
type mysqlStore struct {
	path string
	db   *sql.DB
}

// Open 打开/创建 MySQL 离线工商数据库并完成 schema 初始化。
// 保持原签名兼容：path 若非空视为 MySQL DSN（兼容）；否则读 env GEO_MYSQL_DSN，缺省为内置默认。
func Open(path string) (OfflineStore, error) {
	dsn := path
	if dsn == "" {
		dsn = os.Getenv("GEO_MYSQL_DSN")
		if dsn == "" {
			dsn = defaultMySQLDSN
		}
	}
	dsn = dbprovider.NormalizeMySQLDSN(dsn)
	sqldb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 失败: %w", err)
	}
	dbprovider.ConfigurePool(sqldb, "offline")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("离线库 MySQL ping 失败: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, `SET NAMES utf8mb4, sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION', innodb_strict_mode=ON`); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("初始化 MySQL 会话变量失败: %w", err)
	}

	// 表结构由 deploy/initdb 初始化（02-schema.sql），应用内不再内嵌 migration。
	return &mysqlStore{path: dsn, db: sqldb}, nil
}

// Close 关闭数据库。
func (d *mysqlStore) Close() error { return d.db.Close() }

// Path 返回实际数据库 DSN。
func (d *mysqlStore) Path() string { return d.path }

// Backend 返回后端类型标识。
func (d *mysqlStore) Backend() string { return "mysql" }

// ---------- 统计 ----------

// Stats 统计数据库体量与省分布。
func (d *mysqlStore) Stats(ctx context.Context) (Stats, error) {
	st := Stats{Path: d.path, Backend: "mysql", FileSize: 0}
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM companies`).Scan(&st.Count); err != nil {
		return st, err
	}
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
	Query    string
	TopN     int
	Province string
	City     string
}

// Search 按查询词模糊搜索，返回 TopN 匹配结果（MySQL 布尔全文检索优先，不足补 LIKE 兜底）。
func (d *mysqlStore) Search(ctx context.Context, opt SearchOptions) ([]Company, error) {
	q := strings.TrimSpace(opt.Query)
	if q == "" {
		return nil, fmt.Errorf("query 不能为空")
	}
	topN := opt.TopN
	if topN <= 0 {
		topN = 20
	}
	ftQuery := buildFTQuery(q)

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
		whereClause = " AND " + strings.Join(where, " AND ")
	}

	matchExpr := `MATCH(c.name, c.business_scope, c.legal_rep, c.address) AGAINST (? IN BOOLEAN MODE)`

	ftArgs := append([]interface{}{ftQuery}, args...)
	ftArgs = append(ftArgs, topN)
	sqlFT := fmt.Sprintf(`
SELECT c.id, c.name, COALESCE(c.code,''), COALESCE(c.established_date,''),
       COALESCE(c.industry,''), COALESCE(c.legal_rep,''),
       COALESCE(c.registered_capital,''), COALESCE(c.business_scope,''),
       COALESCE(c.province,''), COALESCE(c.city,''), COALESCE(c.address,''),
       c.created_at,
       %s AS score
FROM companies c
WHERE %s%s
ORDER BY score DESC
LIMIT ?`, matchExpr, matchExpr, whereClause)

	rows, err := d.db.QueryContext(ctx, sqlFT, ftArgs...)
	if err != nil {
		return d.searchLikeFallback(ctx, opt, q, topN, 0)
	}
	out, scanErr := scanCompanies(rows, true)
	if scanErr != nil {
		return nil, scanErr
	}

	normalizeFTScore(out)

	remain := topN - len(out)
	if remain > 0 {
		seen := map[int64]struct{}{}
		for _, c := range out {
			seen[c.ID] = struct{}{}
		}
		extra, err := d.searchLikeFallback(ctx, opt, q, remain, len(out))
		if err == nil {
			for _, c := range extra {
				if _, ok := seen[c.ID]; !ok {
					out = append(out, c)
					seen[c.ID] = struct{}{}
				}
			}
		}
	}

	return out, nil
}

func scanCompanies(rows *sql.Rows, withScore bool) ([]Company, error) {
	defer rows.Close()
	var out []Company
	for rows.Next() {
		var c Company
		var err error
		if withScore {
			err = rows.Scan(&c.ID, &c.Name, &c.Code, &c.RegistrationDay,
				&c.Character, &c.LegalRepresentative,
				&c.Capital, &c.BusinessScope,
				&c.Province, &c.City, &c.Address,
				&c.ImportedAt, &c.Score)
		} else {
			err = rows.Scan(&c.ID, &c.Name, &c.Code, &c.RegistrationDay,
				&c.Character, &c.LegalRepresentative,
				&c.Capital, &c.BusinessScope,
				&c.Province, &c.City, &c.Address,
				&c.ImportedAt)
		}
		if err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *mysqlStore) searchLikeFallback(ctx context.Context, opt SearchOptions, q string, topN int, offsetRank int) ([]Company, error) {
	cond := []string{"(c.name LIKE CONCAT('%', ?, '%') OR c.business_scope LIKE CONCAT('%', ?, '%') OR c.legal_rep LIKE CONCAT('%', ?, '%') OR c.address LIKE CONCAT('%', ?, '%'))"}
	args := []interface{}{q, q, q, q}
	if opt.Province != "" {
		cond = append(cond, "c.province = ?")
		args = append(args, opt.Province)
	}
	if opt.City != "" {
		cond = append(cond, "c.city = ?")
		args = append(args, opt.City)
	}
	namePrefix := q + "%"
	sqlQ := fmt.Sprintf(`
SELECT c.id, c.name, COALESCE(c.code,''), COALESCE(c.established_date,''),
       COALESCE(c.industry,''), COALESCE(c.legal_rep,''),
       COALESCE(c.registered_capital,''), COALESCE(c.business_scope,''),
       COALESCE(c.province,''), COALESCE(c.city,''), COALESCE(c.address,''),
       c.created_at
FROM companies c
WHERE %s
ORDER BY CASE WHEN c.name LIKE ? THEN 0 ELSE 1 END, LENGTH(c.name), c.id
LIMIT ?`, strings.Join(cond, " AND "))
	args = append(args, namePrefix, topN)
	rows, err := d.db.QueryContext(ctx, sqlQ, args...)
	if err != nil {
		return nil, err
	}
	out, scanErr := scanCompanies(rows, false)
	if scanErr != nil {
		return nil, scanErr
	}
	for i := range out {
		base := 50.0 - float64(offsetRank+i)*0.5
		sim := float64(len(q)) / float64(max(1, len(out[i].Name))) * 50
		out[i].Score = base + sim
		if out[i].Score > 100 {
			out[i].Score = 100
		}
		if out[i].Score < 0 {
			out[i].Score = 0
		}
	}
	return out, nil
}

func buildFTQuery(q string) string {
	tokens := strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '，' || r == '、' ||
			r == ';' || r == '；' || r == '|' || r == '/' || r == '\\'
	})
	var parts []string
	for _, t := range tokens {
		t = strings.Trim(t, "*:()\"'+-@<>~")
		if t == "" {
			continue
		}
		runes := []rune(t)
		if len(runes) == 1 && unicode.Is(unicode.Han, runes[0]) {
			continue
		}
		parts = append(parts, "+"+t)
	}
	if len(parts) == 0 {
		for _, r := range []rune(q) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
				s := string(r)
				if !unicode.Is(unicode.Han, r) || len([]rune(s)) > 1 || len(parts) == 0 {
					parts = append(parts, "+"+s)
				}
			}
		}
		if len(parts) == 0 {
			return "+" + strings.ReplaceAll(q, "'", "")
		}
	}
	return strings.Join(parts, " ")
}

func normalizeFTScore(in []Company) {
	if len(in) == 0 {
		return
	}
	var maxS float64
	for _, c := range in {
		if c.Score > maxS {
			maxS = c.Score
		}
	}
	if maxS < 1e-9 {
		for i := range in {
			in[i].Score = 100
		}
		return
	}
	for i := range in {
		s := 100 * (in[i].Score / maxS)
		if s < 0 {
			s = 0
		}
		if s > 100 {
			s = 100
		}
		in[i].Score = s
	}
}

// ---------- 导入 ----------

// ImportResult 导入统计。
type ImportResult struct {
	Inserted int64
	Skipped  int64
	Failed   int64
	Duration time.Duration
	Files    int
}

// ImportJSONFile 导入单个 JSON 文件。
//
// 支持两种格式（自动识别）：
//  1. 标准 JSON 数组：[{...}, {...}, ...]
//  2. JSONL：每行一个 {...}（处理大文件更省内存）
func (d *mysqlStore) ImportJSONFile(ctx context.Context, path string, batchSize int) (ImportResult, error) {
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

// ImportDir 递归导入目录下所有 .json 文件。
func (d *mysqlStore) ImportDir(ctx context.Context, dir string, batchSize int) (ImportResult, error) {
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

func mapRec(m map[string]any) (rec rawRecord) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok && v != nil {
				if s, ok := v.(string); ok {
					return strings.TrimSpace(s)
				}
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
	buf := make([]byte, 512)
	n, e := io.ReadFull(r, buf)
	head = buf[:n]
	if e == io.EOF || (e == io.ErrUnexpectedEOF && n == 0) {
		return "", head, fmt.Errorf("文件为空")
	}
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
func (d *mysqlStore) importJSONL(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
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

// importJSONArray 导入 JSON 数组（流式，避免一次性加载内存）。
func (d *mysqlStore) importJSONArray(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
	importedAt := time.Now().Unix()
	dec := json.NewDecoder(r)
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("解析 [ 失败: %w", err)
	}
	if dl, ok := t.(json.Delim); !ok || dl.String() != "[" {
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

// importJSONObject 导入 {"erDataList": [...]} 等对象包裹数组格式（流式）。
func (d *mysqlStore) importJSONObject(ctx context.Context, r io.Reader, batchSize int, res *ImportResult) error {
	importedAt := time.Now().Unix()
	dec := json.NewDecoder(r)

	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("解析 { 失败: %w", err)
	}
	if dl, ok := t.(json.Delim); !ok || dl != '{' {
		return fmt.Errorf("期望 { 但得到 %v", t)
	}

	for dec.More() {
		_, err := dec.Token()
		if err != nil {
			return fmt.Errorf("读取 key 失败: %w", err)
		}
		valToken, err := dec.Token()
		if err != nil {
			return fmt.Errorf("读取 value 失败: %w", err)
		}

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
	return nil
}

// insertBatch 单事务中批量 INSERT，INSERT IGNORE 跳过 code 重复。
func (d *mysqlStore) insertBatch(ctx context.Context, batch []rawRecord, importedAt int64) (inserted, skipped, failed int, err error) {
	if len(batch) == 0 {
		return 0, 0, 0, nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT IGNORE INTO companies
 (name, code, established_date, industry, legal_rep, registered_capital, business_scope, province, city, district, address, status, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
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
			nil,
			emptyToNull(r.Address),
			nil,
			importedAt,
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

// Clear 清空 companies 表。
func (d *mysqlStore) Clear(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM companies`)
	return err
}

// ---------- 工具 ----------

// Provinces 返回数据库内所有省份。
func (d *mysqlStore) Provinces(ctx context.Context) ([]string, error) {
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
	slices.Sort(out)
	return out, rows.Err()
}

var _ OfflineStore = (*mysqlStore)(nil)
