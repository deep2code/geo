// Package config 提供 GEO 系统的配置与 AI 引擎预设。
//
// 预设数据参考 geo-optimizer (Go) 的平台预配置与 geo-optimizer-skill
// 的引擎偏好研究：不同生成式引擎对内容信号有差异化权重。
package config

import (
	"os"

	"my-geo/internal/adapter"
	"my-geo/internal/models"
)

// AIEnginePresets 各生成式引擎的预设偏好。
//
// 权重值反映该引擎对相应 GEO 信号的敏感程度（0-1）。
var AIEnginePresets = map[models.EngineType]models.AIEnginePreset{
	models.EngineChatGPT: {
		Engine:              models.EngineChatGPT,
		PreferredStrategies: []models.StrategyType{models.StrategyCiteSources, models.StrategyStatistics, models.StrategyAnswerFirst},
		MaxTokens:           4096,
		Temperature:         0.7,
		Weights: map[string]float64{
			"cite_sources":   0.9,
			"statistics":     0.85,
			"answer_first":   0.8,
			"structure":      0.75,
			"fluency":        0.7,
			"authoritative":  0.6,
		},
	},
	models.EnginePerplexity: {
		Engine:              models.EnginePerplexity,
		PreferredStrategies: []models.StrategyType{models.StrategyCiteSources, models.StrategyQuotation, models.StrategyAuthoritative},
		MaxTokens:           4096,
		Temperature:         0.5,
		Weights: map[string]float64{
			"cite_sources":   1.0,
			"quotation":      0.95,
			"authoritative":  0.85,
			"statistics":     0.8,
			"answer_first":   0.7,
			"structure":      0.65,
		},
	},
	models.EngineGemini: {
		Engine:              models.EngineGemini,
		PreferredStrategies: []models.StrategyType{models.StrategyStatistics, models.StrategyTechnicalTerms, models.StrategyStructure},
		MaxTokens:           8192,
		Temperature:         0.4,
		Weights: map[string]float64{
			"statistics":      0.9,
			"technical_terms": 0.85,
			"structure":       0.8,
			"cite_sources":    0.75,
			"answer_first":    0.7,
			"unique_words":    0.6,
		},
	},
	models.EngineClaude: {
		Engine:              models.EngineClaude,
		PreferredStrategies: []models.StrategyType{models.StrategyFluency, models.StrategyAuthoritative, models.StrategyStructure},
		MaxTokens:           8192,
		Temperature:         0.6,
		Weights: map[string]float64{
			"fluency":        0.95,
			"authoritative":  0.9,
			"structure":      0.85,
			"cite_sources":   0.8,
			"answer_first":   0.75,
			"statistics":     0.65,
		},
	},
	// ===== 国内大模型 =====
	models.EngineQwen: {
		Engine:              models.EngineQwen,
		PreferredStrategies: []models.StrategyType{models.StrategyCiteSources, models.StrategyStructure, models.StrategyAnswerFirst},
		MaxTokens:           8192,
		Temperature:         0.6,
		Weights: map[string]float64{
			"cite_sources":   0.85,
			"structure":      0.85,
			"answer_first":   0.8,
			"statistics":     0.75,
			"fluency":        0.7,
			"authoritative":  0.7,
		},
	},
	models.EngineGLM: {
		Engine:              models.EngineGLM,
		PreferredStrategies: []models.StrategyType{models.StrategyStatistics, models.StrategyCiteSources, models.StrategyAnswerFirst},
		MaxTokens:           8192,
		Temperature:         0.5,
		Weights: map[string]float64{
			"statistics":     0.9,
			"cite_sources":   0.85,
			"answer_first":   0.8,
			"structure":      0.75,
			"authoritative":  0.7,
			"fluency":        0.65,
		},
	},
	models.EngineDeepSeek: {
		Engine:              models.EngineDeepSeek,
		PreferredStrategies: []models.StrategyType{models.StrategyTechnicalTerms, models.StrategyStructure, models.StrategyCiteSources},
		MaxTokens:           8192,
		Temperature:         0.4,
		Weights: map[string]float64{
			"technical_terms": 0.9,
			"structure":       0.85,
			"cite_sources":    0.8,
			"statistics":      0.75,
			"answer_first":    0.7,
			"unique_words":    0.65,
		},
	},
	models.EngineKimi: {
		Engine:              models.EngineKimi,
		PreferredStrategies: []models.StrategyType{models.StrategyCiteSources, models.StrategyFluency, models.StrategyAnswerFirst},
		MaxTokens:           8192,
		Temperature:         0.6,
		Weights: map[string]float64{
			"cite_sources":   0.9,
			"fluency":        0.85,
			"answer_first":   0.8,
			"authoritative":  0.75,
			"structure":      0.7,
			"statistics":     0.65,
		},
	},
	models.EngineWenxin: {
		Engine:              models.EngineWenxin,
		PreferredStrategies: []models.StrategyType{models.StrategyAuthoritative, models.StrategyStatistics, models.StrategyStructure},
		MaxTokens:           4096,
		Temperature:         0.6,
		Weights: map[string]float64{
			"authoritative":  0.9,
			"statistics":     0.85,
			"structure":      0.8,
			"cite_sources":   0.75,
			"answer_first":   0.7,
			"fluency":        0.65,
		},
	},
	models.EngineDoubao: {
		Engine:              models.EngineDoubao,
		PreferredStrategies: []models.StrategyType{models.StrategyFluency, models.StrategyStructure, models.StrategyAnswerFirst},
		MaxTokens:           4096,
		Temperature:         0.6,
		Weights: map[string]float64{
			"fluency":        0.9,
			"structure":      0.85,
			"answer_first":   0.8,
			"cite_sources":   0.75,
			"statistics":     0.7,
			"authoritative":  0.65,
		},
	},
	models.EngineXiaomi: {
		Engine:              models.EngineXiaomi,
		PreferredStrategies: []models.StrategyType{models.StrategyEasyUnderstand, models.StrategyFluency, models.StrategyStructure},
		MaxTokens:           4096,
		Temperature:         0.6,
		Weights: map[string]float64{
			"easy_understand": 0.9,
			"fluency":         0.85,
			"structure":       0.8,
			"answer_first":    0.75,
			"cite_sources":    0.7,
			"statistics":      0.65,
		},
	},
	models.EngineXunfei: {
		Engine:              models.EngineXunfei,
		PreferredStrategies: []models.StrategyType{models.StrategyCiteSources, models.StrategyAuthoritative, models.StrategyStructure},
		MaxTokens:           4096,
		Temperature:         0.5,
		Weights: map[string]float64{
			"cite_sources":   0.9,
			"authoritative":  0.85,
			"structure":      0.8,
			"statistics":     0.75,
			"answer_first":   0.7,
			"fluency":        0.65,
		},
	},
	models.EngineYuanbao: {
		Engine:              models.EngineYuanbao,
		PreferredStrategies: []models.StrategyType{models.StrategyAuthoritative, models.StrategyCiteSources, models.StrategyStructure},
		MaxTokens:           4096,
		Temperature:         0.5,
		Weights: map[string]float64{
			"authoritative":  0.9,
			"cite_sources":   0.85,
			"structure":      0.8,
			"statistics":     0.75,
			"answer_first":   0.7,
			"fluency":        0.65,
		},
	},
}

