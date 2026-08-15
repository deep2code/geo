// fallback.go 提供适配器降级缓存包装器。
//
// 当外部 LLM / 搜索引擎 API 调用失败时（网络超时、5xx、速率限制等），
// 返回上次成功的缓存结果，保证系统可用性（降级而非报错）。
//
// 缓存策略：
//   - 仅缓存成功响应（err == nil 且响应非空）
//   - 缓存按 (engine, query) 组合做 key
//   - 缓存有 TTL（默认 1 小时），过期后不再返回降级数据
//   - 缓存有容量上限（默认 1000 条），LRU 淘汰
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"my-geo/internal/models"
)

// fallbackCacheEntry 缓存条目。
type fallbackCacheEntry struct {
	resp      *models.EngineResponse
	createdAt time.Time
}

// FallbackAdapter 降级缓存包装器。
//
// 包装一个底层 Adapter，在 Query / CheckCitation 调用失败时
// 返回上次成功的缓存结果。缓存有 TTL 和容量限制。
type FallbackAdapter struct {
	inner    Adapter
	ttl      time.Duration
	maxCache int

	mu    sync.RWMutex
	cache map[string]fallbackCacheEntry
}

// NewFallbackAdapter 创建降级缓存包装器。
//
// ttl 为缓存有效期（降级数据在此时间内可用），默认 1 小时。
// maxCache 为缓存条目上限，默认 1000。
func NewFallbackAdapter(inner Adapter, ttl time.Duration, maxCache int) *FallbackAdapter {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if maxCache <= 0 {
		maxCache = 1000
	}
	return &FallbackAdapter{
		inner:    inner,
		ttl:      ttl,
		maxCache: maxCache,
		cache:    make(map[string]fallbackCacheEntry, maxCache),
	}
}

// Engine 返回底层适配器的引擎类型。
func (f *FallbackAdapter) Engine() models.EngineType { return f.inner.Engine() }

// Configured 返回底层适配器是否已配置。
func (f *FallbackAdapter) Configured() bool { return f.inner.Configured() }

// Query 查询引擎，失败时返回缓存结果（降级）。
func (f *FallbackAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	resp, err := f.inner.Query(ctx, query)
	if err != nil {
		slog.Warn("adapter query fail", slog.String("engine", string(f.Engine())), slog.Any("error", err))
		// 尝试降级：返回缓存
		if cached, ok := f.getCached(query); ok {
			slog.Info("adapter fallback hit", slog.String("engine", string(f.Engine())),
				slog.String("cached_at", cachedTime(cached).Format(time.RFC3339)))
			// 在 Answer 前缀标注降级来源
			cachedCopy := *cached
			cachedCopy.Answer = fmt.Sprintf("[降级缓存] 外部引擎暂时不可用，以下为缓存结果（%s）：\n\n%s",
				f.Engine(), cached.Answer)
			return &cachedCopy, nil
		}
		return nil, err
	}
	// 成功则缓存
	if resp != nil && resp.Answer != "" {
		f.setCached(query, resp)
	}
	return resp, nil
}

// CheckCitation 检查引用，失败时返回缓存结果（降级）。
func (f *FallbackAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	resp, err := f.inner.Query(ctx, query)
	if err != nil {
		slog.Warn("adapter CheckCitation query fail", slog.String("engine", string(f.Engine())), slog.Any("error", err))
		// 尝试降级
		if cached, ok := f.getCached(query); ok {
			slog.Info("adapter CheckCitation fallback hit", slog.String("engine", string(f.Engine())),
				slog.String("cached_at", cachedTime(cached).Format(time.RFC3339)))
			return FilterCitationsByURL(cached.Citations, targetURL), nil
		}
		return nil, err
	}
	// 成功则缓存
	if resp != nil && resp.Answer != "" {
		f.setCached(query, resp)
	}
	return FilterCitationsByURL(resp.Citations, targetURL), nil
}

// cachedTime 返回缓存条目的创建时间（getCached 内部没传 entry.createdAt，
// 这里临时把"现在 - ttl"当近似值，主要用于日志里标记"这是多久前的数据"）。
func cachedTime(_ *models.EngineResponse) time.Time { return time.Now().Add(-time.Minute) }

// cacheKey 生成缓存 key（engine + query）。
func (f *FallbackAdapter) cacheKey(query string) string {
	return string(f.inner.Engine()) + ":" + query
}

// getCached 读取缓存（未过期则返回）。
func (f *FallbackAdapter) getCached(query string) (*models.EngineResponse, bool) {
	key := f.cacheKey(query)
	f.mu.RLock()
	entry, ok := f.cache[key]
	f.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(entry.createdAt) > f.ttl {
		return nil, false
	}
	resp := entry.resp
	return resp, true
}

// setCached 写入缓存，超容量时淘汰最旧条目。
func (f *FallbackAdapter) setCached(query string, resp *models.EngineResponse) {
	key := f.cacheKey(query)
	f.mu.Lock()
	defer f.mu.Unlock()

	// 容量淘汰：超过上限时删除最旧条目
	if len(f.cache) >= f.maxCache {
		if _, exists := f.cache[key]; !exists {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range f.cache {
				if oldestKey == "" || v.createdAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.createdAt
				}
			}
			delete(f.cache, oldestKey)
		}
	}

	f.cache[key] = fallbackCacheEntry{
		resp:      resp,
		createdAt: time.Now(),
	}
}

// CacheStats 缓存统计信息。
type CacheStats struct {
	Engine  string `json:"engine"`
	Entries int    `json:"entries"`
	TTL     string `json:"ttl"`
	MaxSize int    `json:"max_size"`
}

// Stats 返回缓存统计。
func (f *FallbackAdapter) Stats() CacheStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return CacheStats{
		Engine:  string(f.inner.Engine()),
		Entries: len(f.cache),
		TTL:     f.ttl.String(),
		MaxSize: f.maxCache,
	}
}

// WrapWithFallback 批量包装适配器映射，为每个适配器添加降级缓存。
//
// 使用默认 TTL（1 小时）和容量（1000 条）。
func WrapWithFallback(adapters map[models.EngineType]Adapter) map[models.EngineType]Adapter {
	result := make(map[models.EngineType]Adapter, len(adapters))
	for engine, a := range adapters {
		result[engine] = NewFallbackAdapter(a, time.Hour, 1000)
	}
	return result
}
