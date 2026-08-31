// Package optimizer GEO 优化引擎，编排策略执行流程。
//
// 执行流程（参考 geo-optimizer Go 架构）：
//  1. 分析原始内容 → scoreBefore
//  2. 解析适用的策略集
//  3. 逐策略 Preprocess（规则化预处理）
//  4. 聚合策略 prompt → LLM 改写（若有可用 Provider）
//  5. 逐策略 Postprocess（后处理 + 资产生成）
//  6. 评分优化后内容 → scoreAfter
//  7. 估算可见度与效用指标
//  8. 生成优化建议
package optimizer

import (
	"context"
	"fmt"
	"strings"

	"my-geo/internal/config"
	"my-geo/internal/dualformat"
	"my-geo/internal/llm"
	"my-geo/internal/models"
	"my-geo/internal/optimizer/strategies"
	"my-geo/internal/scorer"
)

// Optimizer GEO 优化引擎。
type Optimizer struct {
	registry *strategies.Registry
	scorer   *scorer.Scorer
	llmMgr   *llm.Manager
}

// New 创建优化引擎。
func New(s *scorer.Scorer, llmMgr *llm.Manager) *Optimizer {
	return &Optimizer{
		registry: strategies.NewRegistry(),
		scorer:   s,
		llmMgr:   llmMgr,
	}
}

// Optimize 执行 GEO 优化。
func (o *Optimizer) Optimize(ctx context.Context, req *models.OptimizationRequest) (*models.OptimizationResponse, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("内容不能为空")
	}
	if req.Language == "" {
		req.Language = "zh"
	}

	// 若未指定策略，根据领域与引擎推荐
	if len(req.Strategies) == 0 {
		req.Strategies = config.RecommendStrategies(req.DomainType, req.TargetEngines)
	}

	// 1. 评分原始内容
	scoreBefore, _ := o.scorer.Score(req.Content)

	// 2. 解析策略
	applicable := o.registry.Resolve(req)

	// 3. 逐策略 Preprocess
	content := req.Content
	var applied []models.StrategyResult
	var prompts []string

	for _, strat := range applicable {
		if !strat.Validate(req) {
			applied = append(applied, models.StrategyResult{
				Strategy: strat.Type(), Applied: false, Detail: "策略不适用于当前请求",
			})
			continue
		}
		beforePre := content
		content = strat.Preprocess(content, req)
		prompt := strat.BuildPrompt(req)
		if prompt != "" {
			prompts = append(prompts, fmt.Sprintf("【%s】\n%s", strat.Name(), prompt))
		}
		changed := beforePre != content
		applied = append(applied, models.StrategyResult{
			Strategy: strat.Type(), Applied: true,
			Improvement: strat.Effectiveness(),
			PWCBoost:    strat.PWCBoost(),
			Detail:      fmt.Sprintf("预处理%s", map[bool]string{true: "已调整", false: "无变更"}[changed]),
		})
	}

	// 4. LLM 改写（若有 prompt 且有可用 Provider）
	if len(prompts) > 0 && o.llmMgr.HasAvailable() {
		combinedPrompt := buildCombinedPrompt(prompts, req)
		rewritten, err := o.llmMgr.Rewrite(ctx, combinedPrompt, content)
		if err == nil && strings.TrimSpace(rewritten) != "" {
			content = rewritten
		}
	}

	// 5. 逐策略 Postprocess（逆序，使后注册的策略先处理）
	for i := len(applicable) - 1; i >= 0; i-- {
		strat := applicable[i]
		if !strat.Validate(req) {
			continue
		}
		content = strat.Postprocess(content, req)
	}

	// 6. 生成结构化资产
	assets := o.generateAssets(req, content)

	// 7. 评分优化后内容
	scoreAfter, _ := o.scorer.Score(content)

	// 8. 估算可见度与效用
	appliedTypes := make([]models.StrategyType, 0, len(applied))
	for _, r := range applied {
		if r.Applied {
			appliedTypes = append(appliedTypes, r.Strategy)
		}
	}
	visibility := o.scorer.EstimateVisibility(scoreAfter, appliedTypes)
	analysis := o.scorer.Analyze(content)
	utility := o.scorer.EstimateUtility(analysis)

	// 9. 生成建议
	recommendations := o.generateRecommendations(scoreBefore, scoreAfter, analysis)

	// 10. 生成双格式内容（HTML + Markdown）
	title := req.Title
	if title == "" && req.Enterprise != nil && req.Enterprise.CompanyName != "" {
		title = req.Enterprise.CompanyName
	}
	dual := dualformat.Render(content, title, dualformat.FormatHTML)

	return &models.OptimizationResponse{
		OptimizedContent:  content,
		MarkdownContent:   dual.Markdown,
		HTMLContent:       string(dual.HTML),
		ScoreBefore:       scoreBefore,
		ScoreAfter:        scoreAfter,
		GeoScore:          visibility,
		UtilityScore:      utility,
		AppliedStrategies: applied,
		Recommendations:   recommendations,
		GeneratedAssets:   assets,
	}, nil
}