// DomainStrategyRecommendation 基于领域类型的策略推荐。
//
// 来自 Princeton 论文关键洞察：严肃话题靠引用、软性话题靠语气、知识话题靠数据。
var DomainStrategyRecommendation = map[models.DomainType][]models.StrategyType{
	models.DomainSerious: {
		models.StrategyCiteSources, models.StrategyStatistics, models.StrategyAuthoritative,
	},
	models.DomainSoft: {
		models.StrategyAuthoritative, models.StrategyFluency, models.StrategyQuotation,
	},
	models.DomainKnowledge: {
		models.StrategyStatistics, models.StrategyTechnicalTerms, models.StrategyUniqueWords,
	},
}

// AllStrategies 全部可用策略（Princeton 9 策略 + 工程化扩展）。
var AllStrategies = []models.StrategyType{
	models.StrategyCiteSources,
	models.StrategyStatistics,
	models.StrategyAuthoritative,
	models.StrategyQuotation,
	models.StrategyFluency,
	models.StrategyEasyUnderstand,
	models.StrategyKeyword,
	models.StrategyUniqueWords,
	models.StrategyTechnicalTerms,
	models.StrategyStructure,
	models.StrategyFAQ,
	models.StrategySchema,
	models.StrategyAnswerFirst,
}

// StrategyEffectiveness 策略效果系数（来自 Princeton 论文实验数据）。
//
// 值表示该策略对可见度的提升幅度。
var StrategyEffectiveness = map[models.StrategyType]float64{
	models.StrategyQuotation:      0.41, // +41%
	models.StrategyStatistics:     0.33, // +33%
	models.StrategyFluency:        0.29, // +29%
	models.StrategyCiteSources:    0.27, // +27%
	models.StrategyAuthoritative:  0.25,
	models.StrategyTechnicalTerms: 0.20,
	models.StrategyUniqueWords:    0.18,
	models.StrategyStructure:      0.22,
	models.StrategyAnswerFirst:    0.24,
	models.StrategyFAQ:            0.20,
	models.StrategySchema:         0.30, // 结构化数据提升 LLM 提取准确率
	models.StrategyEasyUnderstand: 0.15,
	models.StrategyKeyword:        0.10,
}

