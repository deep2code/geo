// Package config 多语言/多市场扩展：支持更多垂直领域和语言的评测集。
//
// 扩展 eval 包的能力：
//   - 多语言评测集管理
//   - 垂直领域预设（电商、教育、医疗、金融等）
//   - 本地化策略调整
package config

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed benchmarks/*.json
var benchmarkFS embed.FS

// BenchmarkMeta 评测集元数据。
type BenchmarkMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Language    string   `json:"language"`    // zh/en/ja/ko 等
	Domain      string   `json:"domain"`      // ecommerce/education/health/finance 等
	Region      string   `json:"region"`      // cn/us/eu/jp 等
	CaseCount   int      `json:"case_count"`
	Description string   `json:"description,omitempty"`
}

// AvailableBenchmark 可用的评测集列表。
var AvailableBenchmark = []BenchmarkMeta{
	{
		ID:       "zh-geo-sample",
		Name:     "中文 GEO 示例评测集",
		Language: "zh",
		Domain:   "general",
		Region:   "cn",
	},
	{
		ID:       "zh-ecommerce",
		Name:     "中文电商 GEO 评测集",
		Language: "zh",
		Domain:   "ecommerce",
		Region:   "cn",
	},
	{
		ID:       "en-general",
		Name:     "English General GEO Benchmark",
		Language: "en",
		Domain:   "general",
		Region:   "us",
	},
	{
		ID:       "en-ecommerce",
		Name:     "English E-commerce GEO Benchmark",
		Language: "en",
		Domain:   "ecommerce",
		Region:   "us",
	},
	{
		ID:       "en-education",
		Name:     "English Education GEO Benchmark",
		Language: "en",
		Domain:   "education",
		Region:   "us",
	},
	{
		ID:       "en-health",
		Name:     "English Health GEO Benchmark",
		Language: "en",
		Domain:   "health",
		Region:   "us",
	},
	{
		ID:       "en-finance",
		Name:     "English Finance GEO Benchmark",
		Language: "en",
		Domain:   "finance",
		Region:   "us",
	},
}

// GetBenchmarkList 获取可用评测集列表。
func GetBenchmarkList() []BenchmarkMeta {
	return AvailableBenchmark
}

// ListBenchmarks 列出可用的评测集文件。
func ListBenchmarks() ([]BenchmarkMeta, error) {
	entries, err := benchmarkFS.ReadDir("benchmarks")
	if err != nil {
		return nil, fmt.Errorf("读取评测集目录失败: %w", err)
	}

	var benchmarks []BenchmarkMeta
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 尝试读取并解析 JSON
		data, err := benchmarkFS.ReadFile("benchmarks/" + entry.Name())
		if err != nil {
			continue
		}
		var meta BenchmarkMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		benchmarks = append(benchmarks, meta)
	}
	return benchmarks, nil
}

// DomainStrategyPreset 垂直领域策略预设。
type DomainStrategyPreset struct {
	Domain            string   `json:"domain"`
	Name              string   `json:"name"`
	PreferredStrategies []string `json:"preferred_strategies"`
	KeywordsToAvoid   []string `json:"keywords_to_avoid"`
	ContentLengthMin  int      `json:"content_length_min"`
	ContentLengthMax  int      `json:"content_length_max"`
}

// DomainPresets 垂直领域预设配置。
var DomainPresets = map[string]DomainStrategyPreset{
	"ecommerce": {
		Domain:            "ecommerce",
		Name:              "电商",
		PreferredStrategies: []string{"statistics", "cite_sources", "faq", "structure"},
		KeywordsToAvoid:   []string{"购买", "下单", "优惠"},
		ContentLengthMin:  300,
		ContentLengthMax:  1500,
	},
	"education": {
		Domain:            "education",
		Name:              "教育",
		PreferredStrategies: []string{"authoritative", "cite_sources", "quotation", "structure"},
		KeywordsToAvoid:   []string{"报名", "课程价格"},
		ContentLengthMin:  500,
		ContentLengthMax:  2000,
	},
	"health": {
		Domain:            "health",
		Name:              "医疗健康",
		PreferredStrategies: []string{"authoritative", "cite_sources", "statistics", "easy_understand"},
		KeywordsToAvoid:   []string{"治疗", "药物", "诊断"},
		ContentLengthMin:  500,
		ContentLengthMax:  2500,
	},
	"finance": {
		Domain:            "finance",
		Name:              "金融",
		PreferredStrategies: []string{"authoritative", "statistics", "cite_sources", "structure"},
		KeywordsToAvoid:   []string{"投资", "收益", "风险"},
		ContentLengthMin:  400,
		ContentLengthMax:  2000,
	},
}

// GetDomainPreset 获取垂直领域预设。
func GetDomainPreset(domain string) (*DomainStrategyPreset, bool) {
	preset, ok := DomainPresets[domain]
	return &preset, ok
}

// ListDomainPresets 列出所有垂直领域预设。
func ListDomainPresets() []DomainStrategyPreset {
	presets := make([]DomainStrategyPreset, 0, len(DomainPresets))
	for _, p := range DomainPresets {
		presets = append(presets, p)
	}
	return presets
}
