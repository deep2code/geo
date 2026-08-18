// Package knowledge 提供离线品牌/公司知识库，用于智能补全前的快速匹配。
//
// 数据来源：SinoFacts Open Dataset（CC BY 4.0），
// 383 家中国出海软件公司，每字段均带证据 URL 与置信度。
// 设计原则：
//  1. 数据集通过 go:embed 嵌入二进制，部署零依赖
//  2. 启动时一次加载并构建倒排索引
//  3. 支持品牌名 / 公司名 / 域名 多种查询入口
//  4. 命中结果可直接转为 BrandProfile + Company，省去 LLM 分析可能的幻觉
package knowledge

import (
	"bufio"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
)

//go:embed data/sinofacts.jsonl
var dataFS embed.FS

// Record SinoFacts 原始记录（只解析我们关心的字段）。
type Record struct {
	Source     string  `json:"source"`
	Canonical  string  `json:"canonical"`
	CiteAs     string  `json:"cite_as"`
	Slug       string  `json:"slug"`
	Domain     string  `json:"domain"`
	Confidence float64 `json:"-"`
	Profile    struct {
		NameEn        string   `json:"name_en"`
		NameZh        string   `json:"name_zh"`
		ProductNames  []string `json:"product_names"`
		FoundedYear   *int     `json:"founded_year"`
		HqCity        *string  `json:"hq_city"`
		Category      string   `json:"category"`
		Subcategory   string   `json:"subcategory"`
		DescriptionEn string   `json:"description_en"`
		DescriptionZh string   `json:"description_zh"`
		ChinaOrigin   struct {
			IsChinaOrigin bool     `json:"is_china_origin"`
			OriginCity    *string  `json:"origin_city"`
			Founders      []string `json:"founders_public"`
		} `json:"china_origin"`
		TargetMarkets []string `json:"target_markets"`
	} `json:"profile"`
	Provenance struct {
		Confidence float64 `json:"confidence"`
		Model      string  `json:"model"`
	} `json:"provenance"`
}

// Entry 知识库索引条目（预处理完的品牌+公司画像）。
type Entry struct {
	// 原始引用（展示给用户，标注 CC BY 4.0）
	CiteAs    string
	Canonical string
	// 品牌层
	BrandName     string
	BrandAliases  []string
	BrandDomain   string
	Products      []string
	Industry      string
	Category      string
	DescriptionZh string
	// 公司层
	CompanyName        string
	CompanyAliases     []string
	CompanyDomain      string
	CompanyIndustry    string
	CompanyDescription string
	Headquarters       string
	FoundedYear        int
}

// Knowledge 品牌公司知识库。
type Knowledge struct {
	entries []Entry
	// 倒排：小写 token → 条目索引
	index map[string][]int
	// 域名 → 条目索引（精确匹配）
	byDomain map[string]int
	// P2-8：normalized 全名 → 条目索引（精确匹配 O(1)，避免 Search 全量遍历）
	nameExact map[string]int
	// 条目总数
	N int
	// 向量存储（TF-IDF / Embedding），用于语义检索
	vectorStore *HybridVectorStore
}

// globalKBPtr 无锁单例：atomic.Pointer 保证并发 Load 只构建一次且无数据竞争。
// 与 sync.Once 相比，首次构建失败时指针保持 nil，下次 Load 可重试（Once 会缓存错误）。
var globalKBPtr atomic.Pointer[Knowledge]

// Load 加载并构建索引（幂等，多次调用返回同一实例；并发调用安全）。
func Load() (*Knowledge, error) {
	if kb := globalKBPtr.Load(); kb != nil {
		return kb, nil
	}
	f, err := dataFS.Open("data/sinofacts.jsonl")
	if err != nil {
		return nil, fmt.Errorf("打开内嵌数据集失败: %w", err)
	}
	defer f.Close()

	var records []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("第 %d 行解析失败: %w", line, err)
		}
		if r.Provenance.Confidence > 0 {
			r.Confidence = r.Provenance.Confidence
		}
		records = append(records, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取数据集失败: %w", err)
	}

	kb := &Knowledge{
		entries:   make([]Entry, 0, len(records)),
		index:     map[string][]int{},
		byDomain:  map[string]int{},
		nameExact: map[string]int{},
	}
	for i, r := range records {
		e := buildEntry(r)
		kb.entries = append(kb.entries, e)
		kb.addIndex(i, e)
	}
	kb.N = len(kb.entries)
	// 构建向量存储（HybridVectorStore）：优先 Embedding API，回退本地 TF-IDF
	// 每个 entry 的 text 拼接 BrandName + BrandDomain + Products + Industry + Description
	kb.vectorStore = NewHybridVectorStore()
	for i, e := range kb.entries {
		text := e.BrandName + " " + e.BrandDomain + " " +
			strings.Join(e.Products, " ") + " " + e.Industry + " " + e.DescriptionZh
		id := e.Canonical
		if id == "" {
			id = strconv.Itoa(i)
		}
		_ = kb.vectorStore.Add(id, text, map[string]string{
			"brand":    e.BrandName,
			"domain":   e.BrandDomain,
			"industry": e.Industry,
		})
	}
	// 并发构建时仅第一个成功者胜出，其余复用已发布实例
	if !globalKBPtr.CompareAndSwap(nil, kb) {
		return globalKBPtr.Load(), nil
	}
	return kb, nil
}

