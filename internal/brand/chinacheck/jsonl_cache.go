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

// jsonlStore JSON Lines 文件实现的 CacheStore（零依赖默认后端）。
//
// 特性：
//   - 每行一个 cacheEntry JSON，追加写入；加载时按 key 去重保留最新
//   - 过期条目在 get 时懒删除，set 超量或 Compact() 时重写压缩
//   - 并发安全（内部 RWMutex）
type jsonlStore struct {
	mu       sync.RWMutex
	filePath string
	items    map[string]cacheEntry
	maxItems int
	ttl      time.Duration
}

// 默认缓存参数。
const (
	defaultTTL       = 30 * 24 * time.Hour
	defaultMaxItems  = 10000
	defaultCacheFile = "geo_chinacheck_cache.jsonl"
)

// cacheEntry 单条缓存（序列化到 JSONL 每行）。
type cacheEntry struct {
	Key      string          `json:"k"`
	Value    json.RawMessage `json:"v"`
	SavedAt  time.Time       `json:"t"`
	ExpireAt time.Time       `json:"e,omitempty"`
}

// newJSONLCache 创建 JSONL 后端缓存，filePath 为空用默认。
func newJSONLCache(filePath string, opts ...CacheOption) (*jsonlStore, error) {
	if filePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("chinacheck/jsonl: 无法获取用户主目录: %w", err)
		}
		dir := filepath.Join(home, ".cache", "geo")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("chinacheck/jsonl: 创建缓存目录失败: %w", err)
		}
		filePath = filepath.Join(dir, defaultCacheFile)
	}
	c := &jsonlStore{
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

func (c *jsonlStore) Path() string    { return c.filePath }
func (c *jsonlStore) Backend() string { return "jsonl" }

func (c *jsonlStore) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := CacheStats{
		Backend:    "jsonl",
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

// ---- search / snapshot key helpers ----

func searchKey(lang, query string, limit int) string {
	if limit <= 0 {
		limit = 0
	}
	return fmt.Sprintf("s|%s|%d|%s", lang, limit, query)
}

func snapshotKeyByID(companyID string) string { return "p|" + companyID }
func snapshotKeyByQuery(query string) string { return "q|" + query }

func (c *jsonlStore) GetSearch(lang, query string, limit int) (*SearchResult, bool) {
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

func (c *jsonlStore) SetSearch(lang, query string, limit int, v *SearchResult) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.set(searchKey(lang, query, limit), raw)
}

func (c *jsonlStore) GetSnapshot(companyID, query string) (*SnapshotResponse, bool) {
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

func (c *jsonlStore) SetSnapshot(companyID, query string, v *SnapshotResponse) error {
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

// ---- 内部 get/set ----

func (c *jsonlStore) get(key string) (json.RawMessage, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	now := time.Now()
	if !entry.ExpireAt.IsZero() && now.After(entry.ExpireAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil, false
	}
	return entry.Value, true
}

func (c *jsonlStore) set(key string, value json.RawMessage) error {
	now := time.Now()
	entry := cacheEntry{
		Key:      key,
		Value:    append(json.RawMessage(nil), value...),
		SavedAt:  now,
		ExpireAt: now.Add(c.ttl),
	}
	c.mu.Lock()
	c.items[key] = entry
	overLimit := len(c.items) - c.maxItems
	c.mu.Unlock()

	if err := c.appendEntry(entry); err != nil {
		return fmt.Errorf("chinacheck/jsonl 落盘失败: %w", err)
	}
	if overLimit > 0 {
		if err := c.evictOldAndCompact(overLimit); err != nil {
			fmt.Fprintf(os.Stderr, "[chinacheck/jsonl 警告] 压缩失败: %v\n", err)
		}
	}
	return nil
}

func (c *jsonlStore) appendEntry(e cacheEntry) error {
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

func (c *jsonlStore) load() error {
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
			continue
		}
		c.items[e.Key] = e
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("chinacheck/jsonl 读取缓存文件失败: %w", err)
	}
	return nil
}

func (c *jsonlStore) evictOldAndCompact(overLimit int) error {
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
	sort.Slice(all, func(i, j int) bool { return all[i].ent.SavedAt.Before(all[j].ent.SavedAt) })
	if overLimit > len(all) {
		overLimit = len(all)
	}
	remaining := all[overLimit:]
	c.items = make(map[string]cacheEntry, len(remaining))
	for _, x := range remaining {
		c.items[x.key] = x.ent
	}

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

func (c *jsonlStore) Clear() error {
	c.mu.Lock()
	c.items = map[string]cacheEntry{}
	c.mu.Unlock()
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *jsonlStore) Compact() error { return c.evictOldAndCompact(0) }

// 编译期断言：jsonlStore 必须完整实现 CacheStore。
var _ CacheStore = (*jsonlStore)(nil)
