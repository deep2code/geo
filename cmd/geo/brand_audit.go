package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"my-geo/internal/brand"
	"my-geo/internal/config"
)

// newBrandAuditCmd 创建品牌可见度审计命令。
//
// 参考 AiCMO / oneglanse 的品牌监控能力，对品牌在 AI 引擎中的
// 提及/引用/情感/位置/竞品/幽灵引用进行检测，输出 BVS 评分与运营报告。
func buildBrandAuditCmd(name string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name,
		Short: "品牌可见度审计：评分 + 报告 + 运营行动建议",
		Long: `品牌可见度审计（Brand Visibility Audit）。

检测品牌在 AI 搜索引擎中的可见度，生成 BVS 评分与运营行动报告，
指导运营人员下一步工作方向（内容缺口、引擎补强、实体建设等）。

输入品牌画像 JSON（-f 文件 / --profile / stdin），示例：
{
  "name": "Acme",
  "aliases": ["Acme Inc"],
  "domain": "acme.com",
  "products": ["Acme CRM"],
  "category": "SaaS",
  "prompts": ["最好的CRM工具", "推荐项目管理软件"],
  "competitors": [{"name": "HubSpot", "domain": "hubspot.com"}],
  "target_engines": ["glm", "deepseek", "chatgpt"]
}

引擎 API Key 通过环境变量配置（每引擎独立）：
  GEO_GLM_KEY / GEO_DEEPSEEK_KEY / GEO_KIMI_KEY / GEO_QWEN_KEY
  GEO_OPENAI_KEY / GEO_PERPLEXITY_KEY / GEO_GEMINI_KEY / GEO_CLAUDE_KEY
  GEO_DOUBAO_KEY / GEO_XIAOMI_KEY / GEO_XUNFEI_KEY / GEO_YUANBAO_KEY
未配置 key 的引擎返回模拟响应，不影响评分流程运行。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, err := readBrandProfile(cmd)
			if err != nil {
				return err
			}
			engines, adapterErrs := config.BrandAdaptersFromEnv()
			if len(engines) == 0 {
				return fmt.Errorf("未创建任何引擎适配器")
			}
			for eng, e := range adapterErrs {
				fmt.Fprintf(os.Stderr, "  [警告] %s 适配器创建失败: %v\n", eng, e)
			}

			be := brand.New(brand.WithAdapters(engines))
			ctx := context.Background()
			report, err := be.Audit(ctx, *profile)
			if err != nil {
				return err
			}

			out, _ := cmd.Flags().GetString("output")
			if out != "" {
				data, _ := json.MarshalIndent(report, "", "  ")
				return os.WriteFile(out, data, 0644)
			}

			printBrandReport(report)
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "品牌画像 JSON 文件路径")
	cmd.Flags().String("profile", "", "品牌画像 JSON 字符串")
	cmd.Flags().StringP("output", "o", "", "报告输出 JSON 文件路径（不设则打印摘要）")
	return cmd
}

// readBrandProfile 读取品牌画像。
func readBrandProfile(cmd *cobra.Command) (*brand.BrandProfile, error) {
	file, _ := cmd.Flags().GetString("file")
	profileStr, _ := cmd.Flags().GetString("profile")

	var raw []byte
	if profileStr != "" {
		raw = []byte(profileStr)
	} else if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("读取文件失败: %w", err)
		}
		raw = b
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			b, err := readAll(os.Stdin)
			if err != nil {
				return nil, err
			}
			raw = b
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("请通过 -f <文件>、--profile <JSON> 或管道提供品牌画像")
	}

	var profile brand.BrandProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, fmt.Errorf("解析品牌画像 JSON 失败: %w", err)
	}
	if profile.Name == "" {
		return nil, fmt.Errorf("品牌画像缺少 name 字段")
	}
	if len(profile.Prompts) == 0 {
		return nil, fmt.Errorf("品牌画像缺少 prompts 字段")
	}
	return &profile, nil
}

// readAll 简易读取全部内容。
func readAll(f *os.File) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

// printBrandReport 打印品牌可见度报告摘要。
func printBrandReport(r *brand.VisibilityReport) {
	divider := strings.Repeat("═", 50)
	fmt.Println(divider)
	fmt.Printf(" 品牌可见度报告：%s\n", r.BrandName)
	if r.Category != "" {
		fmt.Printf(" 品类：%s\n", r.Category)
	}
	fmt.Println(divider)
	fmt.Printf(" BVS 评分：%.1f / 100   等级：%s   梯队：%s\n", r.Score, r.Grade, tierLabel(r.Tier))
	fmt.Println()
	fmt.Println("【6 维评分明细】")
	bd := r.ScoreBreakdown
	fmt.Printf("  提及率     %.1f\n", bd.MentionRate)
	fmt.Printf("  引用率     %.1f\n", bd.CitationRate)
	fmt.Printf("  声量份额   %.1f\n", bd.ShareOfVoice)
	fmt.Printf("  引用位置   %.1f\n", bd.CitationPosition)
	fmt.Printf("  情感       %.1f\n", bd.Sentiment)
	fmt.Printf("  实体识别   %.1f\n", bd.EntityRecognition)
	fmt.Println()
	fmt.Println("【各引擎表现】")
	for _, st := range r.EngineStats {
		cfgMark := "（模拟）"
		if st.Configured {
			cfgMark = "（已配置）"
		}
		fmt.Printf("  %-12s 提及率 %5.1f%%  引用率 %5.1f%%  SOV %5.1f%%  正面 %5.1f%%  %s\n",
			st.Engine, st.MentionRate, st.CitationRate, st.ShareOfVoice, st.PositiveRate, cfgMark)
	}
	if len(r.ContentGaps) > 0 {
		fmt.Println()
		fmt.Printf("【内容缺口】（%d 个高机会查询）\n", len(r.ContentGaps))
		for i, g := range r.ContentGaps {
			if i >= 5 {
				fmt.Printf("  ... 还有 %d 个\n", len(r.ContentGaps)-5)
				break
			}
			fmt.Printf("  • 「%s」竞品：%s\n", g.Prompt, strings.Join(g.CompetitorNamed, "、"))
		}
	}
	if len(r.CompetitorSOV) > 0 {
		fmt.Println()
		fmt.Println("【竞品声量份额】")
		for _, c := range r.CompetitorSOV {
			fmt.Printf("  %-16s 提及 %d 次  SOV %5.1f%%\n", c.Name, c.MentionCount, c.SOV)
		}
	}
	fmt.Println()
	fmt.Println("【运营行动建议】")
	for _, a := range r.Actions {
		fmt.Printf("  [%s][%s] %s\n", prioLabel(a.Priority), a.Category, a.Title)
		for _, t := range a.Tasks {
			fmt.Printf("      → %s\n", t)
		}
		if a.ExpectedImpact != "" {
			fmt.Printf("      预期：%s\n", a.ExpectedImpact)
		}
	}
	fmt.Println(divider)
}

func tierLabel(tier string) string {
	switch tier {
	case "household":
		return "头部"
	case "midmarket":
		return "中坚"
	default:
		return "长尾"
	}
}

func prioLabel(p string) string {
	switch p {
	case "high":
		return "高"
	case "medium":
		return "中"
	default:
		return "低"
	}
}