// buildCombinedPrompt 组合多策略提示词。
func buildCombinedPrompt(prompts []string, req *models.OptimizationRequest) string {
	var b strings.Builder
	b.WriteString("你是一位 GEO（生成式引擎优化）专家。请根据以下优化策略改写内容，使其更容易被 AI 搜索引擎（如 ChatGPT、Perplexity）引用。\n\n")
	b.WriteString("优化要求：\n")
	for _, p := range prompts {
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	if engineHint := llm.MergeEngines(req.TargetEngines); engineHint != "" {
		b.WriteString(engineHint)
		b.WriteString("\n\n")
	}
	b.WriteString("检索友好度约束（SAGEO Arena 2026 研究发现：内容扩写会稀释关键词密度导致检索排名下降）：\n")
	b.WriteString("- 控制改写后文档长度在 300-2000 词区间，避免过度扩写\n")
	b.WriteString("- 保留原文中的高频关键词，不要用同义词替换所有术语\n")
	b.WriteString("- 关键词密度保持在 1%-5%（即每个关键词在文中出现 1-5 次/百词）\n")
	b.WriteString("- 保留数字、缩写、专有名词等实体词，这些是检索匹配的关键信号\n")
	b.WriteString("- 优先增加结构化信息（标题、列表、表格）而非纯文本扩展\n\n")
	b.WriteString("约束：\n")
	b.WriteString("- 保持原文核心语义不变，不得编造事实\n")
	b.WriteString("- 自然融入优化，避免生硬堆砌\n")
	b.WriteString("- 输出纯文本/Markdown，不要解释优化过程\n")
	return b.String()
}

// generateAssets 生成结构化资产（llms.txt）。
func (o *Optimizer) generateAssets(req *models.OptimizationRequest, content string) *models.GeneratedAssets {
	assets := &models.GeneratedAssets{}

	// 从内容中提取 JSON-LD（schema 策略生成）。
	// 起点 idx 是 ```json-ld 开栏自身，闭合 ``` 必须从开栏之后搜起——
	// 此前 content[idx:] 开头就是 "```json-ld"，Index 恒命中开栏返回 0，
	// end > 0 永假，JSONLD 恒为空。
	if idx := strings.Index(content, "```json-ld"); idx >= 0 {
		bodyStart := idx + len("```json-ld")
		if end := strings.Index(content[bodyStart:], "```"); end >= 0 {
			assets.JSONLD = strings.TrimSpace(content[bodyStart : bodyStart+end])
		}
	}

	// 生成 llms.txt（参考 llms.txt 标准）
	var b strings.Builder
	b.WriteString("# ")
	if req.Enterprise != nil && req.Enterprise.CompanyName != "" {
		b.WriteString(req.Enterprise.CompanyName)
	} else if req.Title != "" {
		b.WriteString(req.Title)
	} else {
		b.WriteString("站点内容")
	}
	b.WriteString("\n\n")
	b.WriteString("> ")
	if req.Enterprise != nil && req.Enterprise.Description != "" {
		b.WriteString(req.Enterprise.Description)
	} else {
		b.WriteString("本页面内容已针对生成式引擎优化，便于 AI 搜索引擎理解与引用。")
	}
	b.WriteString("\n\n")
	if req.URL != "" {
		b.WriteString("## 站点\n")
		b.WriteString(fmt.Sprintf("- [%s](%s)\n", "主页", req.URL))
	}
	b.WriteString("## 摘要\n")
	// 取首段作为摘要
	summary := req.Content
	if i := strings.Index(summary, "\n\n"); i > 0 {
		summary = summary[:i]
	}
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:200]) + "..."
	}
	b.WriteString(summary)
	b.WriteString("\n")
	assets.LLMsTxt = b.String()

	return assets
}

