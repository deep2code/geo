// Package uniqueness 提供内容独特性检测能力。
//
// 在 GEO 场景中，批量生成的内容若彼此高度相似，会被 AI 引擎判定为低质量内容
// 从而降低引用率。本包通过双算法交叉验证检测内容重复度：
//   - MinHash（基于 n-gram 的 Jaccard 相似度估计）：快速近似，适合大规模去重
//   - 余弦相似度（基于字符 n-gram TF 向量）：精确度量文本相似性
//
// 设计要点：
//   - 仅使用标准库，零外部依赖
//   - 支持 CJK（中文 2-gram）与英文混合分词
//   - 线程安全，支持并发读与写入
//   - 提供 Detector 检测器，维护内容语料库并对外查重
package uniqueness

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// ============================================================
// 1. 分词器（复用 knowledge 包的 CJK 2-gram 策略）
// ============================================================

// tokenize 将文本分词为 token 列表（小写化）。
//
// 英文/数字按词分割，中文按 2-gram 滑动窗口分割。
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	var cnBuf []rune
	var enBuf []rune

	flushCN := func() {
		n := len(cnBuf)
		if n == 0 {
			return
		}
		if n == 1 {
			tokens = append(tokens, string(cnBuf))
		} else {
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
			flushCN()
			flushEN()
		}
	}
	flushCN()
	flushEN()
	return tokens
}

// isCJK 判断是否为中文字符。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x3000 && r <= 0x303F)
}

// ============================================================
// 2. MinHash 签名（基于 FNV-1a 的 k 个独立哈希函数）
// ============================================================

const defaultNumHash = 128

// MinHash 使用 k 个独立哈希函数计算集合的最小哈希签名。
//
// 签名长度为 numHash，可用于估计两个集合的 Jaccard 相似度。
type MinHash struct {
	numHash int
	sig     []uint64
	set     map[string]struct{}
	dirty   bool
}

// NewMinHash 创建 MinHash 签名器。
func NewMinHash(numHash int) *MinHash {
	if numHash <= 0 {
		numHash = defaultNumHash
	}
	return &MinHash{
		numHash: numHash,
		sig:     make([]uint64, numHash),
		set:     make(map[string]struct{}),
	}
}

// Push 向集合中添加一个 token。
func (m *MinHash) Push(token string) {
	m.set[token] = struct{}{}
	m.dirty = true
}

// PushAll 批量添加 token。
func (m *MinHash) PushAll(tokens []string) {
	for _, t := range tokens {
		m.set[t] = struct{}{}
	}
	if len(tokens) > 0 {
		m.dirty = true
	}
}

// Reset 清空集合，复用底层切片。
func (m *MinHash) Reset() {
	m.set = make(map[string]struct{})
	m.dirty = true
}

// Signature 返回当前集合的 MinHash 签名（lazy 计算）。
func (m *MinHash) Signature() []uint64 {
	if !m.dirty {
		return m.sig
	}
	// 使用 FNV-1a + 不同种子生成 k 个独立哈希
	for i := 0; i < m.numHash; i++ {
		m.sig[i] = math.MaxUint64
	}
	seed := uint64(0)
	for token := range m.set {
		for i := 0; i < m.numHash; i++ {
			h := fnvHash(token, seed+uint64(i)*0x9E3779B97F4A7C15)
			if h < m.sig[i] {
				m.sig[i] = h
			}
		}
	}
	m.dirty = false
	return m.sig
}

// fnvHash 带种子的 FNV-1a 哈希。
func fnvHash(s string, seed uint64) uint64 {
	h := fnv.New64a()
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(seed >> (i * 8))
	}
	h.Write(b[:])
	h.Write([]byte(s))
	return h.Sum64()
}

// Jaccard 估计两个 MinHash 签名的 Jaccard 相似度（0-1）。
func Jaccard(sig1, sig2 []uint64) float64 {
	n := len(sig1)
	if len(sig2) < n {
		n = len(sig2)
	}
	if n == 0 {
		return 0
	}
	matches := 0
	for i := 0; i < n; i++ {
		if sig1[i] == sig2[i] {
			matches++
		}
	}
	return float64(matches) / float64(n)
}

