package chinacheck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"my-geo/internal/dbprovider"

	_ "github.com/go-sql-driver/mysql"
)

type mysqlCacheStore struct {
	mu       sync.RWMutex
	dsn      string
	db       *sql.DB
	maxItems int
	ttl      time.Duration
}

const defaultMySQLDSN = "" // 不再提供硬编码默认值；必须通过 GEO_MYSQL_DSN 或显式参数配置

func resolveDSN(filePath string) (string, error) {
	if filePath != "" {
		return filePath, nil
	}
	if dsn := dbprovider.DSNFor(dbprovider.ModuleChinaCheckCache); dsn != "" {
		return dsn, nil
	}
	return "", errors.New("chinacheck: 未配置 GEO_MYSQL_DSN 且未提供显式 DSN 参数；请设置环境变量 GEO_MYSQL_DSN 或传入连接串")
}

func newMySQLCache(dsn string, opts ...CacheOption) (*mysqlCacheStore, error) {
	resolved, err := resolveDSN(dsn)
	if err != nil {
		return nil, err
	}
	dsn = dbprovider.NormalizeMySQLDSN(resolved)
	c := &mysqlCacheStore{
		dsn:      dsn,
		maxItems: defaultMaxItems,
		ttl:      defaultTTL,
	}
	for _, o := range opts {
		o(c)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("chinacheck/mysql: open failed: %w", err)
	}
	dbprovider.ConfigurePool(db, "cache")
	c.db = db

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("chinacheck/mysql: ping failed: %w", err)
	}
	if _, err := db.ExecContext(ctx, "SET NAMES utf8mb4, sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("chinacheck/mysql: set session failed: %w", err)
	}
	// 表结构由 deploy/initdb 初始化（02-schema.sql），应用内不再内嵌 migration。
	return c, nil
}

func (c *mysqlCacheStore) Path() string    { return c.dsn }
func (c *mysqlCacheStore) Backend() string { return "mysql" }

func extractServerAddrFromDSN(dsn string) string {
	start := strings.Index(dsn, "tcp(")
	if start < 0 {
		return ""
	}
	start += len("tcp(")
	end := strings.Index(dsn[start:], ")")
	if end < 0 {
		return ""
	}
	return dsn[start : start+end]
}

func extractDBNameFromDSN(dsn string) string {
	idx := strings.LastIndex(dsn, "/")
	if idx < 0 {
		return ""
	}
	rest := dsn[idx+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		rest = rest[:q]
	}
	return rest
}

func (c *mysqlCacheStore) Stats() CacheStats {
	c.mu.RLock()
	maxItems := c.maxItems
	ttlSeconds := int(c.ttl / time.Second)
	dsn := c.dsn
	dbName := extractDBNameFromDSN(dsn)
	c.mu.RUnlock()

	s := CacheStats{
		Backend:    "mysql",
		File:       dsn,
		MaxItems:   maxItems,
		TTLSeconds: ttlSeconds,
		ServerAddr: extractServerAddrFromDSN(dsn),
	}
	var count int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM chinacheck_cache").Scan(&count); err == nil {
		s.Count = count
	}
	// DATA_LENGTH + INDEX_LENGTH 近似表占用字节数，用在 Compact 前后对比
	if dbName != "" {
		var size int64
		err := c.db.QueryRow(
			`SELECT IFNULL(SUM(DATA_LENGTH + INDEX_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME='chinacheck_cache'`,
			dbName,
		).Scan(&size)
		if err == nil {
			s.FileSizeByte = size
		}
	}
	return s
}

func (c *mysqlCacheStore) get(key string) (json.RawMessage, bool) {
	c.mu.RLock()
	c.mu.RUnlock()

	var value []byte
	var expireAt sql.NullInt64
	err := c.db.QueryRow("SELECT value, expire_at FROM chinacheck_cache WHERE cache_key=?", key).Scan(&value, &expireAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false
		}
		return nil, false
	}
	now := time.Now().Unix()
	if expireAt.Valid && expireAt.Int64 < now {
		c.mu.Lock()
		_, _ = c.db.Exec("DELETE FROM chinacheck_cache WHERE cache_key=? AND expire_at IS NOT NULL AND expire_at<?", key, now)
		c.mu.Unlock()
		return nil, false
	}
	return json.RawMessage(value), true
}

func (c *mysqlCacheStore) set(key string, value json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	savedAt := now.Unix()
	expireAt := now.Add(c.ttl).Unix()

	_, err := c.db.Exec(
		"INSERT INTO chinacheck_cache(cache_key,value,saved_at,expire_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE value=VALUES(value), saved_at=VALUES(saved_at), expire_at=VALUES(expire_at)",
		key, []byte(value), savedAt, expireAt,
	)
	if err != nil {
		return fmt.Errorf("chinacheck/mysql: set failed: %w", err)
	}

	var count int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM chinacheck_cache").Scan(&count); err == nil && count > c.maxItems {
		_ = c.compactLocked()
	}
	return nil
}

func (c *mysqlCacheStore) GetSearch(lang, query string, limit int) (*SearchResult, bool) {
	raw, ok := c.get(searchKey(lang, query, limit))
	if !ok {
		return nil, false
	}
	var out SearchResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return &out, true
}

func (c *mysqlCacheStore) SetSearch(lang, query string, limit int, v *SearchResult) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.set(searchKey(lang, query, limit), raw)
}

func (c *mysqlCacheStore) GetSnapshot(companyID, query string) (*SnapshotResponse, bool) {
	var raw json.RawMessage
	var ok bool
	if companyID != "" {
		raw, ok = c.get(snapshotKeyByID(companyID))
	}
	if !ok && query != "" {
		raw, ok = c.get(snapshotKeyByQuery(query))
	}
	if !ok {
		return nil, false
	}
	var out SnapshotResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return &out, true
}

func (c *mysqlCacheStore) SetSnapshot(companyID, query string, v *SnapshotResponse) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if companyID != "" {
		if err := c.set(snapshotKeyByID(companyID), raw); err != nil {
			return err
		}
	}
	if query != "" {
		if err := c.set(snapshotKeyByQuery(query), raw); err != nil {
			return err
		}
	}
	return nil
}

func (c *mysqlCacheStore) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec("DELETE FROM chinacheck_cache")
	return err
}

func (c *mysqlCacheStore) compactLocked() error {
	now := time.Now().Unix()
	if _, err := c.db.Exec("DELETE FROM chinacheck_cache WHERE expire_at IS NOT NULL AND expire_at<?", now); err != nil {
		return err
	}
	var count int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM chinacheck_cache").Scan(&count); err != nil {
		return err
	}
	if count <= c.maxItems {
		return nil
	}
	removeN := count - c.maxItems
	rows, err := c.db.Query("SELECT cache_key FROM chinacheck_cache ORDER BY saved_at ASC LIMIT ?", removeN)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	const batch = 500
	for i := 0; i < len(keys); i += batch {
		end := i + batch
		if end > len(keys) {
			end = len(keys)
		}
		batchKeys := keys[i:end]
		placeholders := make([]string, len(batchKeys))
		args := make([]interface{}, len(batchKeys))
		for idx, k := range batchKeys {
			placeholders[idx] = "?"
			args[idx] = k
		}
		q := "DELETE FROM chinacheck_cache WHERE cache_key IN (" + strings.Join(placeholders, ",") + ")"
		if _, err := c.db.Exec(q, args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *mysqlCacheStore) Compact() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compactLocked()
}

var _ CacheStore = (*mysqlCacheStore)(nil)
