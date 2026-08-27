// Package report 提供 Markdown 格式的品牌可见度审计报告生成。
package report

import (
	"fmt"
	"strings"
	"time"

	"my-geo/internal/brand"
)

// GenerateMarkdown 从审计报告生成 Markdown 格式报告。
func GenerateMarkdown(vr *brand.VisibilityReport) string {
	if vr == nil {
		return "# 空报告\n\n无数据。"
	}
	var b strings.Builder

	// 封面
	b.WriteString(fmt.Sprintf("# %s — 品牌可见度报告\n\n", vr.BrandName))
	b.WriteString(fmt.Sprintf("- **BVS 评分**: %.1f / 100（%s）\n", vr.Score, vr.Grade))
	b.WriteString(fmt.Sprintf("- **梯队**: %s\n", vr.Tier))
	if vr.Industry != "" {
		b.WriteString(fmt.Sprintf("- **行业**: %s\n", vr.Industry))
	}
	b.WriteString(fmt.Sprintf("- **生成时间**: %s\n\n", vr.GeneratedAt.Format(time.RFC3339)))

	// 评分明细
	b.WriteString("## 评分明细\n\n")
	sb := vr.ScoreBreakdown
	dims := []struct {
		name string
		val  float64
	}{
		{"ContentQuality", sb.ContentQuality},
		{"TechnicalSEO", sb.TechnicalSEO},
		{"OnPageSEO", sb.OnPageSEO},
		{"Schema", sb.Schema},
		{"Performance", sb.Performance},
		{"AIReadiness", sb.AIReadiness},
		{"ImageOptimization", sb.ImageOptimization},
	}
	b.WriteString("| 维度 | 得分 |\n|------|------|\n")
	for _, d := range dims {
		if d.val > 0 {
			b.WriteString(fmt.Sprintf("| %s | %.1f |\n", d.name, d.val))
		}
	}

	// 各引擎表现
	if len(vr.EngineStats) > 0 {
		b.WriteString("\n## 各引擎表现\n\n")
		b.WriteString("| 引擎 | 提及率 | 引用率 | SOV | 正面率 | 已配置 |\n")
		b.WriteString("|------|--------|--------|-----|--------|--------|\n")
		for _, e := range vr.EngineStats {
			configured := "否"
			if e.Configured {
				configured = "是"
			}
			b.WriteString(fmt.Sprintf("| %s | %.1f%% | %.1f%% | %.1f%% | %.1f%% | %s |\n",
				e.Engine, e.MentionRate*100, e.CitationRate*100, e.ShareOfVoice*100, e.PositiveRate*100, configured))
		}
	}

	// 竞品声量
	if len(vr.CompetitorSOV) > 0 {
		b.WriteString("\n## 竞品声量份额\n\n")
		b.WriteString("| 竞品 | 声量 |\n|------|------|\n")
		for _, c := range vr.CompetitorSOV {
			b.WriteString(fmt.Sprintf("| %s | %.1f%% |\n", c.Name, c.SOV*100))
		}
	}

	// 内容缺口
	if len(vr.ContentGaps) > 0 {
		b.WriteString("\n## 内容缺口\n\n")
		for i, gap := range vr.ContentGaps {
			b.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, gap.Prompt, gap.Engine))
			if len(gap.CompetitorNamed) > 0 {
				b.WriteString(fmt.Sprintf("   - 被提及竞品: %s\n", strings.Join(gap.CompetitorNamed, ", ")))
			}
			if gap.SuggestedTopic != "" {
				b.WriteString(fmt.Sprintf("   - 建议话题: %s\n", gap.SuggestedTopic))
			}
		}
	}

	// 运营行动建议
	if len(vr.Actions) > 0 {
		b.WriteString("\n## 运营行动建议\n\n")
		for _, action := range vr.Actions {
			b.WriteString(fmt.Sprintf("### [%s] %s\n\n", strings.ToUpper(action.Priority), action.Title))
			b.WriteString(fmt.Sprintf("%s\n\n", action.Detail))
			if len(action.Tasks) > 0 {
				b.WriteString("**具体任务:**\n")
				for _, task := range action.Tasks {
					b.WriteString(fmt.Sprintf("- %s\n", task))
				}
				b.WriteString("\n")
			}
			if action.ExpectedImpact != "" {
				b.WriteString(fmt.Sprintf("**预期效果:** %s\n\n", action.ExpectedImpact))
			}
		}
	}

	// 健康问题
	if len(vr.SeverityIssues) > 0 {
		b.WriteString("## 健康问题\n\n")
		b.WriteString("| 维度 | 得分 | 严重级别 | 说明 |\n")
		b.WriteString("|------|------|----------|------|\n")
		for _, issue := range vr.SeverityIssues {
			b.WriteString(fmt.Sprintf("| %s | %.1f | %s | %s |\n",
				issue.Dimension, issue.Score, issue.Severity, issue.Impact))
		}
	}

	// 页脚
	b.WriteString("\n---\n\n")
	b.WriteString(fmt.Sprintf("*报告由 MyGEO 于 %s 生成 | AI 生成内容，仅供参考*\n",
		time.Now().Format("2006-01-02 15:04")))

	return b.String()
}
