package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"my-geo/internal/brand"
	"my-geo/internal/brand/discover"
	"my-geo/internal/brand/knowledge"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/config"
)

// newDiscoverCmd 创建 discover 子命令：关键词→公司推断→GEO 报告。
//
// 用户输入一个关键词（如 "短视频"），系统自动：
//  1. 搜索离线工商库 + SinoFacts 知识库，找到匹配的公司
//  2. 多个结果时让用户选择
//  3. 自动生成品牌画像并执行 GEO 审计
func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "关键词→公司推断→自动 GEO 报告",
		Long: `关键词发现与自动 GEO 报告生成。

输入一个关键词（行业/产品/公司名），系统自动搜索匹配的公司，
生成品牌画像并输出完整的 GEO 报告（品牌可见度 + AI 就绪度 + 优化建议）。

示例：
  # 按关键词搜索公司
  geo discover "短视频"

  # 直接指定公司名（跳过选择步骤）
  geo discover "腾讯" --direct

  # 指定行业查询词
  geo discover "在线教育" --engines glm,deepseek`,
		RunE: runDiscover,
	}
	cmd.Flags().StringArray("engines", nil, "目标 AI 引擎（如 glm,deepseek,chatgpt）")
	cmd.Flags().Bool("direct", false, "直接使用关键词作为公司名搜索（跳过选择）")
	cmd.Flags().StringP("output", "o", "", "报告输出 JSON 文件路径（不设则打印摘要）")
	cmd.Flags().String("db", "", "离线工商库路径（默认 ~/.local/share/geo/geo_offline_companies.db）")
	return cmd
}

func runDiscover(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("请提供搜索关键词，例如: geo discover \"短视频\"")
	}
	keyword := strings.TrimSpace(args[0])
	if keyword == "" {
		return fmt.Errorf("关键词不能为空")
	}

	direct, _ := cmd.Flags().GetBool("direct")
	outputFile, _ := cmd.Flags().GetString("output")
	dbPath, _ := cmd.Flags().GetString("db")

	// 1. 打开离线工商库（可选）
	var offlineDB offlinedb.DB
	if d, err := offlinedb.Open(dbPath); err == nil {
		offlineDB = d
		defer offlineDB.Close()
	}

	// 2. 加载知识库
	var kb *knowledge.Knowledge
	if k, err := knowledge.Load(); err == nil {
		kb = k
	}

	// 3. 搜索匹配公司
	fmt.Printf("\n🔍 搜索关键词「%s」...\n\n", keyword)
	result, err := discover.Discover(context.Background(), keyword, offlineDB, kb)
	if err != nil {
		return err
	}

	if len(result.Candidates) == 0 {
		fmt.Println("❌ 未找到匹配的公司。")
		fmt.Println("建议：")
		fmt.Println("  1. 导入工商数据：geo brand-db import-file -d /path/to/json")
		fmt.Println("  2. 尝试更具体的关键词，如公司全称或品牌名")
		return nil
	}

	// 4. 选择公司
	var selected *discover.Candidate
	if direct && len(result.Candidates) > 0 {
		selected = &result.Candidates[0]
		fmt.Printf("✅ 直接选择第一个匹配：%s\n\n", selected.Name)
	} else if len(result.Candidates) == 1 {
		selected = &result.Candidates[0]
		fmt.Printf("✅ 唯一匹配：%s\n\n", selected.Name)
	} else {
		fmt.Printf("找到 %d 个匹配结果：\n\n", len(result.Candidates))
		for i, c := range result.Candidates {
			sourceTag := "工商库"
			if c.Source == "sinofacts" {
				sourceTag = "知识库"
			}
			fmt.Printf("  [%d] %s  (%s", i+1, c.Name, sourceTag)
			if c.Province != "" {
				fmt.Printf(", %s", c.Province)
			}
			if c.Industry != "" {
				fmt.Printf(", %s", c.Industry)
			}
			fmt.Printf(", 匹配%.0f%%", c.Score)
			fmt.Println(")")
			if c.BusinessScope != "" && len(c.BusinessScope) > 0 {
				scope := c.BusinessScope
				if len(scope) > 80 {
					scope = scope[:80] + "..."
				}
				fmt.Printf("      经营范围: %s\n", scope)
			}
			if c.Summary != "" {
				fmt.Printf("      简介: %s\n", c.Summary)
			}
		}
		fmt.Print("\n请选择公司编号 (1-" + strconv.Itoa(len(result.Candidates)) + "): ")

		reader := os.Stdin
		var input string
		fmt.Fscanln(reader, &input)
		idx, err := strconv.Atoi(strings.TrimSpace(input))
		if err != nil || idx < 1 || idx > len(result.Candidates) {
			return fmt.Errorf("无效的选择: %s", input)
		}
		selected = &result.Candidates[idx-1]
		fmt.Printf("\n✅ 已选择：%s\n\n", selected.Name)
	}

	// 5. 构建品牌引擎
	engines, adapterErrs := config.BrandAdaptersFromEnv()
	if len(engines) == 0 {
		fmt.Println("⚠️  未配置任何 AI 引擎 API Key，将跳过品牌可见度审计。")
		fmt.Println("   配置方法：export GEO_GLM_KEY=xxx 或 export GEO_DEEPSEEK_KEY=xxx")
	} else {
		for eng, e := range adapterErrs {
			fmt.Fprintf(os.Stderr, "  [警告] %s 适配器创建失败: %v\n", eng, e)
		}
	}

	var brandEngine *brand.Engine
	if len(engines) > 0 {
		opts := []brand.Option{brand.WithAdapters(engines)}
		// 注入离线工商库和知识库
		if offlineDB != nil {
			opts = append(opts, brand.WithOfflineDB(offlineDB))
		}
		brandEngine = brand.New(opts...)
	}

	// 6. 生成 GEO 报告
	fmt.Println("📊 正在生成 GEO 报告...")
	report, err := discover.GenerateReport(context.Background(), selected, keyword, brandEngine)
	if err != nil {
		return fmt.Errorf("生成报告失败: %w", err)
	}

	// 7. 输出
	if outputFile != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(outputFile, data, 0644); err != nil {
			return err
		}
		fmt.Printf("✅ 报告已保存到 %s\n", outputFile)
	}

	printDiscoverReport(report)
	return nil
}

