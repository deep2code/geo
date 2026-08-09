package chinacheck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 默认缓存参数。
const (
	defaultTTL       = 30 * 24 * time.Hour // 默认 TTL 30 天（工商数据变更频率低）
	defaultMaxItems  = 10000               // 默认最多缓存条目
	defaultCacheFile = "geo_chinacheck_cache.jsonl"
)

// cacheEntry 单个缓存条目（序列化到 JSONL 每行）。
type cacheEntry struct {
	Key      string          `json:"k"`
	Value    json.RawMessage `json:"v"`
	SavedAt  time.Time       `json:"t"`
	ExpireAt time.Time       `json:"e,omitempty"`
}

// Cache China-Check MCP 查询结果本地持久化缓存。
//
// 双 key 策略：
//   - Search：   s|<lang>|<limit>|<query>  →  SearchResult
//   - Snapshot： p|<company_id>  或  q|<query>  →  SnapshotResponse
//
// 存储格式：每行一个 JSON（JSONL），追加写入；加载时按 key 去重保留最新；
// 淘汰策略：超过 MaxItems 时按 SavedAt 从小到大淘汰最老；过期条目加载/读取时剔除。
// 并发安全（内部 RWMutex）。
type Cache struct {
	mu       sync.RWMutex
	filePath string
	items    map[string]cacheEntry
	maxItems int
	ttl      time.Duration
}

// CacheOption Cache 配置选项。
type CacheOption func(*Cache)

// WithMaxItems 设置最大缓存条目（超过后淘汰最老）。
func WithMaxItems(n int) CacheOption {
	return func(c *Cache) {
		if n > 0 {
			c.maxItems = n
		}
	}
}

// WithTTL 设置单个条目 TTL（过期视为失效，查询不命中）。
func WithTTL(ttl time.Duration) CacheOption {
	return func(c *Cache) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// NewCache 创建本地缓存并从 filePath 加载已有条目（文件不存在则空缓存）。
// filePath 为空时使用默认路径 ~/.cache/geo/geo_chinacheck_cache.jsonl。
func NewCache(filePath string, opts ...CacheOption) (*Cache, error) {
	if filePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("无法获取用户主目录: %w", err)
		}
		dir := filepath.Join(home, ".cache", "geo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建缓存目录失败: %w", err)
		}
		filePath = filepath.Join(dir, defaultCacheFile)
	}
	c := &Cache{
		filePath: filePath,
		items:    map[string]cacheEntry{},
		maxItems: defaultMaxItems,
		ttl:      defaultTTL,
	}
	for _, o := range opts {
		o(c)
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// Path 返回实际缓存文件路径。
func (c *Cache) Path() string { return c.filePath }

// Stats 返回缓存统计。
type CacheStats struct {
	File         string `json:"file"`
	Count        int    `json:"count"`
	MaxItems     int    `json:"max_items"`
	TTLSeconds   int    `json:"ttl_seconds"`
	FileSizeByte int64  `json:"file_size_bytes,omitempty"`
}

func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := CacheStats{
		File:       c.filePath,
		Count:      len(c.items),
		MaxItems:   c.maxItems,
		TTLSeconds: int(c.ttl / time.Second),
	}
	if info, err := os.Stat(c.filePath); err == nil {
		s.FileSizeByte = info.Size()
	}
	return s
}

// ---------- 对外：search / snapshot 缓存 ----------

// searchKey 生成 Search 缓存 key。
func searchKey(lang, query string, limit int) string {
	if limit <= 0 {
		limit = 0
	}
	return fmt.Sprintf("s|%s|%d|%s", lang, limit, query)
}

// snapshotKeyByID 按公司 ID 生成 snapshot 缓存 key（优先级更高）。
func snapshotKeyByID(companyID string) string { return "p|" + companyID }

// snapshotKeyByQuery 按查询词生成 snapshot 缓存 key。
func snapshotKeyByQuery(query string) string { return "q|" + query }

