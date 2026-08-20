// Package brand 实现企业/品牌/产品在 AI 搜索引擎中的可见度评分与报告系统。
//
// 区别于 pkg/geo 面向"单篇内容"的优化，brand 包面向"品牌实体"：
//   - 输入：品牌信息 + 竞争对手 + 业务相关查询词（高意图 prompt）
//   - 处理：查询多个 AI 引擎，检测品牌提及/引用/情感/位置/竞品/幽灵引用
//   - 输出：品牌可见度评分（BVS）+ 运营行动报告（指导运营人员工作方向）
//
// 理论基础：
//   - Ranqo 论文 (arXiv:2606.20065) 的 5 维可见度指标体系
//   - Victorious 平台优先框架（各引擎引用率差异巨大，不可合并为单一数值）
//   - Amicited AI Visibility Index 复合评分模型
//   - 开源参考：AiCMO / oneglanse / xanlens / ai-brand-monitor-mcp
package brand

import (
	"time"

	"my-geo/internal/brand/attribution"
	"my-geo/internal/brand/llmanalysis"
	"my-geo/internal/brand/persona"
	"my-geo/internal/models"
)

// Company 关联公司信息，用于 AI 引擎回答中的实体识别与关联匹配。
//
// 品牌通常隶属于某个公司（母公司/集团），AI 回答时可能以公司名替代品牌名，
// 或同时提及公司与产品。提供公司信息有助于：
//  1. 扩大提及检测的匹配范围（公司名 + 品牌名 + 产品名）
//  2. 提升实体识别的准确度，减少幽灵引用
//  3. 区分同品牌名但不同公司的歧义场景
type Company struct {
	// 公司全称（必须），如 "Salesforce, Inc."。
	Name string `json:"name"`
	// 公司简称/别名，用于匹配。
	Aliases []string `json:"aliases,omitempty"`
	// 公司官网域名（不含协议），如 salesforce.com。
	Domain string `json:"domain,omitempty"`
	// 公司简介，用于品牌实体一致性增强。
	Description string `json:"description,omitempty"`
	// 所属行业/领域（与品牌 category 可能不同，例如公司集团横跨多行业）。
	Industry string `json:"industry,omitempty"`
	// 公司总部所在地（可选）。
	Headquarters string `json:"headquarters,omitempty"`
	// 成立年份（可选）。
	FoundedYear int `json:"founded_year,omitempty"`

	// ---------- 工商数据专属字段（来自 GSXT/SAMR，经由 China-Check MCP 查询） ----------

	// 统一社会信用代码（18 位，官方唯一标识）。
	CreditCode string `json:"credit_code,omitempty"`
	// 注册资本（含币种与金额，如 "CNY 10,000 万"）。
	RegisteredCapital string `json:"registered_capital,omitempty"`
	// 实收资本（可选）。
	PaidInCapital string `json:"paid_in_capital,omitempty"`
	// 法定代表人姓名。
	LegalRepresentative string `json:"legal_representative,omitempty"`
	// 登记状态：在营 / 存续 / 吊销 / 注销 / 迁出 / 停业 / 清算 / 其他。
	RegistrationStatus string `json:"registration_status,omitempty"`
	// 成立日期（ISO YYYY-MM-DD，如 "1998-11-11"）。
	EstablishedDate string `json:"established_date,omitempty"`
	// 企业类型：有限责任公司 / 股份有限公司 / 港澳台投资 / 外商投资等。
	CompanyType string `json:"company_type,omitempty"`
	// 注册地址（完整地址）。
	RegisteredAddress string `json:"registered_address,omitempty"`
	// 经营范围（原始工商登记文本，可能很长）。
	BusinessScope string `json:"business_scope,omitempty"`
	// 所属省份/直辖市。
	Province string `json:"province,omitempty"`
	// 人员规模（可选，如 "500-999人"）。
	StaffSize string `json:"staff_size,omitempty"`
}