func printDiscoverReport(report *discover.GEOReport) {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("  GEO 报告 — %s\n", report.CompanyName)
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// 品牌画像摘要
	fmt.Println("【品牌画像】")
	fmt.Printf("  品牌名: %s\n", report.Profile.Name)
	if len(report.Profile.Aliases) > 0 {
		fmt.Printf("  别名: %s\n", strings.Join(report.Profile.Aliases, ", "))
	}
	if report.Profile.Domain != "" {
		fmt.Printf("  官网: %s\n", report.Profile.Domain)
	}
	if report.Profile.Industry != "" {
		fmt.Printf("  行业: %s\n", report.Profile.Industry)
	}
	if report.Profile.Category != "" {
		fmt.Printf("  品类: %s\n", report.Profile.Category)
	}
	if len(report.Profile.Products) > 0 {
		fmt.Printf("  产品: %s\n", strings.Join(report.Profile.Products, ", "))
	}
	fmt.Printf("  行业类型: %s\n", report.Vertical)
	fmt.Printf("  查询词 (%d 个):\n", len(report.Profile.Prompts))
	for i, p := range report.Profile.Prompts {
		fmt.Printf("    %d. %s\n", i+1, p)
	}
	fmt.Println()

	// 品牌可见度审计
	if report.BrandReport != nil {
		fmt.Println("【品牌可见度审计】")
		fmt.Printf("  BVS 评分: %.1f/100  等级: %s\n", report.BrandReport.Score, report.BrandReport.Grade)
		if report.BrandReport.Tier != "" {
			fmt.Printf("  梯队: %s\n", report.BrandReport.Tier)
		}
		if len(report.BrandReport.EngineStats) > 0 {
			fmt.Println("  引擎统计:")
			for _, es := range report.BrandReport.EngineStats {
				fmt.Printf("    %-12s 提及率 %.0f%%  引用率 %.0f%%  声量份额 %.0f%%\n",
					es.Engine, es.MentionRate, es.CitationRate, es.ShareOfVoice)
			}
		}
		if len(report.BrandReport.ContentGaps) > 0 {
			fmt.Println("  内容缺口:")
			for _, g := range report.BrandReport.ContentGaps {
				fmt.Printf("    ⚠ %s\n", g.SuggestedTopic)
			}
		}
		fmt.Println()
	} else {
		fmt.Println("【品牌可见度审计】未执行（未配置 AI 引擎 Key）")
		fmt.Println()
	}

	// AI 就绪度
	if report.ReadinessResult != nil {
		fmt.Println("【AI 就绪度】")
		fmt.Printf("  总分: %.1f/100  等级: %s\n", report.ReadinessResult.TotalScore, report.ReadinessResult.Grade)
		for _, c := range report.ReadinessResult.Checks {
			mark := "✓"
			if c.Status == "fail" {
				mark = "✗"
			} else if c.Status == "warn" {
				mark = "⚠"
			}
			fmt.Printf("    %s %-24s %3.0f分  %s\n", mark, c.Name, c.Score, c.Detail)
		}
		fmt.Println()
	}

	// 综合建议
	if len(report.Suggestions) > 0 {
		fmt.Println("【优化建议】")
		for i, s := range report.Suggestions {
			fmt.Printf("  %d. %s\n", i+1, s)
		}
		fmt.Println()
	}

	fmt.Println("═══════════════════════════════════════════════════════════")
}