// GetPreset 获取指定引擎的预设。
func GetPreset(engine models.EngineType) (models.AIEnginePreset, bool) {
	p, ok := AIEnginePresets[engine]
	return p, ok
}

// RecommendStrategies 根据领域类型和目标引擎推荐策略组合。
//
// 优先取领域推荐与引擎偏好的交集；若无交集则使用领域推荐。
func RecommendStrategies(domain models.DomainType, engines []models.EngineType) []models.StrategyType {
	domainStrats := DomainStrategyRecommendation[domain]
	if len(domainStrats) == 0 {
		return defaultStrategies()
	}
	// 聚合引擎偏好策略
	enginePref := map[models.StrategyType]bool{}
	for _, e := range engines {
		if p, ok := GetPreset(e); ok {
			for _, s := range p.PreferredStrategies {
				enginePref[s] = true
			}
		}
	}
	// 领域推荐优先，与引擎偏好交集排前
	var primary, secondary []models.StrategyType
	for _, s := range domainStrats {
		if enginePref[s] {
			primary = append(primary, s)
		} else {
			secondary = append(secondary, s)
		}
	}
	result := append(primary, secondary...)
	// 补充结构化基础策略
	result = append(result, models.StrategyStructure, models.StrategySchema, models.StrategyAnswerFirst)
	return dedupStrategies(result)
}

func defaultStrategies() []models.StrategyType {
	return []models.StrategyType{
		models.StrategyCiteSources, models.StrategyStatistics, models.StrategyStructure,
		models.StrategyAnswerFirst, models.StrategySchema,
	}
}

func dedupStrategies(strats []models.StrategyType) []models.StrategyType {
	seen := map[models.StrategyType]bool{}
	var result []models.StrategyType
	for _, s := range strats {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// ===== 环境变量与适配器统一配置 =====

// engineEnvKeys 引擎 → API Key 环境变量名（全局唯一数据源，CLI 与 server 共用）。
var engineEnvKeys = map[models.EngineType]string{
	models.EngineChatGPT:    "GEO_OPENAI_KEY",
	models.EnginePerplexity: "GEO_PERPLEXITY_KEY",
	models.EngineGemini:     "GEO_GEMINI_KEY",
	models.EngineClaude:     "GEO_CLAUDE_KEY",
	models.EngineQwen:       "GEO_QWEN_KEY",
	models.EngineGLM:        "GEO_GLM_KEY",
	models.EngineDeepSeek:   "GEO_DEEPSEEK_KEY",
	models.EngineKimi:       "GEO_KIMI_KEY",
	models.EngineWenxin:     "GEO_WENXIN_KEY",
	models.EngineDoubao:     "GEO_DOUBAO_KEY",
	models.EngineXiaomi:     "GEO_XIAOMI_KEY",
	models.EngineXunfei:     "GEO_XUNFEI_KEY",
	models.EngineYuanbao:    "GEO_YUANBAO_KEY",
}

// EngineEnvKey 获取指定引擎的 API Key 环境变量名；不存在返回空串。
func EngineEnvKey(engine models.EngineType) string { return engineEnvKeys[engine] }

// AllEngineEnvKeys 返回所有「引擎 → 环境变量名」映射的副本。
func AllEngineEnvKeys() map[models.EngineType]string {
	m := make(map[models.EngineType]string, len(engineEnvKeys))
	for k, v := range engineEnvKeys {
		m[k] = v
	}
	return m
}

// BrandAdaptersFromEnv 从环境变量批量构建品牌审计用适配器映射。
//
// 每引擎独立 API Key 环境变量；未配置 key 的引擎仍会创建适配器（返回模拟响应）。
// errs 返回创建失败的引擎 → error 映射（可忽略或打印警告），不影响其他引擎。
func BrandAdaptersFromEnv() (adapters map[models.EngineType]adapter.Adapter, errs map[models.EngineType]error) {
	adapters = map[models.EngineType]adapter.Adapter{}
	errs = map[models.EngineType]error{}
	for engine, envKey := range engineEnvKeys {
		key := os.Getenv(envKey)
		a, err := adapter.NewAdapter(engine, adapter.Config{APIKey: key})
		if err != nil {
			errs[engine] = err
			continue
		}
		adapters[engine] = a
	}
	if len(errs) == 0 {
		errs = nil
	}
	return adapters, errs
}

// Env 读取字符串环境变量，未设置时返回 fallback。
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