func buildEntry(r Record) Entry {
	brandName := r.Profile.NameZh
	if brandName == "" {
		brandName = r.Profile.NameEn
	}
	aliases := []string{}
	if r.Profile.NameZh != "" && r.Profile.NameEn != "" && r.Profile.NameZh != r.Profile.NameEn {
		aliases = append(aliases, r.Profile.NameEn)
	}
	// 行业映射：SinoFacts 的 category/subcategory 对应我们的 Industry/Category
	industry := mapCategoryToIndustry(r.Profile.Category)
	category := r.Profile.Subcategory
	if category == "" {
		category = r.Profile.Category
	}
	desc := r.Profile.DescriptionZh
	if desc == "" {
		desc = r.Profile.DescriptionEn
	}
	var hq string
	if r.Profile.HqCity != nil {
		hq = *r.Profile.HqCity
	} else if r.Profile.ChinaOrigin.OriginCity != nil {
		hq = *r.Profile.ChinaOrigin.OriginCity
	}
	var fy int
	if r.Profile.FoundedYear != nil {
		fy = *r.Profile.FoundedYear
	}
	// 公司名：沿用品牌名（SinoFacts 多数条目未区分品牌名/公司名，
	// 我们约定 company.name=品牌所属公司，当区分不了时用品牌名）
	companyName := brandName
	if industry != "" {
		// 粗略地将品牌行业也作为公司行业
	}
	return Entry{
		CiteAs:    r.CiteAs,
		Canonical: r.Canonical,
		// 品牌
		BrandName:     brandName,
		BrandAliases:  aliases,
		BrandDomain:   r.Domain,
		Products:      r.Profile.ProductNames,
		Industry:      industry,
		Category:      category,
		DescriptionZh: desc,
		// 公司
		CompanyName:        companyName,
		CompanyAliases:     []string{r.Profile.NameEn},
		CompanyDomain:      r.Domain,
		CompanyIndustry:    industry,
		CompanyDescription: desc,
		Headquarters:       hq,
		FoundedYear:        fy,
	}
}

// mapCategoryToIndustry 简单映射 SinoFacts category → 行业（上层 Industry）。
func mapCategoryToIndustry(c string) string {
	switch c {
	case "ai-llm", "ai-infra", "ai-data", "generative-ai", "multimodal-ai":
		return "人工智能 / AI"
	case "developer-tools", "devops", "lowcode", "database", "cloud-infra":
		return "企业软件 / 开发者工具"
	case "saas-vertical", "business-software", "crm", "hr-tech", "martech":
		return "企业级 SaaS"
	case "cybersecurity":
		return "网络安全 / 信息安全"
	case "fintech", "insurtech", "payments":
		return "金融科技 / 支付"
	case "e-commerce", "retail-tech", "logistics-tech":
		return "电子商务 / 零售科技"
	case "edtech":
		return "在线教育 / 教育科技"
	case "healthtech", "medtech":
		return "医疗科技"
	case "semiconductor", "chip-design", "quantum":
		return "半导体 / 芯片"
	case "robotics", "iot", "hardware":
		return "机器人 / IoT / 硬件"
	case "gaming", "media-tech", "content-tech":
		return "游戏 / 内容科技"
	case "mobility", "autonomous":
		return "智能驾驶 / 出行"
	default:
		if c == "" {
			return ""
		}
		return c
	}
}