// MinHashSimilarity 计算两段文本的 MinHash Jaccard 相似度（0-1）。
//
// 内部对文本分词后构建 MinHash 签名并比较。
func MinHashSimilarity(text1, text2 string) float64 {
	tokens1 := tokenize(text1)
	tokens2 := tokenize(text2)
	if len(tokens1) == 0 || len(tokens2) == 0 {
		return 0
	}
	mh1 := NewMinHash(defaultNumHash)
	mh1.PushAll(tokens1)
	mh2 := NewMinHash(defaultNumHash)
	mh2.PushAll(tokens2)
	return Jaccard(mh1.Signature(), mh2.Signature())
}

// ============================================================
// 3. 余弦相似度（基于字符 n-gram TF 向量）
// ============================================================

// charNGrams 提取字符级 n-gram（支持 CJK 单字与英文组合）。
func charNGrams(text string, n int) []string {
	runes := []rune(text)
	if len(runes) < n {
		if len(runes) > 0 {
			return []string{string(runes)}
		}
		return nil
	}
	var grams []string
	for i := 0; i+n <= len(runes); i++ {
		grams = append(grams, string(runes[i:i+n]))
	}
	return grams
}

// termFreq 构建词频向量。
func termFreq(tokens []string) map[string]float64 {
	tf := make(map[string]float64, len(tokens))
	for _, t := range tokens {
		tf[t]++
	}
	return tf
}

// cosineSim 计算两个词频向量的余弦相似度（0-1）。
func cosineSim(vec1, vec2 map[string]float64) float64 {
	if len(vec1) == 0 || len(vec2) == 0 {
		return 0
	}
	// 选择较小的向量迭代
	if len(vec1) > len(vec2) {
		vec1, vec2 = vec2, vec1
	}
	var dot, norm1, norm2 float64
	for k, v1 := range vec1 {
		norm1 += v1 * v1
		if v2, ok := vec2[k]; ok {
			dot += v1 * v2
		}
	}
	for _, v2 := range vec2 {
		norm2 += v2 * v2
	}
	if norm1 == 0 || norm2 == 0 {
		return 0
	}
	return dot / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// CosineSimilarity 计算两段文本的余弦相似度（0-1）。
//
// 使用 3-gram 字符级向量化，适合检测局部修改/洗稿。
func CosineSimilarity(text1, text2 string) float64 {
	grams1 := charNGrams(strings.ToLower(text1), 3)
	grams2 := charNGrams(strings.ToLower(text2), 3)
	if len(grams1) == 0 || len(grams2) == 0 {
		return 0
	}
	return cosineSim(termFreq(grams1), termFreq(grams2))
}

// ============================================================
// 4. 综合相似度
// ============================================================

// SimilarityResult 相似度检测结果。
type SimilarityResult struct {
	MinHash      float64 `json:"minhash"`        // MinHash Jaccard 估计（0-1）
	Cosine       float64 `json:"cosine"`         // 余弦相似度（0-1）
	Combined     float64 `json:"combined"`       // 综合相似度（0-1），取两者加权平均
	IsDuplicate  bool    `json:"is_duplicate"`   // 是否判定为重复
	MaxSimilarID string  `json:"max_similar_id"` // 最相似的内容 ID（空表示无匹配）
}

// CombinedSimilarity 计算综合相似度（MinHash 40% + Cosine 60%）。
//
// MinHash 偏重词级集合重叠，Cosine 偏重字符序列相似性，
// 加权综合能同时捕捉词汇重复与洗稿改写。
func CombinedSimilarity(text1, text2 string) float64 {
	mh := MinHashSimilarity(text1, text2)
	cs := CosineSimilarity(text1, text2)
	return mh*0.4 + cs*0.6
}

// ============================================================
// 5. 检测器（维护语料库，提供查重能力）
// ============================================================

// Entry 语料库中的一条内容。
type Entry struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Brand    string `json:"brand,omitempty"`
	Platform string `json:"platform,omitempty"`
}

// Detector 内容独特性检测器。
//
// 维护已生成内容的语料库，对新内容进行查重。
// 使用 MinHash 签名 + 余弦向量的双索引，兼顾速度与精度。
type Detector struct {
	mu                 sync.RWMutex
	entries            map[string]*corpusEntry
	duplicateThreshold float64
}

type corpusEntry struct {
	Entry
	minHashSig []uint64
	cosineVec  map[string]float64
}

