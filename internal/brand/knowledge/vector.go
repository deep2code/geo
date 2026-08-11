// Package knowledge 向量检索与 Embedding 支持。
//
// 本文件提供：
//  1. 本地 TF-IDF 向量存储（无需外部 API，开箱即用）
//  2. OpenAI 兼容 Embedding API 接口（预留，通过环境变量启用）
//  3. 混合向量存储（HybridVectorStore）：优先 Embedding API，回退本地 TF-IDF
//
// 设计目标：
//   - 零外部依赖（仅用标准库）
//   - 线程安全，支持并发读
//   - 接口与实现分离，便于后续扩展（如换用其他 Embedding 服务）
package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ============================================================
// 1. 公共接口与结果类型
// ============================================================

// VectorStore 向量存储接口
type VectorStore interface {
	// Add 添加文档到向量存储
	Add(id string, text string, metadata map[string]string) error
	// Search 语义搜索，返回 topK 最相似文档
	Search(query string, topK int) []VectorSearchResult
	// Size 返回文档数量
	Size() int
}

// VectorSearchResult 向量搜索结果
type VectorSearchResult struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ============================================================
// 2. 分词器（中文 2-gram + 英文按词）
// ============================================================

// tokenize 中文+英文混合分词
// 英文：按空格和标点分割，转小写
// 中文：2-gram 滑动窗口（如"品牌画像" → "品牌", "牌画", "画像"）
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	var cnBuf []rune // 当前连续中文段
	var enBuf []rune // 当前连续英文/数字段

	flushCN := func() {
		n := len(cnBuf)
		if n == 0 {
			return
		}
		if n == 1 {
			// 单字也作为 token，保证短查询的召回
			tokens = append(tokens, string(cnBuf))
		} else {
			// 2-gram 滑动窗口
			for i := 0; i+1 < n; i++ {
				tokens = append(tokens, string(cnBuf[i:i+2]))
			}
		}
		cnBuf = cnBuf[:0]
	}

	flushEN := func() {
		if len(enBuf) == 0 {
			return
		}
		s := strings.ToLower(string(enBuf))
		if s != "" {
			tokens = append(tokens, s)
		}
		enBuf = enBuf[:0]
	}

	for _, r := range text {
		switch {
		case isCJK(r):
			flushEN()
			cnBuf = append(cnBuf, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCN()
			enBuf = append(enBuf, r)
		default:
			// 空白、标点等：作为分隔符，刷新两段
			flushCN()
			flushEN()
		}
	}
	flushCN()
	flushEN()
	return tokens
}

// isCJK 判断是否为中文字符（CJK 统一表意文字及相关区段）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Unified Ideographs Extension A
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0x3000 && r <= 0x303F) // CJK Symbols and Punctuation
}

// ============================================================
// 3. 本地 TF-IDF 向量存储
// ============================================================

// LocalTFIDFStore 本地 TF-IDF 向量存储，无需外部 API
// 使用词频-逆文档频率（TF-IDF）将文本向量化，余弦相似度检索
type LocalTFIDFStore struct {
	mu        sync.RWMutex
	docs      []tfidfDoc            // 文档集合
	docFreq   map[string]int        // 每个词的文档频率
	totalDocs int                   // 文档总数
	tokenizer func(string) []string // 分词器
	dirty     bool                  // 向量是否需要重算
}

// tfidfDoc TF-IDF 文档结构
type tfidfDoc struct {
	id         string
	text       string
	termFreq   map[string]int     // 原始词频（用于 lazy 计算 TF-IDF）
	totalTerms int                // 文档总词数
	vector     map[string]float64 // TF-IDF 向量（lazy 计算）
	norm       float64            // 向量 L2 范数（预计算）
	metadata   map[string]string
}

// NewLocalTFIDFStore 创建本地 TF-IDF 向量存储
func NewLocalTFIDFStore() *LocalTFIDFStore {
	return &LocalTFIDFStore{
		docs:      make([]tfidfDoc, 0),
		docFreq:   make(map[string]int),
		tokenizer: tokenize,
		dirty:     false,
	}
}

// Add 添加文档到向量存储
func (s *LocalTFIDFStore) Add(id string, text string, metadata map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := s.tokenizer(text)
	termFreq := make(map[string]int, len(tokens))
	for _, t := range tokens {
		termFreq[t]++
	}
	// 更新文档频率（每个词在本文档中出现则 +1）
	for term := range termFreq {
		s.docFreq[term]++
	}
	s.totalDocs++
	s.docs = append(s.docs, tfidfDoc{
		id:         id,
		text:       text,
		termFreq:   termFreq,
		totalTerms: len(tokens),
		metadata:   metadata,
	})
	s.dirty = true
	return nil
}

