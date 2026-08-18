package scorer

import "my-geo/internal/config"

// ApplyRuleSet 将外部化规则集应用到评分引擎：覆盖权重 + 策略效果系数。
//
// 零值/空字段忽略；非法字段（负权重、未知策略键）由 config.RuleSet.Validate 拦截，
// 此处仅做幂等应用。应在启动时调用一次（Scorer 创建前），非并发安全。
func ApplyRuleSet(rs *config.RuleSet) {
	if rs == nil {
		return
	}
	if len(rs.Weights) > 0 {
		OverrideWeights(rs.Weights)
	}
	if len(rs.StrategyEffectiveness) > 0 {
		config.SetStrategyEffectiveness(rs.StrategyEffectiveness)
	}
}