// generateRecommendations 生成优化建议。
func (o *Optimizer) generateRecommendations(scoreBefore, scoreAfter float64, a *models.ContentAnalysis) []models.Recommendation {
	var recs []models.Recommendation

	if scoreAfter < 60 {
		recs = append(recs, models.Recommendation{
			Category: "overall", Priority: "high",
			Message: fmt.Sprintf("当前 GEO 评分 %.0f，建议继续应用更多优化策略提升可引用性", scoreAfter),
		})
	}
	// 可引用性建议
	if !a.CitabilitySignals["cite_sources"] {
		recs = append(recs, models.Recommendation{
			Category: "citability", Priority: "high",
			Message: "内容缺乏引用来源，建议为关键论断补充可信来源（可提升 ~27% 可见度）",
		})
	}
	if !a.CitabilitySignals["statistics"] {
		recs = append(recs, models.Recommendation{
			Category: "citability", Priority: "high",
			Message: "内容缺乏统计数据，建议补充具体数值与百分比（可提升 ~33% 可见度）",
		})
	}
	if !a.CitabilitySignals["quotation"] {
		recs = append(recs, models.Recommendation{
			Category: "citability", Priority: "medium",
			Message: "建议补充权威引用语，直接引述专家观点（可提升 ~41% 可见度）",
		})
	}
	// 结构建议
	if !a.StructureSignals["front_loading"] {
		recs = append(recs, models.Recommendation{
			Category: "structure", Priority: "medium",
			Message: "建议将核心结论前置到首段，便于 AI 引擎快速提取",
		})
	}
	if !a.StructureSignals["heading_hierarchy"] {
		recs = append(recs, models.Recommendation{
			Category: "structure", Priority: "medium",
			Message: "建议使用标题层级（H1-H3）组织内容结构",
		})
	}
	// 负向信号
	for _, neg := range a.NegativeSignals {
		recs = append(recs, models.Recommendation{
			Category: "negative", Priority: "high",
			Message: fmt.Sprintf("检测到负向信号: %s，建议消除以避免被 AI 引擎降权", neg),
		})
	}
	// 检索友好度建议（SAGEO Arena 2026）
	if a.RetrievalSignals != nil {
		if !a.RetrievalSignals.ContentLengthOK {
			recs = append(recs, models.Recommendation{
				Category: "retrieval", Priority: "high",
				Message: fmt.Sprintf("内容长度不在检索友好区间（当前 %d 词，推荐 300-2000 词），过长的内容会稀释关键词密度导致检索排名下降", a.WordCount),
			})
		}
		if a.RetrievalSignals.KeywordDensity < 0.01 {
			recs = append(recs, models.Recommendation{
				Category: "retrieval", Priority: "medium",
				Message: "关键词密度过低，建议保留更多高频关键词而非用同义词全部替换",
			})
		}
		if a.RetrievalSignals.KeywordDensity > 0.05 {
			recs = append(recs, models.Recommendation{
				Category: "retrieval", Priority: "medium",
				Message: "关键词密度过高，可能存在关键词堆砌风险，建议自然分散关键词",
			})
		}
		if !a.RetrievalSignals.NoSemanticDrift {
			recs = append(recs, models.Recommendation{
				Category: "retrieval", Priority: "medium",
				Message: "实体词保留率偏低，改写可能造成语义漂移，建议保留更多专有名词和缩写",
			})
		}
	}
	return recs
}

// Registry 暴露策略注册表，供外部查询可用策略。
func (o *Optimizer) Registry() *strategies.Registry { return o.registry }
