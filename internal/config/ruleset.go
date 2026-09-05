// 规则集外部化：将评分权重与策略触发条件从硬编码常量改为可加载的 JSON 资产。
//
// 对标 AutoGEO (ICLR 2026) 的 rule extraction——把"评分经验"沉淀为可版本化、
// 可按行业/引擎偏好组合的配置，而非散落在 scorer.go 与 config.go 的包级 var。
// 这是战略级改进方向 #2（规则集外部化）的核心落地，也是 P2-5 权重配置化的正式形态。
//
// 用法：
//
//	rs, _ := config.LoadRuleSet("config/rules/zh-ecommerce.json")
//	scorer.ApplyRuleSet(rs) // 覆盖权重 + 策略效果系数
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"my-geo/internal/models"
)

// TriggerRule 单个策略的触发条件（阈值化），预留给规则驱动的优化调度。
type TriggerRule struct {
	// Enabled 是否在规则集中启用该策略（false = 强制关闭）。
	Enabled bool `json:"enabled" yaml:"enabled"`
	// MinScore 命中该策略所需的最低内容评分（0-100）；0 表示不限制。
	MinScore float64 `json:"min_score,omitempty" yaml:"min_score,omitempty"`
}

// RuleSet GEO 规则集：评分权重 + 策略效果系数 + 策略触发条件。
//
// 通过 Version 字段版本化；通过 Engine / Domain 字段声明适用场景，
// 便于"一个基准 + 多个行业/引擎叠加"的组合式管理。
type RuleSet struct {
	Version               string                              `json:"version" yaml:"version"`
	Name                  string                              `json:"name" yaml:"name"`
	Description           string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Engine                models.EngineType                  `json:"engine,omitempty" yaml:"engine,omitempty"`
	Domain                models.DomainType                  `json:"domain,omitempty" yaml:"domain,omitempty"`
	Weights               map[string]float64                  `json:"weights" yaml:"weights"`
	StrategyEffectiveness map[models.StrategyType]float64     `json:"strategy_effectiveness" yaml:"strategy_effectiveness"`
	StrategyTriggers      map[models.StrategyType]TriggerRule `json:"strategy_triggers,omitempty" yaml:"strategy_triggers,omitempty"`
}

// DefaultWeights 当前评分权重副本（与 scorer.go 包级 var 基线完全一致），
// 供生成/对比规则集使用。
func DefaultWeights() map[string]float64 {
	return map[string]float64{
		"quotation":         8.0,
		"statistics":        7.0,
		"cite_sources":      7.0,
		"fluency":           6.0,
		"authoritative":     5.0,
		"technical_terms":   4.0,
		"unique_words":      3.0,
		"heading_hierarchy": 8.0,
		"front_loading":     7.0,
		"lists":             5.0,
		"definition_opening": 5.0,
		"tables":            3.0,
		"faq":               2.0,
		"negative_penalty":  2.5,
		"evergreen":         12.0,
		"retrieval":         15.0,
	}
}

// DefaultRuleSet 返回内置默认规则集（与代码硬编码基线一致）。
func DefaultRuleSet() *RuleSet {
	se := make(map[models.StrategyType]float64, len(StrategyEffectiveness()))
	for k, v := range StrategyEffectiveness() {
		se[k] = v
	}
	return &RuleSet{
		Version:               "builtin-1.0.0",
		Name:                  "default",
		Description:           "内置默认规则集（与代码硬编码基线一致）",
		Weights:               DefaultWeights(),
		StrategyEffectiveness: se,
	}
}

// LoadRuleSet 从 JSON 文件加载并校验规则集。
func LoadRuleSet(path string) (*RuleSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取规则集失败: %w", err)
	}
	var rs RuleSet
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, fmt.Errorf("解析规则集失败: %w", err)
	}
	if err := rs.Validate(); err != nil {
		return nil, err
	}
	return &rs, nil
}

// Validate 校验规则集合法性：版本/名称非空，权重与策略系数非负。
func (rs *RuleSet) Validate() error {
	if rs.Version == "" {
		return fmt.Errorf("规则集 version 不能为空")
	}
	if rs.Name == "" {
		return fmt.Errorf("规则集 name 不能为空")
	}
	for k, v := range rs.Weights {
		if v < 0 {
			return fmt.Errorf("权重 %s 为负: %v", k, v)
		}
	}
	for k, v := range rs.StrategyEffectiveness {
		if v < 0 {
			return fmt.Errorf("策略效果系数 %s 为负: %v", k, v)
		}
		if _, ok := StrategyEffectiveness()[k]; !ok {
			return fmt.Errorf("未知策略类型: %s", k)
		}
	}
	return nil
}

// SetStrategyEffectiveness 用规则集覆盖当前策略效果系数（copy-on-write，
// 并发安全：整表替换原子指针，读侧无锁）。仅覆盖合法策略键，未知键与负值忽略。
func SetStrategyEffectiveness(m map[models.StrategyType]float64) {
	cur := *strategyEffPtr.Load()
	next := make(map[models.StrategyType]float64, len(cur)+len(m))
	for k, v := range cur {
		next[k] = v
	}
	for k, v := range m {
		if v < 0 {
			continue
		}
		if _, ok := cur[k]; ok {
			next[k] = v
		}
	}
	strategyEffPtr.Store(&next)
}