// finalize 重算所有文档的 TF-IDF 向量与范数（必须在写锁内调用）。
// TF-IDF 计算：tf = 词频/文档总词数，idf = log(总文档数 / (1 + 含该词文档数))
func (s *LocalTFIDFStore) finalize() {
	if !s.dirty || s.totalDocs == 0 {
		return
	}
	N := s.totalDocs
	for i := range s.docs {
		d := &s.docs[i]
		vec := make(map[string]float64, len(d.termFreq))
		var sumSq float64
		for term, cnt := range d.termFreq {
			if d.totalTerms == 0 {
				continue
			}
			tf := float64(cnt) / float64(d.totalTerms)
			idf := math.Log(float64(N) / float64(1+s.docFreq[term]))
			w := tf * idf
			vec[term] = w
			sumSq += w * w
		}
		d.vector = vec
		d.norm = math.Sqrt(sumSq)
	}
	s.dirty = false
}

// Search 语义搜索，返回 topK 最相似文档
func (s *LocalTFIDFStore) Search(query string, topK int) []VectorSearchResult {
	if topK <= 0 {
		topK = 5
	}
	// 确保向量已计算
	s.mu.Lock()
	if s.dirty {
		s.finalize()
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.docs) == 0 {
		return nil
	}

	// 计算 query 的 TF-IDF 向量（复用全局 idf）
	qTokens := s.tokenizer(query)
	if len(qTokens) == 0 {
		return nil
	}
	qFreq := make(map[string]int, len(qTokens))
	for _, t := range qTokens {
		qFreq[t]++
	}
	N := s.totalDocs
	qVec := make(map[string]float64, len(qFreq))
	var qSumSq float64
	for term, cnt := range qFreq {
		tf := float64(cnt) / float64(len(qTokens))
		idf := math.Log(float64(N) / float64(1+s.docFreq[term]))
		w := tf * idf
		qVec[term] = w
		qSumSq += w * w
	}
	qNorm := math.Sqrt(qSumSq)
	if qNorm == 0 {
		return nil
	}

	// 计算与每个文档的余弦相似度：cos(a,b) = dot(a,b) / (|a| * |b|)
	type scored struct {
		idx   int
		score float64
	}
	results := make([]scored, 0, len(s.docs))
	for i := range s.docs {
		d := &s.docs[i]
		if d.norm == 0 {
			continue
		}
		// 点积：在较小的 vector 上迭代以减少计算量
		var dot float64
		if len(qVec) <= len(d.vector) {
			for term, w := range qVec {
				if dw, ok := d.vector[term]; ok {
					dot += w * dw
				}
			}
		} else {
			for term, dw := range d.vector {
				if w, ok := qVec[term]; ok {
					dot += w * dw
				}
			}
		}
		score := dot / (qNorm * d.norm)
		results = append(results, scored{i, score})
	}

	// 排序取 topK
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	out := make([]VectorSearchResult, 0, min(topK, len(results)))
	for k := 0; k < len(results) && k < topK; k++ {
		// 跳过零分结果（无共现词）
		if results[k].score <= 0 {
			continue
		}
		d := &s.docs[results[k].idx]
		out = append(out, VectorSearchResult{
			ID:       d.id,
			Text:     d.text,
			Score:    results[k].score,
			Metadata: d.metadata,
		})
	}
	return out
}

// Size 返回文档数量
func (s *LocalTFIDFStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// ============================================================
// 4. Embedding API 接口（预留）
// ============================================================

// EmbeddingProvider Embedding API 接口（预留）
type EmbeddingProvider interface {
	// Embed 生成单条文本的向量
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedBatch 批量生成向量
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions 返回向量维度
	Dimensions() int
	// Available 是否可用（如配置齐全、API 可达）
	Available() bool
}

// OpenAIEmbedding OpenAI 兼容 Embedding API
// 环境变量：GEO_EMBEDDING_KEY, GEO_EMBEDDING_BASE, GEO_EMBEDDING_MODEL
type OpenAIEmbedding struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
	// 缓存的向量维度（首次 Embed 成功后填充）
	dim int
	mu  sync.Mutex
}

// NewOpenAIEmbedding 从环境变量创建 OpenAI 兼容 Embedding 客户端
func NewOpenAIEmbedding() *OpenAIEmbedding {
	return &OpenAIEmbedding{
		apiKey:  strings.TrimSpace(os.Getenv("GEO_EMBEDDING_KEY")),
		baseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("GEO_EMBEDDING_BASE")), "/"),
		model:   strings.TrimSpace(os.Getenv("GEO_EMBEDDING_MODEL")),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Available 是否可用（环境变量齐全）
func (e *OpenAIEmbedding) Available() bool {
	return e != nil && e.apiKey != "" && e.baseURL != "" && e.model != ""
}

// Dimensions 返回向量维度（首次 Embed 成功后才知道，否则返回 0）
func (e *OpenAIEmbedding) Dimensions() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dim
}

// embeddingRequest OpenAI embeddings API 请求体
type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embeddingResponse OpenAI embeddings API 响应体
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// Embed 生成单条文本的向量
func (e *OpenAIEmbedding) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embedding 返回空结果")
	}
	return vecs[0], nil
}