// GetSearch 获取 search 缓存；未命中或过期返回 (nil, false)。
func (c *Cache) GetSearch(lang, query string, limit int) (*SearchResult, bool) {
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

// SetSearch 写入 search 缓存并落盘。
func (c *Cache) SetSearch(lang, query string, limit int, v *SearchResult) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.set(searchKey(lang, query, limit), raw)
}

// GetSnapshot 获取 snapshot 缓存（先 ID 再 query）。
func (c *Cache) GetSnapshot(companyID, query string) (*SnapshotResponse, bool) {
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

// SetSnapshot 写入 snapshot 缓存并落盘（同时写 ID key 和 query key 如果提供了）。
func (c *Cache) SetSnapshot(companyID, query string, v *SnapshotResponse) error {
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

// ---------- 内部：读写核心 ----------

func (c *Cache) get(key string) (json.RawMessage, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	now := time.Now()
	if !entry.ExpireAt.IsZero() && now.After(entry.ExpireAt) {
		// 过期：懒删除（不写回文件，下次 save 会和其他条目一起重写压缩时剔除）
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.Value, true
}

func (c *Cache) set(key string, value json.RawMessage) error {
	now := time.Now()
	entry := cacheEntry{
		Key:      key,
		Value:    append(json.RawMessage(nil), value...), // copy 避免外部修改
		SavedAt:  now,
		ExpireAt: now.Add(c.ttl),
	}
	c.mu.Lock()
	// 已存在？直接覆盖内存即可，落盘追加；加载时按"后出现覆盖先出现"去重
	c.items[key] = entry
	overLimit := len(c.items) - c.maxItems
	c.mu.Unlock()

	// 追加写入 JSONL
	if err := c.appendEntry(entry); err != nil {
		return fmt.Errorf("缓存落盘失败: %w", err)
	}

	// 超过容量上限：淘汰最老 N 条 + 重写压缩整个文件
	if overLimit > 0 {
		if err := c.evictOldAndCompact(overLimit); err != nil {
			// 压缩失败不影响当前写入，仅打 stderr 提示
			fmt.Fprintf(os.Stderr, "[chinacheck cache 警告] 压缩失败: %v\n", err)
		}
	}
	return nil
}

// appendEntry 追加单条到文件末尾。
func (c *Cache) appendEntry(e cacheEntry) error {
	f, err := os.OpenFile(c.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// load 从 JSONL 文件全量加载，相同 key 取最后一条（天然支持 set 覆盖）。
func (c *Cache) load() error {
	f, err := os.Open(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	now := time.Now()
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e cacheEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if !e.ExpireAt.IsZero() && now.After(e.ExpireAt) {
			continue // 过期跳过
		}
		c.items[e.Key] = e
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("读取缓存文件失败: %w", err)
	}
	return nil
}

// evictOldAndCompact 淘汰最老的 overLimit 条，并重写整个缓存文件（剔除已删条目=压缩）。
func (c *Cache) evictOldAndCompact(overLimit int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	type kv struct {
		key string
		ent cacheEntry
	}
	all := make([]kv, 0, len(c.items))
	for k, v := range c.items {
		all = append(all, kv{k, v})
	}
	// 按 SavedAt 升序：最老的在前
	sort.Slice(all, func(i, j int) bool { return all[i].ent.SavedAt.Before(all[j].ent.SavedAt) })

	// 淘汰 overLimit 条最老的
	if overLimit > len(all) {
		overLimit = len(all)
	}
	remaining := all[overLimit:]
	c.items = make(map[string]cacheEntry, len(remaining))
	for _, x := range remaining {
		c.items[x.key] = x.ent
	}

	// 全量重写（临时文件 + rename 原子替换）
	tmpPath := c.filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 256*1024)
	enc := json.NewEncoder(w)
	for _, x := range remaining {
		if err := enc.Encode(x.ent); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, c.filePath)
}

// Clear 清空缓存（删除文件 + 重置内存）。
func (c *Cache) Clear() error {
	c.mu.Lock()
	c.items = map[string]cacheEntry{}
	c.mu.Unlock()
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Compact 手动压缩/去重缓存文件（同时剔除过期条目）。
func (c *Cache) Compact() error { return c.evictOldAndCompact(0) }