// BrandProfile 品牌画像，作为可见度评估的输入。
type BrandProfile struct {
	// WorkspaceID 多租户隔离键（工作区 ID）。
	// 由账号体系中间件从 JWT 注入；未启用账号体系时为空（全局共享）。
	WorkspaceID string `json:"workspace_id,omitempty"`
	// 品牌名称（必须），用于 AI 回答中的提及检测。
	Name string `json:"name"`
	// 品牌别名/简称，任一匹配即视为被提及。
	Aliases []string `json:"aliases,omitempty"`
	// 品牌官网域名（不含协议），用于引用检测，如 example.com。
	Domain string `json:"domain,omitempty"`
	// 产品名称列表，用于产品级可见度检测。
	Products []string `json:"products,omitempty"`
	// 关联公司信息（母公司/集团），用于扩大匹配范围与实体识别。
	Company *Company `json:"company,omitempty"`
	// 竞争对手列表，用于声量份额（SOV）计算。
	Competitors []Competitor `json:"competitors,omitempty"`
	// 业务相关查询词（高意图 prompt），如 "最好的 CRM 工具"、"AI 写作软件推荐"。
	// 这些是潜在客户可能向 AI 提问的问题，品牌希望在此类问题中被提及。
	Prompts []string `json:"prompts"`
	// 目标 AI 引擎列表，为空时使用全部已配置引擎。
	TargetEngines []models.EngineType `json:"target_engines,omitempty"`
	// 采样次数：每个查询词×引擎重复查询 N 次，多数票判定（提升结果稳定性）。
	// 0 或 1 = 单次查询（默认）；建议 3。覆盖引擎级 GEO_AUDIT_SAMPLES 配置。
	Samples int `json:"samples,omitempty"`
	// 品牌所属行业（大品类），如 企业软件 / 金融科技 / 电子商务 / 在线教育。
	// 比 Category 更上层，用于跨品类对标与报告分组。
	Industry string `json:"industry,omitempty"`
	// 品类/细分品类，用于报告分类，如 CRM / 项目管理 / 在线支付。
	Category string `json:"category,omitempty"`

	// ---------- 多语言/多市场审计（#8） ----------
	// 市场代码（cn/us/jp/kr/de/fr/global），用于按市场过滤主流 AI 引擎。
	// 为空时按 TargetEngines 处理，不按市场过滤。
	Market string `json:"market,omitempty"`
	// 查询语言代码（zh/en/ja/ko/de/fr）。非中文时 Audit 会自动将 Prompts
	// 本地化（翻译/改写）为目标语言后再查询 AI 引擎。
	Language string `json:"language,omitempty"`
}

// Competitor 竞争对手。
type Competitor struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Domain  string   `json:"domain,omitempty"`
	// 关联公司信息（可选），增强竞品匹配。
	Company *Company `json:"company,omitempty"`
}

// PromptResult 单个查询词在单个引擎上的检测结果。
type PromptResult struct {
	Prompt    string            `json:"prompt"`
	Engine    models.EngineType `json:"engine"`
	Answer    string            `json:"answer"`
	Citations []models.Citation `json:"citations,omitempty"`
	// 品牌是否在回答文本中被提及。
	BrandMentioned bool `json:"brand_mentioned"`
	// 品牌首次提及的位置（按段落计，1 表示第一段，0 表示未提及）。
	BrandPosition int `json:"brand_position"`
	// 品牌官网是否被引用。
	BrandCited bool `json:"brand_cited"`
	// 幽灵引用：官网被引用但品牌名未在文本中出现。
	GhostCitation bool `json:"ghost_citation"`
	// 情感倾向：positive / neutral / negative。
	Sentiment string `json:"sentiment"`
	// 情感判定置信度 0-1（LLM 判定时填充，降级词典法时给固定值）。
	SentimentConfidence float64 `json:"sentiment_confidence,omitempty"`
	// 该结果是否经过 LLM 判定（true=LLM，false=词典法降级）。
	LLMJudged bool `json:"llm_judged,omitempty"`
	// LLM 识别出的回答"采信来源"实体（P1-a：源情报深化），降级时为正则提取的 URL。
	ExtractedSources []llmanalysis.SourceClaim `json:"extracted_sources,omitempty"`
	// 回答中提及的竞争对手列表。
	CompetitorMentions []CompetitorMention `json:"competitor_mentions,omitempty"`
	// 查询耗时。
	Duration time.Duration `json:"duration,omitempty"`
	// 错误信息（查询失败时）。
	Error string `json:"error,omitempty"`
	// ---------- 多次采样（Samples=N，多数票判定）----------
	// 采样次数（1=单次查询）。
	Samples int `json:"samples,omitempty"`
	// 品牌提及票数（Samples 次查询中被判定提及的次数）。
	MentionVotes int `json:"mention_votes,omitempty"`
	// 品牌引用票数。
	CitedVotes int `json:"cited_votes,omitempty"`
	// 一致性：MentionVotes/Samples（0-1），1=多次采样判定完全一致。
	// 一致性低（如 0.5）说明该查询结果不稳定，分数置信度低。
	Consistency float64 `json:"consistency,omitempty"`
}