// EmbedBatch 批量生成向量
// 实际调用 POST {baseURL}/v1/embeddings，Available()=false 时返回错误。
func (e *OpenAIEmbedding) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if !e.Available() {
		return nil, fmt.Errorf("embedding 服务未配置（需要 GEO_EMBEDDING_KEY / GEO_EMBEDDING_BASE / GEO_EMBEDDING_MODEL）")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embeddingRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := e.baseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 embedding API 失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API 返回 %d: %s", resp.StatusCode, string(raw))
	}

	var er embeddingResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("embedding API 错误: %s", er.Error.Message)
	}

	// 按 index 排序保证顺序与输入一致
	sort.Slice(er.Data, func(i, j int) bool {
		return er.Data[i].Index < er.Data[j].Index
	})
	out := make([][]float32, 0, len(er.Data))
	for _, d := range er.Data {
		out = append(out, d.Embedding)
	}

	// 缓存维度
	if len(out) > 0 && len(out[0]) > 0 {
		e.mu.Lock()
		e.dim = len(out[0])
		e.mu.Unlock()
	}
	return out, nil
}

// ============================================================
// 5. 混合向量存储
// ============================================================

// HybridVectorStore 混合向量存储
// 优先使用 Embedding API（如已配置），回退到本地 TF-IDF
type HybridVectorStore struct {
	embedding EmbeddingProvider // 可为 nil
	local     *LocalTFIDFStore

	mu           sync.RWMutex
	embeddedDocs []embeddedDoc // 仅当 embedding 可用时填充
}

// embeddedDoc 已生成 embedding 向量的文档
type embeddedDoc struct {
	id       string
	text     string
	vector   []float32
	norm     float64
	metadata map[string]string
}

// NewHybridVectorStore 创建混合向量存储
func NewHybridVectorStore() *HybridVectorStore {
	return &HybridVectorStore{
		embedding: NewOpenAIEmbedding(),
		local:     NewLocalTFIDFStore(),
	}
}

// Add 添加文档到向量存储
func (h *HybridVectorStore) Add(id string, text string, metadata map[string]string) error {
	// 总是加入本地 TF-IDF（作为兜底）
	if err := h.local.Add(id, text, metadata); err != nil {
		return err
	}
	// 若 embedding 可用，生成向量并缓存
	if h.embedding != nil && h.embedding.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		vec, err := h.embedding.Embed(ctx, text)
		if err == nil && len(vec) > 0 {
			h.mu.Lock()
			h.embeddedDocs = append(h.embeddedDocs, embeddedDoc{
				id:       id,
				text:     text,
				vector:   vec,
				norm:     l2NormFloat32(vec),
				metadata: metadata,
			})
			h.mu.Unlock()
		}
		// embedding 失败不影响整体 Add（local 仍可用）
	}
	return nil
}

// Search 语义搜索：优先 embedding，回退本地 TF-IDF
func (h *HybridVectorStore) Search(query string, topK int) []VectorSearchResult {
	if topK <= 0 {
		topK = 5
	}
	// 若 embedding 可用且有向量数据，走 embedding 检索
	if h.embedding != nil && h.embedding.Available() {
		h.mu.RLock()
		n := len(h.embeddedDocs)
		h.mu.RUnlock()
		if n > 0 {
			return h.searchByEmbedding(query, topK)
		}
	}
	// 否则回退本地 TF-IDF
	return h.local.Search(query, topK)
}

// searchByEmbedding 用 embedding API 检索（余弦相似度）
func (h *HybridVectorStore) searchByEmbedding(query string, topK int) []VectorSearchResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	qVec, err := h.embedding.Embed(ctx, query)
	if err != nil || len(qVec) == 0 {
		// embedding 失败，回退本地
		return h.local.Search(query, topK)
	}
	qNorm := l2NormFloat32(qVec)
	if qNorm == 0 {
		return h.local.Search(query, topK)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	type scored struct {
		idx   int
		score float64
	}
	results := make([]scored, 0, len(h.embeddedDocs))
	for i := range h.embeddedDocs {
		d := &h.embeddedDocs[i]
		if d.norm == 0 {
			continue
		}
		dot := dotProductFloat32(qVec, d.vector)
		score := dot / (qNorm * d.norm)
		results = append(results, scored{i, score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	out := make([]VectorSearchResult, 0, min(topK, len(results)))
	for k := 0; k < len(results) && k < topK; k++ {
		d := &h.embeddedDocs[results[k].idx]
		out = append(out, VectorSearchResult{
			ID:       d.id,
			Text:     d.text,
			Score:    results[k].score,
			Metadata: d.metadata,
		})
	}
	return out
}

// Size 返回文档数量
func (h *HybridVectorStore) Size() int {
	return h.local.Size()
}

// ============================================================
// 6. 向量工具函数
// ============================================================

// l2NormFloat32 计算 float32 向量的 L2 范数
func l2NormFloat32(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// dotProductFloat32 计算两个 float32 向量的点积
func dotProductFloat32(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
