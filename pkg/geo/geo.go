// Package geo 提供 GEO 系统的公开 API，是使用 GEO 能力的统一入口。
//
// 封装 analyzer + scorer + optimizer + llm，提供简洁的 Optimize/Score/Analyze 接口。
//
// 基本用法：
//
//	engine := geo.New(geo.WithOpenAI("sk-xxx", "gpt-4o-mini"))
//	resp, err := engine.Optimize(ctx, &geo.OptimizationRequest{Content: "..."})
package geo

import (
	"context"

	"my-geo/internal/analyzer"
	"my-geo/internal/config"
	"my-geo/internal/llm"
	"my-geo/internal/models"
	"my-geo/internal/optimizer"
	"my-geo/internal/scorer"
)

// Engine GEO 优化引擎，系统对外统一入口。
type Engine struct {
	optimizer *optimizer.Optimizer
	scorer    *scorer.Scorer
	analyzer  *analyzer.Analyzer
}

// Option Engine 配置选项。
type Option func(*engineConfig)

type engineConfig struct {
	llmProvider llm.Provider
	budgetUSD   float64 // LLM 月度预算上限（USD），>0 时超限熔断
}

// WithLLM 注入自定义 LLM Provider。
func WithLLM(p llm.Provider) Option {
	return func(c *engineConfig) { c.llmProvider = p }
}

// WithOpenAI 快捷配置 OpenAI 兼容 LLM。
//
// baseURL/model 为空时使用默认值（https://api.openai.com / gpt-4o-mini）。
func WithOpenAI(apiKey, baseURL, model string) Option {
	return func(c *engineConfig) {
		if apiKey == "" {
			return
		}
		opts := []llm.OpenAIOption{}
		if baseURL != "" {
			opts = append(opts, llm.WithBaseURL(baseURL))
		}
		if model != "" {
			opts = append(opts, llm.WithModel(model))
		}
		c.llmProvider = llm.NewOpenAI(apiKey, opts...)
	}
}

// WithBudgetUSD 设置 LLM 月度预算上限（USD）；超限后引擎拒绝后续 LLM 调用。
func WithBudgetUSD(limit float64) Option {
	return func(c *engineConfig) {
		if limit > 0 {
			c.budgetUSD = limit
		}
	}
}

// New 创建 GEO 引擎。
//
// 未配置 LLM 时，系统仍可运行（仅规则化预处理 + 评分 + 建议），不调用 LLM。
func New(opts ...Option) *Engine {
	cfg := &engineConfig{}
	for _, o := range opts {
		o(cfg)
	}

	a := analyzer.New()
	sc := scorer.New(a)

	var providers []llm.Provider
	if cfg.llmProvider != nil {
		providers = append(providers, cfg.llmProvider)
	}
	mgr := llm.NewManagerWithOptions(providers, llm.WithMonthlyBudgetUSD(cfg.budgetUSD))

	opt := optimizer.New(sc, mgr)
	return &Engine{optimizer: opt, scorer: sc, analyzer: a}
}

// Optimize 执行 GEO 优化。
func (e *Engine) Optimize(ctx context.Context, req *models.OptimizationRequest) (*models.OptimizationResponse, error) {
	return e.optimizer.Optimize(ctx, req)
}

// Score 对内容评分（0-100），返回总分与明细。
func (e *Engine) Score(content string) (float64, []models.ScoreBreakdown) {
	return e.scorer.Score(content)
}

// Analyze 分析内容的 GEO 信号。
func (e *Engine) Analyze(content string) *models.ContentAnalysis {
	return e.analyzer.Analyze(content)
}

// ApplyRuleSet 应用外部化规则集（覆盖评分权重与策略效果系数）。
//
// 详见 internal/config/ruleset.go——将评分经验从硬编码改为可版本化配置，
// 支持按行业/引擎偏好组合。应在启动早期、任何 Score/Optimize 调用之前调用。
func (e *Engine) ApplyRuleSet(rs *config.RuleSet) {
	scorer.ApplyRuleSet(rs)
}

// EstimateVisibility 预估给定评分在应用指定策略后的可见度指标（评测集用）。
//
// 与 optimizer 内部使用同一套策略效果系数，便于离线投影"若应用推荐策略"的预期提升，
// 不依赖 LLM 实际改写结果。详见 internal/eval。
func (e *Engine) EstimateVisibility(score float64, applied []models.StrategyType) models.VisibilityMetrics {
	return e.scorer.EstimateVisibility(score, applied)
}

// Strategies 返回全部可用策略类型。
func (e *Engine) Strategies() []models.StrategyType {
	strats := e.optimizer.Registry().All()
	result := make([]models.StrategyType, 0, len(strats))
	for _, s := range strats {
		result = append(result, s.Type())
	}
	return result
}

// StrategyInfos 返回全部策略的元信息（含 PWC 增益百分比），供 API 展示。
func (e *Engine) StrategyInfos() []models.StrategyInfo {
	strats := e.optimizer.Registry().All()
	result := make([]models.StrategyInfo, 0, len(strats))
	for _, s := range strats {
		result = append(result, models.StrategyInfo{
			Type:          s.Type(),
			Name:          s.Name(),
			Effectiveness: s.Effectiveness(),
			PWCBoost:      s.PWCBoost(),
		})
	}
	return result
}

// RecommendStrategies 根据领域与引擎推荐策略。
func (e *Engine) RecommendStrategies(domain models.DomainType, engines []models.EngineType) []models.StrategyType {
	return config.RecommendStrategies(domain, engines)
}
