// Package main 是 GEO Go SDK 使用示例。
//
// 演示如何通过 pkg/geo 公开 API 进行内容评分、分析与优化。
// 运行：go run ./examples
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"my-geo/internal/models"
	"my-geo/pkg/geo"
)

func main() {
	// 创建 GEO 引擎。
	// 未配置 LLM 时仅执行规则化预处理 + 评分 + 建议；
	// 如需 LLM 改写，传入 OpenAI 兼容 Key：
	//   geo.New(geo.WithOpenAI("sk-xxx", "https://api.openai.com", "gpt-4o-mini"))
	engine := geo.New()

	content := `人工智能是计算机科学的一个分支，致力于让机器具备类人智能。

深度学习是当前最主流的AI方法。神经网络是其基础架构。现在很多公司都在用AI技术。`

	// 1. 评估原始内容 GEO 评分
	score, breakdowns := engine.Score(content)
	fmt.Printf("=== 优化前评分: %.1f/100 ===\n", score)
	for _, b := range breakdowns {
		fmt.Printf("  %-18s %.1f/%.0f\n", b.Category, b.Score, b.MaxScore)
	}

	// 2. 执行 GEO 优化（自动推荐策略）
	resp, err := engine.Optimize(context.Background(), &models.OptimizationRequest{
		Content:       content,
		Title:         "人工智能入门指南",
		URL:           "https://example.com/ai-guide",
		DomainType:    models.DomainKnowledge,
		TargetEngines: []models.EngineType{models.EngineChatGPT, models.EnginePerplexity},
		Enterprise: &models.Enterprise{
			CompanyName: "示例科技",
			Description: "专注人工智能技术研究与应用",
		},
	})
	if err != nil {
		fmt.Println("优化失败:", err)
		return
	}

	// 3. 输出优化结果
	fmt.Printf("\n=== 优化后评分: %.1f/100 (提升 %.1f) ===\n", resp.ScoreAfter, resp.ScoreAfter-resp.ScoreBefore)
	fmt.Printf("\n--- 优化后内容 ---\n%s\n", resp.OptimizedContent)

	fmt.Println("\n--- 应用的策略 ---")
	for _, s := range resp.AppliedStrategies {
		status := "✗"
		if s.Applied {
			status = "✓"
		}
		fmt.Printf("  %s %-16s +%d%%\n", status, s.Strategy, int(s.Improvement*100))
	}

	fmt.Println("\n--- 可见度预估 ---")
	visJSON, _ := json.MarshalIndent(resp.GeoScore, "", "  ")
	fmt.Println(string(visJSON))

	fmt.Println("\n--- 优化建议 ---")
	for _, r := range resp.Recommendations {
		fmt.Printf("  [%s] %s: %s\n", r.Priority, r.Category, r.Message)
	}

	fmt.Println("\n--- 生成的 llms.txt ---")
	if resp.GeneratedAssets != nil {
		fmt.Println(resp.GeneratedAssets.LLMsTxt)
	}
}