// CompetitorMention 竞争对手提及。
type CompetitorMention struct {
	Name     string `json:"name"`
	Position int    `json:"position"` // 首次提及段落位置
	Cited    bool   `json:"cited"`    // 是否被引用
}

// EngineStats 单个引擎的聚合统计。
type EngineStats struct {
	Engine             models.EngineType `json:"engine"`
	TotalPrompts       int               `json:"total_prompts"`
	MentionCount       int               `json:"mention_count"`
	CitationCount      int               `json:"citation_count"`
	GhostCitationCount int               `json:"ghost_citation_count"`
	// 提及率 = MentionCount / TotalPrompts × 100。
	MentionRate float64 `json:"mention_rate"`
	// 引用率 = CitationCount / TotalPrompts × 100。
	CitationRate float64 `json:"citation_rate"`
	// 平均提及位置（仅统计被提及的 prompt）。
	AvgPosition float64 `json:"avg_position"`
	// 情感分布。
	SentimentPositive int     `json:"sentiment_positive"`
	SentimentNeutral  int     `json:"sentiment_neutral"`
	SentimentNegative int     `json:"sentiment_negative"`
	PositiveRate      float64 `json:"positive_rate"`
	// 该引擎上品牌声量份额。
	ShareOfVoice float64 `json:"share_of_voice"`
	// 配置状态：是否已配置 API Key。
	Configured bool `json:"configured"`

	// 内部累加字段（不序列化）。
	brandPositions     []int
	competitorMentions map[string]int
}

// CompetitorNames 返回该引擎上被提及的竞品名列表（供 scheduler 等外部包使用）。
func (s EngineStats) CompetitorNames() []string {
	if len(s.competitorMentions) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.competitorMentions))
	for name := range s.competitorMentions {
		names = append(names, name)
	}
	return names
}

// ContentGap 内容缺口：竞品被提及而品牌未被提及的高机会 prompt。
type ContentGap struct {
	Prompt          string            `json:"prompt"`
	Engine          models.EngineType `json:"engine"`
	CompetitorNamed []string          `json:"competitor_named"`
	// 建议创建的内容主题。
	SuggestedTopic string `json:"suggested_topic"`
}