// NewDetector 创建检测器。
//
// duplicateThreshold 为判定重复的相似度阈值（0-1），默认 0.7。
func NewDetector(duplicateThreshold float64) *Detector {
	if duplicateThreshold <= 0 {
		duplicateThreshold = 0.7
	}
	return &Detector{
		entries:            make(map[string]*corpusEntry),
		duplicateThreshold: duplicateThreshold,
	}
}

// Add 向语料库添加内容。
func (d *Detector) Add(entry Entry) error {
	if strings.TrimSpace(entry.ID) == "" {
		return fmt.Errorf("内容 ID 不能为空")
	}
	if strings.TrimSpace(entry.Content) == "" {
		return fmt.Errorf("内容不能为空")
	}
	tokens := tokenize(entry.Content)
	mh := NewMinHash(defaultNumHash)
	mh.PushAll(tokens)
	grams := charNGrams(strings.ToLower(entry.Content), 3)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[entry.ID] = &corpusEntry{
		Entry:      entry,
		minHashSig: mh.Signature(),
		cosineVec:  termFreq(grams),
	}
	return nil
}

// Remove 从语料库移除内容。
func (d *Detector) Remove(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.entries[id]; !ok {
		return false
	}
	delete(d.entries, id)
	return true
}

// Size 返回语料库内容数量。
func (d *Detector) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.entries)
}

// Check 检测新内容与语料库中已有内容的最大相似度。
//
// 返回综合相似度最高的匹配结果。若语料库为空，返回零值结果。
func (d *Detector) Check(content string) SimilarityResult {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.entries) == 0 || strings.TrimSpace(content) == "" {
		return SimilarityResult{}
	}

	tokens := tokenize(content)
	mh := NewMinHash(defaultNumHash)
	mh.PushAll(tokens)
	newSig := mh.Signature()
	grams := charNGrams(strings.ToLower(content), 3)
	newVec := termFreq(grams)

	var best SimilarityResult
	best.MaxSimilarID = ""

	for id, e := range d.entries {
		mhSim := Jaccard(newSig, e.minHashSig)
		csSim := cosineSim(newVec, e.cosineVec)
		combined := mhSim*0.4 + csSim*0.6
		if combined > best.Combined {
			best = SimilarityResult{
				MinHash:      mhSim,
				Cosine:       csSim,
				Combined:     combined,
				MaxSimilarID: id,
			}
		}
	}

	best.IsDuplicate = best.Combined >= d.duplicateThreshold
	return best
}

// CheckAndAdd 检测新内容独特性，若通过则自动加入语料库。
//
// 返回检测结果与是否已添加。若判定为重复则不添加。
func (d *Detector) CheckAndAdd(entry Entry) (SimilarityResult, error) {
	result := d.Check(entry.Content)
	if result.IsDuplicate {
		return result, nil
	}
	if err := d.Add(entry); err != nil {
		return result, err
	}
	return result, nil
}

// SetThreshold 设置重复判定阈值。
func (d *Detector) SetThreshold(t float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t > 0 && t <= 1 {
		d.duplicateThreshold = t
	}
}

// Threshold 返回当前重复判定阈值。
func (d *Detector) Threshold() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.duplicateThreshold
}

// FindAllDuplicates 找出语料库内部所有互为重复的内容对。
//
// 返回 (id1, id2, similarity) 列表，按相似度降序排列。
// 适用于对历史语料做一次性去重扫描。
func (d *Detector) FindAllDuplicates() []DuplicatePair {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var pairs []DuplicatePair
	ids := make([]string, 0, len(d.entries))
	for id := range d.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			e1 := d.entries[ids[i]]
			e2 := d.entries[ids[j]]
			mhSim := Jaccard(e1.minHashSig, e2.minHashSig)
			csSim := cosineSim(e1.cosineVec, e2.cosineVec)
			combined := mhSim*0.4 + csSim*0.6
			if combined >= d.duplicateThreshold {
				pairs = append(pairs, DuplicatePair{
					ID1:        ids[i],
					ID2:        ids[j],
					Similarity: combined,
				})
			}
		}
	}
	slices.SortFunc(pairs, func(a, b DuplicatePair) int { return cmp.Compare(b.Similarity, a.Similarity) })
	return pairs
}

// DuplicatePair 重复内容对。
type DuplicatePair struct {
	ID1        string  `json:"id1"`
	ID2        string  `json:"id2"`
	Similarity float64 `json:"similarity"`
}
