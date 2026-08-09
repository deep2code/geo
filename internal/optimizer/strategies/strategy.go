// Package strategies 定义 GEO 优化策略接口与实现。
//
// 采用策略模式 (Strategy Pattern)，参考 geo-optimizer (Go) 的设计：
// 每个策略实现统一接口，可插拔注册到优化引擎。
// 策略实现基于 Princeton GEO 论文的 9 种优化方法。
package strategies

import "my-geo/internal/models"

// Strategy GEO 优化策略接口。
//
// 每个策略负责：
//   - Name/Type: 标识
//   - Validate: 判断请求是否适用
//   - Preprocess: 预处理内容（规则化调整）
//   - BuildPrompt: 构建 LLM 改写提示词
//   - Postprocess: 后处理 LLM 输出
type Strategy interface {
	// Name 策略名称。
	Name() string
	// Type 策略类型标识。
	Type() models.StrategyType
	// Validate 判断该策略是否适用于当前请求。
	Validate(req *models.OptimizationRequest) bool
	// Preprocess 规则化预处理内容（不调用 LLM）。
	Preprocess(content string, req *models.OptimizationRequest) string
	// BuildPrompt 构建 LLM 改写提示词；返回空串表示该策略无需 LLM。
	BuildPrompt(req *models.OptimizationRequest) string
	// Postprocess 对 LLM 输出做后处理（清洗/格式化）。
	Postprocess(content string, req *models.OptimizationRequest) string
	// Effectiveness 预期效果系数（0-1）。
	Effectiveness() float64
	// PWCBoost 返回该策略的理论 PWC（Position-Adjusted Word Count）增益百分比。
	// 正值表示提升（如 42.6 表示 +42.6%），负值表示应避免（如 -8.7 表示关键词堆砌会降权）。
	// 数据来源：Princeton GEO 论文（KDD 2024）实验基准。
	PWCBoost() float64
}

// Registry 策略注册表。
type Registry struct {
	strategies map[models.StrategyType]Strategy
}

// NewRegistry 创建策略注册表并注册全部内置策略。
func NewRegistry() *Registry {
	r := &Registry{strategies: make(map[models.StrategyType]Strategy)}
	r.Register(&CiteSourcesStrategy{})
	r.Register(&StatisticsStrategy{})
	r.Register(&AuthoritativeStrategy{})
	r.Register(&QuotationStrategy{})
	r.Register(&FluencyStrategy{})
	r.Register(&EasyUnderstandStrategy{})
	r.Register(&KeywordStrategy{})
	r.Register(&UniqueWordsStrategy{})
	r.Register(&TechnicalTermsStrategy{})
	r.Register(&StructureStrategy{})
	r.Register(&FAQStrategy{})
	r.Register(&SchemaStrategy{})
	r.Register(&AnswerFirstStrategy{})
	return r
}

// Register 注册策略。
func (r *Registry) Register(s Strategy) {
	r.strategies[s.Type()] = s
}

// Get 获取指定策略。
func (r *Registry) Get(t models.StrategyType) (Strategy, bool) {
	s, ok := r.strategies[t]
	return s, ok
}

// All 返回全部已注册策略。
func (r *Registry) All() []Strategy {
	result := make([]Strategy, 0, len(r.strategies))
	for _, s := range r.strategies {
		result = append(result, s)
	}
	return result
}

// Resolve 解析请求中的策略列表；为空时返回全部策略。
func (r *Registry) Resolve(req *models.OptimizationRequest) []Strategy {
	if len(req.Strategies) == 0 {
		return r.All()
	}
	var result []Strategy
	for _, t := range req.Strategies {
		if s, ok := r.strategies[t]; ok {
			result = append(result, s)
		}
	}
	return result
}