// addIndex 为条目构建倒排：品牌名/公司名/别名/产品名 分词 → 条目 id。
func (kb *Knowledge) addIndex(i int, e Entry) {
	if e.BrandDomain != "" {
		kb.byDomain[strings.ToLower(e.BrandDomain)] = i
	}
	// P2-8：全名精确匹配索引（normalized），Search 时 O(1) 命中。
	if n := normalize(e.BrandName); n != "" {
		if _, exists := kb.nameExact[n]; !exists {
			kb.nameExact[n] = i
		}
	}
	if n := normalize(e.CompanyName); n != "" {
		if _, exists := kb.nameExact[n]; !exists {
			kb.nameExact[n] = i
		}
	}
	add := func(tok string) {
		tok = normalize(tok)
		if tok == "" {
			return
		}
		kb.index[tok] = append(kb.index[tok], i)
		// 前缀也加入（支持 prefix 搜索），但限制长度避免爆炸
		if len(tok) >= 3 {
			for pl := 3; pl <= len(tok); pl++ {
				p := tok[:pl]
				if !containsId(kb.index[p], i) {
					kb.index[p] = append(kb.index[p], i)
				}
			}
		}
	}
	// 品牌/公司名所有字段都索引
	add(e.BrandName)
	add(e.CompanyName)
	for _, a := range e.BrandAliases {
		add(a)
	}
	for _, a := range e.CompanyAliases {
		add(a)
	}
	for _, p := range e.Products {
		add(p)
	}
	// 域名去掉点后也索引
	if e.BrandDomain != "" {
		add(strings.ReplaceAll(e.BrandDomain, ".", " "))
	}
}

func containsId(arr []int, i int) bool {
	for _, v := range arr {
		if v == i {
			return true
		}
	}
	return false
}

// normalize 将字符串标准化：小写 + 去标点 + 空格去重。
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == ',' || r == '/' || r == '&' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// SearchResult 搜索结果。
type SearchResult struct {
	Entry  Entry
	Score  float64 // 0-100 匹配度
	Source string  // 来源信息（含 License 标注）
}

// Search 按品牌/公司名搜索知识库，返回 Top N（默认 5）匹配度最高的结果。
func (kb *Knowledge) Search(query string, topN int) []SearchResult {
	if topN <= 0 {
		topN = 5
	}
	q := normalize(query)
	if q == "" {
		return nil
	}
	// 投票：匹配 token 越多分越高；命中全名得分最高
	votes := map[int]float64{}
	// P2-8：精确全名匹配 O(1)（替代原先的全量遍历精确匹配分支）。
	// 命中 normalized 全名直接给满分档（60 精确 + 40 完全相等）。
	if id, ok := kb.nameExact[q]; ok {
		votes[id] += 100
	}
	// 全名包含匹配：仍需遍历（子串索引空间大，383 条规模 O(N) 可接受；
	// 品牌数增长到数千级时可改为 n-gram 子串索引）。
	for i, e := range kb.entries {
		if votes[i] >= 100 {
			continue // 精确命中已满分，跳过子串检查
		}
		if strings.Contains(normalize(e.BrandName), q) ||
			strings.Contains(normalize(e.CompanyName), q) {
			votes[i] += 60
		}
		if normalize(e.BrandName) == q || normalize(e.CompanyName) == q {
			votes[i] += 40
		}
		if strings.Contains(strings.ToLower(e.BrandDomain), strings.ToLower(query)) {
			votes[i] += 30
		}
	}
	// 倒排 token 搜索，命中越多分越高
	for _, tok := range strings.Fields(q) {
		if ids, ok := kb.index[tok]; ok {
			for _, i := range ids {
				votes[i] += 15
			}
		}
	}
	if len(votes) == 0 {
		return nil
	}
	type kv struct {
		i     int
		score float64
	}
	var arr []kv
	for i, s := range votes {
		arr = append(arr, kv{i, s})
	}
	slices.SortFunc(arr, func(a, b kv) int { return cmp.Compare(b.score, a.score) })
	out := make([]SearchResult, 0, min(topN, len(arr)))
	for k := 0; k < len(arr) && k < topN; k++ {
		e := kb.entries[arr[k].i]
		score := arr[k].score
		if score > 100 {
			score = 100
		}
		src := e.CiteAs
		if src == "" {
			src = "SinoFacts (sinofacts.com), CC BY 4.0"
		}
		out = append(out, SearchResult{Entry: e, Score: score, Source: src})
	}
	return out
}

// LookupByDomain 按域名精确查找（用于从 autocomplete 域名直接锁定公司）。
func (kb *Knowledge) LookupByDomain(domain string) *Entry {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "https://"))
	d = strings.TrimPrefix(d, "http://")
	d = strings.Split(d, "/")[0]
	d = strings.TrimPrefix(d, "www.")
	if i, ok := kb.byDomain[d]; ok {
		return &kb.entries[i]
	}
	return nil
}

// VectorSearch 向量语义搜索（基于 TF-IDF 或 Embedding API）。
// 与 Search 的关键词倒排匹配不同，VectorSearch 基于词频-逆文档频率向量化的
// 余弦相似度检索，能命中"语义相近但字面不同"的品牌条目。
// topN <= 0 时默认返回 5 条。
func (kb *Knowledge) VectorSearch(query string, topN int) []VectorSearchResult {
	if kb.vectorStore == nil || kb.vectorStore.Size() == 0 {
		return nil
	}
	if topN <= 0 {
		topN = 5
	}
	return kb.vectorStore.Search(query, topN)
}
