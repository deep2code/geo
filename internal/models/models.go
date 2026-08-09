// Package models 定义 GEO 系统的核心数据模型。
//
// 这些数据结构综合参考了普林斯顿大学 GEO 论文 (KDD 2024) 的可见度指标、
// AutoGEO (ICLR 2026) 的 GEO/GEU 双评分体系，以及 geo-optimizer-skill
// 的 47 种研究背书检测方法。
package models

import "time"

// EngineType 生成式引擎类型。
type EngineType string

const (
	EngineChatGPT    EngineType = "chatgpt"
	EnginePerplexity EngineType = "perplexity"
	EngineGemini     EngineType = "gemini"
	EngineClaude     EngineType = "claude"
)

// 国内主流大模型引擎类型。
const (
	EngineQwen     EngineType = "qwen"     // 通义千问（阿里云）
	EngineGLM      EngineType = "glm"      // 智谱 GLM（智谱AI）
	EngineDeepSeek EngineType = "deepseek" // DeepSeek
	EngineKimi     EngineType = "kimi"     // Kimi（月之暗面）
	EngineWenxin   EngineType = "wenxin"   // 文心一言（百度）
	EngineDoubao   EngineType = "doubao"   // 豆包（字节跳动火山引擎）
	EngineXiaomi   EngineType = "xiaomi"   // 小米大模型（MiLM）
	EngineXunfei   EngineType = "xunfei"   // 讯飞星火（科大讯飞）
	EngineYuanbao  EngineType = "yuanbao"  // 元宝/混元（腾讯）
)

// DomainType 内容领域类型，决定最优策略选择（来自 Princeton 论文洞察）。
//
// 严肃话题靠引用、软性话题靠语气、知识话题靠数据。
type DomainType string

const (
	DomainSerious   DomainType = "serious"   // 法律/医疗/政府
	DomainSoft      DomainType = "soft"      // 时尚/娱乐/生活
	DomainKnowledge DomainType = "knowledge" // 历史/科技/事实
)

// StrategyType GEO 优化策略类型，对应 Princeton 论文的 9 种策略。
type StrategyType string

const (
	StrategyCiteSources    StrategyType = "cite_sources"    // 引用来源 (+27%)
	StrategyStatistics     StrategyType = "statistics"      // 统计数据 (+33%)
	StrategyAuthoritative  StrategyType = "authoritative"   // 权威语气
	StrategyQuotation      StrategyType = "quotation"       // 引用语 (+41%)
	StrategyFluency        StrategyType = "fluency"         // 流畅度 (+29%)
	StrategyEasyUnderstand StrategyType = "easy_understand" // 易于理解
	StrategyKeyword        StrategyType = "keyword"         // 关键词
	StrategyUniqueWords    StrategyType = "unique_words"    // 独特词汇
	StrategyTechnicalTerms StrategyType = "technical_terms" // 技术术语
	// 扩展工程化策略
	StrategyStructure   StrategyType = "structure"   // 结构化
	StrategyFAQ         StrategyType = "faq"         // 问答生成
	StrategySchema      StrategyType = "schema"      // JSON-LD
	StrategyAnswerFirst StrategyType = "answer_first" // 结论前置
)

// OptimizationRequest 优化请求。
type OptimizationRequest struct {
	Content        string        `json:"content"`
	URL            string        `json:"url,omitempty"`
	Title          string        `json:"title,omitempty"`
	TargetEngines  []EngineType  `json:"target_engines,omitempty"`
	DomainType     DomainType    `json:"domain_type,omitempty"`
	Strategies     []StrategyType `json:"strategies,omitempty"`
	Enterprise     *Enterprise   `json:"enterprise,omitempty"`
	Language       string        `json:"language,omitempty"` // 默认 zh
}

// Enterprise 企业信息，用于品牌实体一致性增强。
type Enterprise struct {
	CompanyName string `json:"company_name"`
	ProductName string `json:"product_name,omitempty"`
	Description string `json:"description,omitempty"`
}