// VisibilityReport 品牌可见度报告。
type VisibilityReport struct {
	// 报告元信息。
	BrandName string `json:"brand_name"`
	Industry  string `json:"industry,omitempty"`
	Category  string `json:"category,omitempty"`
	// 关联公司信息（品牌-公司实体关联，便于前端展示公司名与官网）。
	Company *Company `json:"company,omitempty"`
	// 公司信息完备度评分（0-100），越高说明品牌实体画像越完整。
	EntityCompletenessScore float64   `json:"entity_completeness_score,omitempty"`
	GeneratedAt             time.Time `json:"generated_at"`
	// 品牌可见度评分（BVS，0-100）。
	Score float64 `json:"score"`
	// 评分等级 A-F。
	Grade string `json:"grade"`
	// 品牌梯队：household（头部）/ midmarket（中坚）/ niche（长尾）。
	Tier string `json:"tier"`
	// 6 维评分明细。
	ScoreBreakdown ScoreBreakdown `json:"score_breakdown"`
	// 各引擎统计。
	EngineStats []EngineStats `json:"engine_stats"`
	// 内容缺口（高优先级运营机会）。
	ContentGaps []ContentGap `json:"content_gaps,omitempty"`
	// 竞品整体声量。
	CompetitorSOV []CompetitorSOV `json:"competitor_sov,omitempty"`
	// 竞品声量份额（加权版）：按引擎覆盖/位置加权，比裸提及更可信。
	WeightedCompetitorSOV []CompetitorSOV `json:"weighted_competitor_sov,omitempty"`
	// 品牌准确性/幻觉检测标记（P0-3）：AI 回答与已核验事实的冲突/编造。
	AccuracyFlags []llmanalysis.AccuracyFlag `json:"accuracy_flags,omitempty"`
	// 采样元信息：本报告采用的采样次数与整体一致性（多次采样时）。
	Sampling *SamplingInfo `json:"sampling,omitempty"`
	// 买家人设分群测量（P1-c）：按人设聚合的可见度/情感。
	PersonaBreakdown []persona.Segment `json:"persona_breakdown,omitempty"`
	// AI 引荐流量 / ROI 归因（P0-2，可选，由外部流量源计算后注入）。
	Attribution *attribution.AttributionReport `json:"attribution,omitempty"`
	// 负面情感提及摘要。
	NegativeMentions []NegativeMention `json:"negative_mentions,omitempty"`
	// 运营行动建议（按优先级排序）。
	Actions []ActionItem `json:"actions"`
	// 健康问题清单：按严重级别排序（Critical 优先），供报告与 CI gate 使用。
	SeverityIssues []HealthIssue `json:"severity_issues,omitempty"`
	// 业务类型联动结果（行业检测、权重覆盖、策略调整、运营建议）。
	VerticalLink *VerticalLink `json:"vertical_link,omitempty"`
	// 原始检测结果。
	Results []PromptResult `json:"results,omitempty"`
}

// HealthIssue 单个健康问题，关联 BVS 维度得分与严重级别。
type HealthIssue struct {
	// Dimension 维度名（内容质量/技术SEO/站内SEO/Schema/页面性能/AI就绪/图像优化/Experience/...）。
	Dimension string `json:"dimension"`
	// Score 该维度得分（0-100）。
	Score float64 `json:"score"`
	// Severity 严重级别：critical / high / medium / low。
	Severity Severity `json:"severity"`
	// Impact 该问题对品牌可见度的影响描述。
	Impact string `json:"impact,omitempty"`
	// SuggestedFix 建议的修复方向。
	SuggestedFix string `json:"suggested_fix,omitempty"`
}

// Severity 严重级别类型。
type Severity string

const (
	// SeverityCritical 阻断级：必须立即修复，否则品牌在 AI 引擎中几乎不可见。
	SeverityCritical Severity = "critical"
	// SeverityHigh 高优先级：显著影响可见度，应尽快处理。
	SeverityHigh Severity = "high"
	// SeverityMedium 中等优先级：有改进空间，建议在下一迭代处理。
	SeverityMedium Severity = "medium"
	// SeverityLow 低优先级：表现良好，保持即可。
	SeverityLow Severity = "low"
)

