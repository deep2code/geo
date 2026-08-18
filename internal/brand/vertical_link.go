package brand

import (
	"my-geo/internal/brand/vertical"
	"my-geo/internal/models"
)

// VerticalLink 业务类型与策略/评分的联动结果。
type VerticalLink struct {
	// Detected 检测到的业务垂直行业。
	Detected vertical.Vertical `json:"detected"`
	// Label 行业中文标签。
	Label string `json:"label"`
	// AppliedWeights 实际应用的 BVS 维度权重（已归一化到合计 1.0）。
	AppliedWeights map[string]float64 `json:"applied_weights"`
	// WeightedBVS 基于行业权重重算的 BVS 分数（0-100）。
	WeightedBVS float64 `json:"weighted_bvs"`
	// StrategyAdjustments 针对该行业推荐调整的策略组合。
	StrategyAdjustments []models.StrategyType `json:"strategy_adjustments,omitempty"`
	// Recommendations 行业差异化运营建议。
	Recommendations []vertical.Recommendation `json:"recommendations,omitempty"`
}

// DetectVertical 检测品牌画像对应的业务垂直行业。
//
// 将 BrandProfile 转换为 vertical.Detect 所需的 map 后执行检测。
func DetectVertical(profile BrandProfile) vertical.Vertical {
	return vertical.Detect(profileToVerticalMap(profile))
}

// LinkVertical 将品牌画像与业务类型联动，生成权重覆盖、策略调整与运营建议。
//
// bvs 为按默认 7 维权重计算出的 BVS 分数，breakdown 为其明细；
// 本函数用检测到的行业权重重算 BVS（WeightedBVS），并返回联动结果。
func LinkVertical(profile BrandProfile, breakdown ScoreBreakdown, bvs float64) VerticalLink {
	v := DetectVertical(profile)
	cfg := vertical.GetConfig(v)
	link := VerticalLink{
		Detected:       v,
		Label:          cfg.Label,
		AppliedWeights: normalizeWeights(cfg.ScoreWeights),
	}
	// 用行业权重重算 BVS
	link.WeightedBVS = applyWeightOverrides(breakdown, cfg.ScoreWeights)
	// 策略调整：基于行业推荐策略
	link.StrategyAdjustments = verticalStrategies(v)
	// 运营建议
	link.Recommendations = vertical.RecommendationsFor(v, bvs)
	return link
}

// profileToVerticalMap 将 BrandProfile 转换为 vertical.Detect 所需的 map。
func profileToVerticalMap(profile BrandProfile) map[string]interface{} {
	m := map[string]interface{}{
		"industry": profile.Industry,
		"category": profile.Category,
		"domain":   profile.Domain,
		"products": toInterfaceSlice(profile.Products),
	}
	if profile.Company != nil {
		companyMap := map[string]interface{}{
			"name":        profile.Company.Name,
			"description": profile.Company.Description,
			"industry":    profile.Company.Industry,
		}
		m["company"] = companyMap
		m["company_description"] = profile.Company.Description
	}
	return m
}

