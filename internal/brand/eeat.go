package brand

import (
	"math"
	"strings"
	"time"
)

// EEATBreakdown E-E-A-T 四维评分明细，各维度 0-100。
type EEATBreakdown struct {
	Experience       float64 `json:"experience"`
	Expertise        float64 `json:"expertise"`
	Authoritativeness float64 `json:"authoritativeness"`
	Trustworthiness  float64 `json:"trustworthiness"`
}

// ScoreEEAT 计算 Google E-E-A-T 四维评分（Experience/Expertise/Authoritativeness/Trustworthiness）。
//
// 评分信号映射（基于现有数据，无需额外爬取）：
//   - Experience 经验：公司成立年限越久分越高（≥20 年满分），无公司信息取中性分
//   - Expertise 专业性：实体完备度(50%) + 行业字段完整(30%) + 产品线丰富度(20%)
//   - Authoritativeness 权威性：引用位置得分(60%) + 声量份额(40%)
//   - Trustworthiness 可信度：工商登记状态(40%) + 低幽灵引用率(40%) + 实体完备度(20%)
//
// 参数：
//   - profile: 品牌画像（含公司成立年份、行业、工商状态等）
//   - avgPos: 跨引擎平均引用位置得分（0-100，已由 positionScore 转换）
//   - avgGhost: 跨引擎平均幽灵引用率（0-100，越低越好）
//   - entityCompleteness: 实体完备度（0-100）
func ScoreEEAT(profile BrandProfile, avgPos, avgGhost, entityCompleteness float64) EEATBreakdown {
	return EEATBreakdown{
		Experience:        scoreExperience(profile),
		Expertise:         scoreExpertise(profile, entityCompleteness),
		Authoritativeness: scoreAuthoritativeness(avgPos, profile),
		Trustworthiness:   scoreTrustworthiness(profile, avgGhost, entityCompleteness),
	}
}

// scoreExperience 经验维度评分。
//
// 公司成立年限越长，品牌运营经验越丰富，AI 引擎对老牌品牌更信任。
//   - ≥20 年：满分 100
//   - 10-20 年：70-100 线性
//   - 3-10 年：40-70 线性
//   - <3 年或无公司信息：中性分 50（避免新品牌被过度惩罚）
func scoreExperience(profile BrandProfile) float64 {
	var foundedYear int
	if profile.Company != nil {
		foundedYear = profile.Company.FoundedYear
	}
	if foundedYear <= 0 {
		return 50 // 无公司信息，取中性分
	}
	now := time.Now().Year()
	years := now - foundedYear
	if years < 0 {
		return 50
	}
	var score float64
	switch {
	case years >= 20:
		score = 100
	case years >= 10:
		score = 70 + float64(years-10)/10*30
	case years >= 3:
		score = 40 + float64(years-3)/7*30
	default:
		score = 50
	}
	return math.Min(score, 100)
}

// scoreExpertise 专业性维度评分。
//
// 实体完备度(50%) + 行业字段完整(30%) + 产品线丰富度(20%)。
func scoreExpertise(profile BrandProfile, entityCompleteness float64) float64 {
	// 实体完备度
	ecScore := math.Max(entityCompleteness, 0)
	// 行业字段完整性：品牌 Industry 与 Company.Industry 均填=100，仅一个=70，都没填=40
	industryScore := 40.0
	hasBrandIndustry := strings.TrimSpace(profile.Industry) != ""
	hasCompanyIndustry := profile.Company != nil && strings.TrimSpace(profile.Company.Industry) != ""
	if hasBrandIndustry && hasCompanyIndustry {
		industryScore = 100
	} else if hasBrandIndustry || hasCompanyIndustry {
		industryScore = 70
	}
	// 产品线丰富度：0 个产品=30，1-2 个=60，3-4 个=85，5+ 个=100
	productScore := 30.0
	n := len(profile.Products)
	switch {
	case n >= 5:
		productScore = 100
	case n >= 3:
		productScore = 85
	case n >= 1:
		productScore = 60
	}
	score := ecScore*0.5 + industryScore*0.3 + productScore*0.2
	return math.Min(score, 100)
}

// scoreAuthoritativeness 权威性维度评分。
//
// 引用位置得分(60%) + 声量份额(40%)。
// avgPos 已是 0-100 的位置得分（位置 1=100，越靠后越低）。
func scoreAuthoritativeness(avgPos float64, profile BrandProfile) float64 {
	posScore := math.Max(avgPos, 0)
	// 声量份额无独立传入，用 entityCompleteness 作为权威信号代理（实体越完整越权威）
	// 同时考虑公司是否有工商核验（RegistrationStatus 非空代表已核验）
	authBase := math.Max(EntityCompleteness(profile), 0)
	// 工商核验加权：已核验且在营 +10
	if profile.Company != nil && strings.TrimSpace(profile.Company.RegistrationStatus) != "" {
		status := strings.TrimSpace(profile.Company.RegistrationStatus)
		if isPositiveRegistrationStatus(status) {
			authBase = math.Min(authBase+10, 100)
		}
	}
	score := posScore*0.6 + authBase*0.4
	return math.Min(score, 100)
}

// scoreTrustworthiness 可信度维度评分。
//
// 工商登记状态(40%) + 低幽灵引用率(40%) + 实体完备度(20%)。
func scoreTrustworthiness(profile BrandProfile, avgGhost, entityCompleteness float64) float64 {
	// 工商登记状态得分
	registrationScore := 50.0 // 默认中性（无工商数据）
	if profile.Company != nil {
		status := strings.TrimSpace(profile.Company.RegistrationStatus)
		if status != "" {
			switch {
			case isPositiveRegistrationStatus(status):
				registrationScore = 100
			case isNegativeRegistrationStatus(status):
				registrationScore = 20
			default:
				registrationScore = 70
			}
		}
		// 有统一社会信用代码 = 已通过官方核验，加分
		if strings.TrimSpace(profile.Company.CreditCode) != "" {
			registrationScore = math.Min(registrationScore+10, 100)
		}
	}
	// 低幽灵引用率得分：幽灵引用率越低分越高
	ghostScore := math.Max(100-avgGhost, 0)
	// 实体完备度
	ecScore := math.Max(entityCompleteness, 0)
	score := registrationScore*0.4 + ghostScore*0.4 + ecScore*0.2
	return math.Min(score, 100)
}

// isPositiveRegistrationStatus 判断工商登记状态是否为正向（在营/存续/正常）。
func isPositiveRegistrationStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	positiveKeywords := []string{"在营", "存续", "正常", "存续（在营）", "active", "operating"}
	for _, kw := range positiveKeywords {
		if strings.Contains(s, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// isNegativeRegistrationStatus 判断工商登记状态是否为负向（吊销/注销/停业/清算）。
func isNegativeRegistrationStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	negativeKeywords := []string{"吊销", "注销", "停业", "清算", "迁出", "revoked", "cancelled", "dissolved"}
	for _, kw := range negativeKeywords {
		if strings.Contains(s, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