// ScoreBreakdown 评分明细。
//
// 包含两套维度：
//   - 引擎可见度 6 维（MentionRate/CitationRate/ShareOfVoice/CitationPosition/Sentiment/EntityRecognition），
//     保留用于历史兼容与引擎级分析。
//   - BVS 加权健康 7 维（ContentQuality/TechnicalSEO/OnPageSEO/Schema/Performance/AIReadiness/ImageOptimization），
//     参考 Claude SEO 权重体系，各维度 0-100，加权求和得到最终 BVS。
//   - E-E-A-T 四维（Experience/Expertise/Authoritativeness/Trustworthiness），
//     对齐 Google 质量评估准则，反映品牌实体在 AI 回答中的可信度信号。
//
// 7 维权重：内容质量 23% + 技术 SEO 22% + 站内 SEO 20% + Schema 10% +
// 页面性能 10% + AI 就绪 10% + 图像优化 5%。
type ScoreBreakdown struct {
	// --- 引擎可见度 6 维（历史兼容，仍由引擎统计填充）---
	MentionRate       float64 `json:"mention_rate"`
	CitationRate      float64 `json:"citation_rate"`
	ShareOfVoice      float64 `json:"share_of_voice"`
	CitationPosition  float64 `json:"citation_position"`
	Sentiment         float64 `json:"sentiment"`
	EntityRecognition float64 `json:"entity_recognition"`

	// --- BVS 加权健康 7 维（权重见 DimWeight 常量）---
	// ContentQuality 内容质量（权重 23%）：反映内容被引用的频率与质量。
	ContentQuality float64 `json:"content_quality"`
	// TechnicalSEO 技术 SEO（权重 22%）：实体识别与结构化基础。
	TechnicalSEO float64 `json:"technical_seo"`
	// OnPageSEO 站内 SEO（权重 20%）：品牌提及率反映站内优化效果。
	OnPageSEO float64 `json:"on_page_seo"`
	// Schema 结构化数据（权重 10%）：实体完备度与 schema 信号。
	Schema float64 `json:"schema"`
	// Performance 页面性能（权重 10%）：引用位置间接反映页面体验。
	Performance float64 `json:"performance"`
	// AIReadiness AI 搜索就绪（权重 10%）：情感正面率与低幽灵引用。
	AIReadiness float64 `json:"ai_readiness"`
	// ImageOptimization 图像优化（权重 5%）：无图片数据时取中性默认分。
	ImageOptimization float64 `json:"image_optimization"`

	// --- E-E-A-T 四维（Google 质量评估准则对齐，各 0-100）---
	// Experience 经验：公司成立年限、产品历史等反映品牌运营经验的信号。
	Experience float64 `json:"experience"`
	// Expertise 专业性：行业匹配度、实体完备度等反映专业深度的信号。
	Expertise float64 `json:"expertise"`
	// Authoritativeness 权威性：引用位置、声量份额等反映行业话语权的信号。
	Authoritativeness float64 `json:"authoritativeness"`
	// Trustworthiness 可信度：工商核验状态、低幽灵引用等反映信息可信度的信号。
	Trustworthiness float64 `json:"trustworthiness"`
}

// SamplingInfo 报告的采样统计信息（多次采样模式）。
type SamplingInfo struct {
	// Samples 实际采用的采样次数（1=单次）。
	Samples int `json:"samples"`
	// AvgConsistency 所有有效查询的平均一致性（0-1），1=全部采样一致。
	AvgConsistency float64 `json:"avg_consistency"`
	// LowConfidenceQueries 一致性 < 0.6 的查询数（结果不稳定，建议复测）。
	LowConfidenceQueries int `json:"low_confidence_queries"`
}

// CompetitorSOV 竞品声量份额。
type CompetitorSOV struct {
	Name         string  `json:"name"`
	MentionCount int     `json:"mention_count"`
	SOV          float64 `json:"sov"` // 占总提及的比例
}

// NegativeMention 负面提及。
type NegativeMention struct {
	Prompt  string            `json:"prompt"`
	Engine  models.EngineType `json:"engine"`
	Snippet string            `json:"snippet"` // 负面上下文片段
	// Category 负面分类（product_issue/service_issue/pricing_issue/competitive_disadvantage/false_info/security_privacy/other）。
	Category string `json:"category,omitempty"`
	// Severity 负面严重级别（critical/high/medium/low），基于分类与上下文判定。
	Severity Severity `json:"severity,omitempty"`
}

// ActionItem 运营行动建议，指导运营人员下一步工作方向。
type ActionItem struct {
	Priority string `json:"priority"` // high / medium / low
	Category string `json:"category"` // content / engine / reputation / entity
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	// 具体待办，运营人员可直接执行。
	Tasks []string `json:"tasks,omitempty"`
	// 预期影响。
	ExpectedImpact string `json:"expected_impact,omitempty"`
}