// toInterfaceSlice 将 []string 转为 []interface{}。
func toInterfaceSlice(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// applyWeightOverrides 用行业权重覆盖默认 7 维权重，重算 BVS。
//
// 行业权重 map 的 key：content_quality/technical_seo/schema/ai_readiness/onpage/performance/image/local_seo。
// local_seo 在 BVS 7 维中无直接对应，作为额外加分项（不超过 100）。
// 未提供的维度沿用默认权重。最终权重重新归一化到合计 1.0。
func applyWeightOverrides(bd ScoreBreakdown, overrides map[string]float64) float64 {
	if len(overrides) == 0 {
		// 无覆盖，用默认权重
		return bd.ContentQuality*WeightContentQuality +
			bd.TechnicalSEO*WeightTechnicalSEO +
			bd.OnPageSEO*WeightOnPageSEO +
			bd.Schema*WeightSchema +
			bd.Performance*WeightPerformance +
			bd.AIReadiness*WeightAIReadiness +
			bd.ImageOptimization*WeightImageOptimization
	}
	// 构建 维度得分 → 权重 映射（使用覆盖值或默认值）
	type dimWeight struct {
		score  float64
		weight float64
	}
	dims := []dimWeight{
		{bd.ContentQuality, getWeight(overrides, "content_quality", WeightContentQuality)},
		{bd.TechnicalSEO, getWeight(overrides, "technical_seo", WeightTechnicalSEO)},
		{bd.OnPageSEO, getWeight(overrides, "onpage", WeightOnPageSEO)},
		{bd.Schema, getWeight(overrides, "schema", WeightSchema)},
		{bd.Performance, getWeight(overrides, "performance", WeightPerformance)},
		{bd.AIReadiness, getWeight(overrides, "ai_readiness", WeightAIReadiness)},
		{bd.ImageOptimization, getWeight(overrides, "image", WeightImageOptimization)},
	}
	// local_seo 额外加分（本地服务行业）
	localSEOScore := 60.0 // 默认中性
	localSEOWeight := overrides["local_seo"]
	if localSEOWeight > 0 {
		dims = append(dims, dimWeight{localSEOScore, localSEOWeight})
	}
	// 归一化权重
	totalWeight := 0.0
	for _, d := range dims {
		totalWeight += d.weight
	}
	if totalWeight == 0 {
		return 0
	}
	bvs := 0.0
	for _, d := range dims {
		bvs += d.score * (d.weight / totalWeight)
	}
	if bvs > 100 {
		bvs = 100
	}
	return round(bvs)
}

// getWeight 从覆盖 map 获取权重，不存在则返回默认值。
func getWeight(overrides map[string]float64, key string, defaultW float64) float64 {
	if w, ok := overrides[key]; ok && w > 0 {
		return w
	}
	return defaultW
}

// normalizeWeights 归一化权重 map 到合计 1.0（用于展示）。
func normalizeWeights(w map[string]float64) map[string]float64 {
	if len(w) == 0 {
		return nil
	}
	total := 0.0
	for _, v := range w {
		total += v
	}
	if total == 0 {
		return w
	}
	out := make(map[string]float64, len(w))
	for k, v := range w {
		out[k] = round(v / total * 1.0) // 保留原比例（0-1）
	}
	return out
}

// verticalStrategies 基于业务垂直行业返回推荐的 GEO 策略组合。
func verticalStrategies(v vertical.Vertical) []models.StrategyType {
	switch v {
	case vertical.VerticalSaaS:
		return []models.StrategyType{
			models.StrategySchema, models.StrategyCiteSources, models.StrategyStatistics,
			models.StrategyTechnicalTerms, models.StrategyStructure,
		}
	case vertical.VerticalLocalService:
		return []models.StrategyType{
			models.StrategySchema, models.StrategyCiteSources, models.StrategyAuthoritative,
			models.StrategyStructure, models.StrategyAnswerFirst,
		}
	case vertical.VerticalEcommerce:
		return []models.StrategyType{
			models.StrategySchema, models.StrategyStatistics, models.StrategyCiteSources,
			models.StrategyStructure, models.StrategyAnswerFirst,
		}
	case vertical.VerticalPublisher:
		return []models.StrategyType{
			models.StrategyCiteSources, models.StrategyAuthoritative, models.StrategyStatistics,
			models.StrategyStructure, models.StrategySchema,
		}
	case vertical.VerticalAgency:
		return []models.StrategyType{
			models.StrategyAuthoritative, models.StrategyCiteSources, models.StrategyStatistics,
			models.StrategyStructure, models.StrategyAnswerFirst,
		}
	default:
		return []models.StrategyType{
			models.StrategyCiteSources, models.StrategyStatistics, models.StrategyStructure,
			models.StrategyAnswerFirst, models.StrategySchema,
		}
	}
}