// OptimizationResponse 优化响应。
type OptimizationResponse struct {
	OptimizedContent string             `json:"optimized_content"`
	ScoreBefore      float64            `json:"score_before"`
	ScoreAfter       float64            `json:"score_after"`
	GeoScore         VisibilityMetrics  `json:"geo_score"`
	UtilityScore     UtilityMetrics     `json:"utility_score"`
	AppliedStrategies []StrategyResult  `json:"applied_strategies"`
	Recommendations  []Recommendation   `json:"recommendations,omitempty"`
	GeneratedAssets  *GeneratedAssets   `json:"generated_assets,omitempty"`
}

// StrategyResult 单个策略执行结果。
type StrategyResult struct {
	Strategy  StrategyType `json:"strategy"`
	Applied   bool         `json:"applied"`
	Improvement float64    `json:"improvement"` // 评分提升
	PWCBoost  float64      `json:"pwc_boost"`   // 理论 PWC 增益百分比
	Detail    string       `json:"detail,omitempty"`
}

// StrategyInfo 策略元信息（供 API 列表展示）。
type StrategyInfo struct {
	Type        StrategyType `json:"type"`
	Name        string       `json:"name"`
	Effectiveness float64    `json:"effectiveness"`
	PWCBoost    float64      `json:"pwc_boost"` // 理论 PWC 增益百分比
}

// Recommendation 优化建议。
type Recommendation struct {
	Category string `json:"category"`
	Priority string `json:"priority"` // high/medium/low
	Message  string `json:"message"`
}

// GeneratedAssets 生成的结构化资产。
type GeneratedAssets struct {
	JSONLD     string `json:"json_ld,omitempty"`
	LLMsTxt    string `json:"llms_txt,omitempty"`
	LLMsFullTxt string `json:"llms_full_txt,omitempty"`
}

// VisibilityMetrics 可见度指标（来自 Princeton 论文）。
//
// V(d) = PositionScore × Relevance 的聚合。
type VisibilityMetrics struct {
	CitationFrequency       int     `json:"citation_frequency"`         // 引用次数
	CitationOrder           int     `json:"citation_order"`             // 引用排名(1=最佳)
	PositionScore           float64 `json:"position_score"`            // 位置得分
	TokenCount              int     `json:"token_count"`               // 被引用token数
	SemanticSimilarity      float64 `json:"semantic_similarity"`       // 语义相似度
	RelativeCitationScore   float64 `json:"relative_citation_score"`   // 相对引用得分
	OverallScore            float64 `json:"overall_score"`             // 综合GEO得分
}

// UtilityMetrics 效用指标（来自 AutoGEO），保证优化不破坏 AI 回答质量。
type UtilityMetrics struct {
	CitationQuality   float64 `json:"citation_quality"`
	KeypointCoverage  float64 `json:"keypoint_coverage"`
	ResponseQuality   float64 `json:"response_quality"`
	OverallScore      float64 `json:"overall_score"`
}

// ContentAnalysis 内容分析结果，包含各类 GEO 信号检测。
type ContentAnalysis struct {
	URL           string         `json:"url,omitempty"`
	RawText       string         `json:"raw_text"`
	WordCount     int            `json:"word_count"`
	CitabilitySignals  map[string]bool `json:"citability_signals"`
	StructureSignals   map[string]bool `json:"structure_signals"`
	NegativeSignals    []string        `json:"negative_signals,omitempty"`
	EvergreenScore     int             `json:"evergreen_score"`
	AnalyzedAt         time.Time       `json:"analyzed_at"`
}

// Citation AI 引擎回答中的引用。
type Citation struct {
	URL       string  `json:"url"`
	Title     string  `json:"title,omitempty"`
	Snippet   string  `json:"snippet,omitempty"`
	Position  int     `json:"position"` // 在参考资料中的位置
}

// EngineResponse 生成式引擎的回答。
type EngineResponse struct {
	Engine    EngineType `json:"engine"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations,omitempty"`
}

// AIEnginePreset AI 引擎预设偏好。
type AIEnginePreset struct {
	Engine           EngineType `json:"engine"`
	PreferredStrategies []StrategyType `json:"preferred_strategies"`
	MaxTokens         int       `json:"max_tokens"`
	Temperature       float64   `json:"temperature"`
	Weights           map[string]float64 `json:"weights"` // 各信号权重
}

// ScoreBreakdown 评分明细。
type ScoreBreakdown struct {
	Category string  `json:"category"`
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Detail   string  `json:"detail,omitempty"`
}
