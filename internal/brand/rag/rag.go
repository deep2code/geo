// Package rag 提供检索增强生成（Retrieval-Augmented Generation）能力。
//
// 在 GEO 场景中，RAG 用于：
//   - 内容生成时注入品牌专属知识（产品手册、FAQ、官方口径），避免 LLM 幻觉
//   - 品牌审计时基于自有知识库交叉验证 AI 引擎回答的事实准确性
//   - 对内容优化提供事实锚点，确保生成的 GEO 内容与品牌官方信息一致
//
// 设计要点：
//   - 复用 knowledge 包的 VectorStore 接口（本地 TF-IDF / Embedding API）
//   - 支持动态添加自定义文档（品牌资料、FAQ、产品手册等）
//   - 提供 Retrieve 检索与 AugmentPrompt 提示词增强两个核心能力
//   - 线程安全，支持并发读
package rag

import (
	"fmt"
	"strings"
	"sync"

	"my-geo/internal/brand/knowledge"
)

// Document RAG 文档。
type Document struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Source   string            `json:"source"`   // 来源：manual/file/url/faq/kb
	Title    string            `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RetrievedChunk 检索到的文档片段。
type RetrievedChunk struct {
	Document
	Score float64 `json:"score"` // 相似度得分（0-1）
}

// Store RAG 知识库存储，支持动态文档管理与向量检索。
//
// 内部使用 knowledge.VectorStore 接口实现向量化与检索，
// 同时维护文档元信息映射（ID → Document）。
type Store struct {
	mu       sync.RWMutex
	vs       knowledge.VectorStore
	docs     map[string]*Document // ID → Document
	docCount int
}

// NewStore 创建 RAG 存储，使用本地 TF-IDF 向量存储（零外部依赖）。
func NewStore() *Store {
	return &Store{
		vs:   knowledge.NewLocalTFIDFStore(),
		docs: map[string]*Document{},
	}
}

// NewStoreWithVectorStore 使用指定的向量存储创建 RAG 存储。
//
// 可传入 knowledge.NewHybridVectorStore() 以启用 Embedding API（需配置环境变量）。
func NewStoreWithVectorStore(vs knowledge.VectorStore) *Store {
	if vs == nil {
		vs = knowledge.NewLocalTFIDFStore()
	}
	return &Store{
		vs:   vs,
		docs: map[string]*Document{},
	}
}

// Add 添加文档到 RAG 知识库。
//
// 若 ID 已存在则覆盖旧文档。文档内容会被分词向量化并加入向量存储。
func (s *Store) Add(doc Document) error {
	if strings.TrimSpace(doc.ID) == "" {
		return fmt.Errorf("文档 ID 不能为空")
	}
	if strings.TrimSpace(doc.Content) == "" {
		return fmt.Errorf("文档内容不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 向量存储添加（覆盖语义由 ID 保证）
	meta := map[string]string{
		"source": doc.Source,
		"title":  doc.Title,
	}
	for k, v := range doc.Metadata {
		meta[k] = v
	}
	if err := s.vs.Add(doc.ID, doc.Content, meta); err != nil {
		return fmt.Errorf("向量化文档失败: %w", err)
	}
	// 如果是覆盖，不重复计数
	if _, exists := s.docs[doc.ID]; !exists {
		s.docCount++
	}
	d := doc
	s.docs[doc.ID] = &d
	return nil
}

// AddBatch 批量添加文档。
func (s *Store) AddBatch(docs []Document) error {
	for _, d := range docs {
		if err := s.Add(d); err != nil {
			return fmt.Errorf("添加文档 %s 失败: %w", d.ID, err)
		}
	}
	return nil
}

// Remove 删除文档（仅从元信息映射移除，向量存储中的向量保留但不会被检索到）。
//
// 注意：本地 TF-IDF 向量存储不支持删除单条文档，移除后该文档不再参与元信息查询，
// 但其向量仍残留在 TF-IDF 索引中（不影响检索正确性，仅占用少量内存）。
func (s *Store) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.docs[id]; !ok {
		return false
	}
	delete(s.docs, id)
	s.docCount--
	return true
}

// Get 按 ID 获取文档。
func (s *Store) Get(id string) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[id]
	return d, ok
}

// Size 返回文档总数。
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docCount
}

// List 列出所有文档（浅拷贝）。
func (s *Store) List() []Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Document, 0, s.docCount)
	for _, d := range s.docs {
		out = append(out, *d)
	}
	return out
}

// Retrieve 基于查询检索相关文档片段，返回 Top K 结果（按相似度降序）。
//
// topK <= 0 时默认返回 5 条。结果包含文档元信息与相似度得分。
func (s *Store) Retrieve(query string, topK int) []RetrievedChunk {
	if topK <= 0 {
		topK = 5
	}
	if strings.TrimSpace(query) == "" {
		return nil
	}
	results := s.vs.Search(query, topK)
	if len(results) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RetrievedChunk, 0, len(results))
	for _, r := range results {
		doc, ok := s.docs[r.ID]
		if !ok {
			// 向量存储中有但元信息中没有（可能已 Remove），跳过
			continue
		}
		chunk := RetrievedChunk{
			Document: *doc,
			Score:    r.Score,
		}
		// 用向量存储返回的文本覆盖（可能被截断或处理过）
		if r.Text != "" {
			chunk.Content = r.Text
		}
		out = append(out, chunk)
	}
	return out
}

// AugmentPrompt 将检索结果作为上下文注入到提示词中，增强 LLM 生成的事实准确性。
//
// 格式：
//
//	【知识库参考】以下是检索到的品牌知识（请在生成内容时参考，确保事实准确）：
//	[1] {文档标题}（来源: {source}）
//	    {文档内容}
//	[2] ...
//
//	{原始用户提示词}
func (s *Store) AugmentPrompt(query, userPrompt string) string {
	chunks := s.Retrieve(query, 3)
	if len(chunks) == 0 {
		return userPrompt
	}
	var sb strings.Builder
	sb.WriteString("【知识库参考】以下是检索到的品牌知识（请在生成内容时参考，确保事实准确，不要编造与以下信息冲突的内容）：\n")
	for i, c := range chunks {
		title := c.Title
		if title == "" {
			title = c.ID
		}
		sb.WriteString(fmt.Sprintf("[%d] %s（来源: %s，相似度: %.0f%%）\n", i+1, title, c.Source, c.Score*100))
		// 限制每条文档注入长度，避免 prompt 过长
		content := c.Content
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}
		sb.WriteString("    ")
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}
	sb.WriteString("【用户请求】\n")
	sb.WriteString(userPrompt)
	return sb.String()
}

// AugmentPromptWithCitations 与 AugmentPrompt 类似，但额外标注引用来源 ID，便于溯源。
//
// 返回增强后的 prompt 与引用的文档 ID 列表。
func (s *Store) AugmentPromptWithCitations(query, userPrompt string) (string, []string) {
	chunks := s.Retrieve(query, 3)
	if len(chunks) == 0 {
		return userPrompt, nil
	}
	var sb strings.Builder
	var citedIDs []string
	sb.WriteString("【知识库参考】以下是检索到的品牌知识（请在生成内容时参考，确保事实准确）：\n")
	for i, c := range chunks {
		title := c.Title
		if title == "" {
			title = c.ID
		}
		sb.WriteString(fmt.Sprintf("[%d] %s（来源: %s，引用ID: %s）\n", i+1, title, c.Source, c.ID))
		content := c.Content
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}
		sb.WriteString("    ")
		sb.WriteString(content)
		sb.WriteString("\n\n")
		citedIDs = append(citedIDs, c.ID)
	}
	sb.WriteString("【用户请求】\n")
	sb.WriteString(userPrompt)
	return sb.String(), citedIDs
}

// VerifyFact 事实核查：检查 statement 是否与知识库中的文档一致。
//
// 返回最相关的文档片段与相似度，若相似度 ≥ threshold（默认 0.5）则视为"有据可查"。
func (s *Store) VerifyFact(statement string, threshold float64) (*RetrievedChunk, bool) {
	if threshold <= 0 {
		threshold = 0.5
	}
	chunks := s.Retrieve(statement, 1)
	if len(chunks) == 0 {
		return nil, false
	}
	if chunks[0].Score >= threshold {
		return &chunks[0], true
	}
	return &chunks[0], false
}
