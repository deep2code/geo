// Package report 将品牌可见度审计报告渲染为自包含的 HTML（可打印为 PDF）。
//
// 设计原则：
//   - 纯标准库实现，不引入第三方 PDF/HTML 库
//   - 输出单文件 HTML（所有 CSS/SVG 内联，不依赖外部资源）
//   - A4 打印优化：@media print + page-break 控制
//   - SVG 图表（评分仪表盘 / 6 维柱状图 / 竞品声量份额）
//
// 使用方式：
//
//	html, err := report.GenerateHTML(visibilityReport)
//
// 配合 server 端 GET /api/v1/brand/report/html 接口返回，前端用浏览器打印功能导出 PDF。
package report

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"my-geo/internal/brand"
)

// scoreDim 评分明细中的单个维度（名称/得分/权重）。
type scoreDim struct {
	name   string
	value  float64
	weight int
}

// GenerateHTML 从审计报告生成自包含的 HTML 报告（可打印为 PDF）。
//
// 报告结构（A4 打印优化）：
//  1. 封面：品牌名 + BVS 评分仪表盘（SVG 环形图）+ 等级 + 梯队 + 生成时间
//  2. 评分明细：6 维评分柱状图（SVG）+ 权重标注
//  3. 各引擎表现：表格（引擎/提及率/引用率/SOV/正面率/状态）
//  4. 竞品声量份额：水平柱状图（SVG）
//  5. 内容缺口：列表（查询词 + 引擎 + 被提及竞品 + 建议话题）
//  6. 运营行动建议：按优先级分组（high/medium/low）
//  7. 页脚：报告生成时间 + 系统署名
func GenerateHTML(r *brand.VisibilityReport) (string, error) {
	if r == nil {
		return "", fmt.Errorf("report: 报告为空")
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	b.WriteString(html.EscapeString(r.BrandName + " · 品牌可见度审计报告"))
	b.WriteString(`</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, "PingFang SC", "Microsoft YaHei", "Helvetica Neue", sans-serif;
    color: #1f2937;
    background: #f3f4f6;
    line-height: 1.65;
    -webkit-font-smoothing: antialiased;
  }
  .page {
    max-width: 780px;
    margin: 0 auto;
    padding: 32px 44px 48px;
    background: #fff;
  }
  /* 工具栏（仅屏幕显示，打印时隐藏） */
  .toolbar {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    padding: 14px 44px;
    max-width: 780px;
    margin: 0 auto;
    background: #fff;
    border-bottom: 1px solid #e5e7eb;
    position: sticky;
    top: 0;
    z-index: 10;
  }
  .toolbar .title { font-size: 14px; font-weight: 600; color: #374151; }
  .toolbar .actions { display: flex; gap: 10px; }
  .btn {
    display: inline-block;
    padding: 7px 16px;
    font-size: 13px;
    border: 1px solid #d1d5db;
    border-radius: 6px;
    background: #fff;
    color: #374151;
    cursor: pointer;
    text-decoration: none;
  }
  .btn:hover { background: #f9fafb; }
  .btn-primary { background: #4f8cff; border-color: #4f8cff; color: #fff; }
  .btn-primary:hover { background: #3b7aed; }

  /* 封面 */
  .cover {
    page-break-after: always;
    min-height: 92vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 40px 0;
  }
  .cover .eyebrow {
    font-size: 12px;
    letter-spacing: 3px;
    color: #6b7280;
    text-transform: uppercase;
    margin-bottom: 12px;
  }
  .cover h1 {
    font-size: 38px;
    font-weight: 700;
    color: #111827;
    margin-bottom: 10px;
    letter-spacing: -0.5px;
  }
  .cover .badges { display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; margin-bottom: 30px; }
  .cover .badge {
    padding: 3px 12px;
    font-size: 12px;
    border-radius: 14px;
    background: #eef2ff;
    color: #4f8cff;
    font-weight: 600;
  }
  .cover .badge.gray { background: #f3f4f6; color: #6b7280; }
  .cover .gauge { margin: 10px 0 20px; }
  .cover .grade-row { display: flex; gap: 40px; align-items: center; justify-content: center; margin: 18px 0; }
  .cover .grade-block .label { font-size: 12px; color: #6b7280; margin-bottom: 4px; }
  .cover .grade-block .val { font-size: 24px; font-weight: 700; color: #4f8cff; }
  .cover .gen-time { margin-top: 28px; font-size: 12px; color: #6b7280; }
  .cover .company-card {
    margin-top: 22px;
    padding: 14px 20px;
    background: #f9fafb;
    border: 1px solid #e5e7eb;
    border-radius: 8px;
    font-size: 12px;
    color: #4b5563;
    max-width: 460px;
    text-align: left;
  }
  .cover .company-card .row { display: flex; gap: 8px; margin: 2px 0; }
  .cover .company-card .k { color: #6b7280; min-width: 70px; }

  /* 章节 */
  .section { margin-bottom: 32px; page-break-inside: avoid; }
  .section h2 {
    font-size: 17px;
    font-weight: 700;
    color: #111827;
    padding-bottom: 8px;
    margin-bottom: 16px;
    border-bottom: 2px solid #4f8cff;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .section h2 .num {
    display: inline-flex;
    width: 24px; height: 24px;
    align-items: center; justify-content: center;
    background: #4f8cff; color: #fff;
    border-radius: 6px;
    font-size: 13px;
  }

  /* 表格 */
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th {
    text-align: left;
    padding: 9px 10px;
    color: #6b7280;
    font-weight: 600;
    background: #f9fafb;
    border-bottom: 2px solid #e5e7eb;
  }
  td { padding: 9px 10px; border-bottom: 1px solid #f3f4f6; color: #374151; }
  tr:last-child td { border-bottom: none; }
  .tag { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 11px; font-weight: 600; }
  .tag.ok { background: #ecfdf5; color: #059669; }
  .tag.mock { background: #f3f4f6; color: #6b7280; }

  /* 内容缺口 */
  .gap-item {
    padding: 12px 14px;
    background: #fff7ed;
    border-left: 3px solid #f59e0b;
    border-radius: 0 6px 6px 0;
    margin-bottom: 10px;
  }
  .gap-item .prompt { font-weight: 600; color: #92400e; font-size: 13px; }
  .gap-item .meta { font-size: 12px; color: #6b7280; margin-top: 4px; }
  .gap-item .topic { font-size: 12px; color: #374151; margin-top: 4px; }

  /* 行动建议 */
  .action-group { margin-bottom: 18px; }
  .action-group .group-title {
    font-size: 13px; font-weight: 700; color: #374151;
    margin-bottom: 10px; display: flex; align-items: center; gap: 8px;
  }
  .prio {
    display: inline-block; padding: 2px 9px; border-radius: 10px;
    font-size: 11px; font-weight: 700;
  }
  .prio.high { background: #fef2f2; color: #dc2626; }
  .prio.medium { background: #fffbeb; color: #d97706; }
  .prio.low { background: #f0fdf4; color: #16a34a; }
  .action-card {
    padding: 14px 16px;
    background: #f9fafb;
    border: 1px solid #e5e7eb;
    border-radius: 8px;
    margin-bottom: 10px;
  }
  .action-card .head { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 6px; }
  .action-card .title { font-weight: 600; color: #111827; font-size: 14px; }
  .action-card .cat { font-size: 11px; color: #6b7280; }
  .action-card .detail { font-size: 12px; color: #4b5563; margin: 4px 0; }
  .action-card ul { margin: 6px 0 4px 18px; font-size: 12px; color: #4b5563; }
  .action-card ul li { margin: 3px 0; }
  .action-card .impact {
    margin-top: 8px; padding-top: 6px; border-top: 1px dashed #e5e7eb;
    font-size: 12px; color: #059669; font-weight: 600;
  }
  .empty-hint { color: #9ca3af; font-size: 13px; font-style: italic; padding: 8px 0; }

  /* 页脚 */
  .footer {
    margin-top: 40px;
    padding-top: 18px;
    border-top: 1px solid #e5e7eb;
    text-align: center;
    font-size: 12px;
    color: #6b7280;
  }

  /* 打印优化 */
  @media print {
    @page { size: A4; margin: 12mm; }
    body { background: #fff; }
    .toolbar { display: none !important; }
    .page { max-width: none; padding: 0; }
    .section { page-break-inside: avoid; }
    .cover { page-break-after: always; min-height: 0; }
    .action-card, .gap-item { page-break-inside: avoid; }
  }
</style>
</head>
<body>
<div class="toolbar">
  <span class="title">📄 ` + html.EscapeString(r.BrandName) + ` · 品牌可见度审计报告</span>
  <div class="actions">
    <a class="btn btn-primary" href="javascript:window.print()">打印 / 保存为 PDF</a>
  </div>
</div>
<div class="page">
`)

	// 1. 封面
	b.WriteString(buildCover(r))

	// 2. 评分明细
	b.WriteString(buildScoreBreakdown(r))

	// 3. 各引擎表现
	b.WriteString(buildEngineStats(r))

	// 4. 竞品声量份额
	b.WriteString(buildCompetitorSOV(r))

	// 5. 内容缺口
	b.WriteString(buildContentGaps(r))

	// 6. 运营行动建议
	b.WriteString(buildActions(r))

	// 7. 页脚
	b.WriteString(buildFooter(r))

	b.WriteString(`
</div>
</body>
</html>`)
	return b.String(), nil
}

// buildCover 构建封面页。
func buildCover(r *brand.VisibilityReport) string {
	var b strings.Builder
	b.WriteString(`<div class="cover">`)
	b.WriteString(`<div class="eyebrow">Brand Visibility Audit Report</div>`)
	b.WriteString(`<h1>` + html.EscapeString(r.BrandName) + `</h1>`)

	// 行业/品类徽章
	var badges []string
	if r.Industry != "" {
		badges = append(badges, `<span class="badge">`+html.EscapeString(r.Industry)+`</span>`)
	}
	if r.Category != "" {
		badges = append(badges, `<span class="badge gray">`+html.EscapeString(r.Category)+`</span>`)
	}
	if len(badges) > 0 {
		b.WriteString(`<div class="badges">` + strings.Join(badges, "") + `</div>`)
	}

	// BVS 评分仪表盘（SVG 环形图）
	b.WriteString(`<div class="gauge">` + svgScoreRing(r.Score, r.Grade) + `</div>`)

	// 等级 + 梯队
	tier := tierLabel(r.Tier)
	b.WriteString(`<div class="grade-row">
    <div class="grade-block"><div class="label">评分等级</div><div class="val">` + html.EscapeString(r.Grade) + ` 级</div></div>
    <div class="grade-block"><div class="label">品牌梯队</div><div class="val">` + html.EscapeString(tier) + `</div></div>
    <div class="grade-block"><div class="label">实体完备度</div><div class="val">` + fmt.Sprintf("%.0f", r.EntityCompletenessScore) + `/100</div></div>
  </div>`)

	// 生成时间
	genTime := r.GeneratedAt
	if genTime.IsZero() {
		genTime = time.Now()
	}
	b.WriteString(`<div class="gen-time">报告生成时间：` + genTime.Local().Format("2006-01-02 15:04:05") + `</div>`)

	// 关联公司信息
	if r.Company != nil && r.Company.Name != "" {
		b.WriteString(`<div class="company-card">`)
		b.WriteString(`<div class="row"><span class="k">关联公司</span><strong>` + html.EscapeString(r.Company.Name) + `</strong></div>`)
		if r.Company.Domain != "" {
			b.WriteString(`<div class="row"><span class="k">官网</span>` + html.EscapeString(r.Company.Domain) + `</div>`)
		}
		if r.Company.Industry != "" {
			b.WriteString(`<div class="row"><span class="k">行业</span>` + html.EscapeString(r.Company.Industry) + `</div>`)
		}
		if r.Company.Headquarters != "" {
			b.WriteString(`<div class="row"><span class="k">总部</span>` + html.EscapeString(r.Company.Headquarters) + `</div>`)
		}
		if r.Company.Description != "" {
			b.WriteString(`<div class="row" style="margin-top:6px"><span class="k">简介</span>` + html.EscapeString(r.Company.Description) + `</div>`)
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`</div>`)
	return b.String()
}

// buildScoreBreakdown 构建评分明细章节（7 维 BVS 加权柱状图 SVG）。
func buildScoreBreakdown(r *brand.VisibilityReport) string {
	bd := r.ScoreBreakdown
	dims := []scoreDim{
		{"内容质量", bd.ContentQuality, 23},
		{"技术SEO", bd.TechnicalSEO, 22},
		{"站内SEO", bd.OnPageSEO, 20},
		{"Schema", bd.Schema, 10},
		{"页面性能", bd.Performance, 10},
		{"AI就绪", bd.AIReadiness, 10},
		{"图像优化", bd.ImageOptimization, 5},
	}

	var b strings.Builder
	b.WriteString(`<div class="section">`)
	b.WriteString(`<h2><span class="num">1</span>评分明细（BVS = ` + fmt.Sprintf("%.1f", r.Score) + `）</h2>`)
	b.WriteString(svgBreakdownBars(dims))
	// 追加严重级别提示
	critical := brand.CriticalDimensions(bd)
	if len(critical) > 0 {
		b.WriteString(`<div class="alert critical">需优先修复的维度：` + strings.Join(critical, "、") + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// buildEngineStats 构建各引擎表现表格。
func buildEngineStats(r *brand.VisibilityReport) string {
	var b strings.Builder
	b.WriteString(`<div class="section">`)
	b.WriteString(`<h2><span class="num">2</span>各引擎表现</h2>`)
	if len(r.EngineStats) == 0 {
		b.WriteString(`<div class="empty-hint">无引擎统计数据</div>`)
		b.WriteString(`</div>`)
		return b.String()
	}
	b.WriteString(`<table>
<thead><tr>
  <th>引擎</th><th>提及率</th><th>引用率</th><th>声量份额</th><th>正面率</th><th>平均位置</th><th>状态</th>
</tr></thead>
<tbody>`)
	for _, s := range r.EngineStats {
		statusTag := `<span class="tag mock">模拟</span>`
		if s.Configured {
			statusTag = `<span class="tag ok">已配置</span>`
		}
		b.WriteString(`<tr>
  <td>` + html.EscapeString(engineDisplayName(string(s.Engine))) + `</td>
  <td>` + fmt.Sprintf("%.1f%%", s.MentionRate) + `</td>
  <td>` + fmt.Sprintf("%.1f%%", s.CitationRate) + `</td>
  <td>` + fmt.Sprintf("%.1f%%", s.ShareOfVoice) + `</td>
  <td>` + fmt.Sprintf("%.1f%%", s.PositiveRate) + `</td>
  <td>` + fmt.Sprintf("%.1f", s.AvgPosition) + `</td>
  <td>` + statusTag + `</td>
</tr>`)
	}
	b.WriteString(`</tbody></table>`)
	b.WriteString(`</div>`)
	return b.String()
}

// buildCompetitorSOV 构建竞品声量份额水平柱状图。
func buildCompetitorSOV(r *brand.VisibilityReport) string {
	var b strings.Builder
	b.WriteString(`<div class="section">`)
	b.WriteString(`<h2><span class="num">3</span>竞品声量份额</h2>`)
	if len(r.CompetitorSOV) == 0 {
		b.WriteString(`<div class="empty-hint">无竞品声量数据</div>`)
		b.WriteString(`</div>`)
		return b.String()
	}
	b.WriteString(svgSOVBars(r.CompetitorSOV, r.BrandName))
	b.WriteString(`</div>`)
	return b.String()
}

// buildContentGaps 构建内容缺口列表。
func buildContentGaps(r *brand.VisibilityReport) string {
	var b strings.Builder
	b.WriteString(`<div class="section">`)
	b.WriteString(`<h2><span class="num">4</span>内容缺口（高机会查询）</h2>`)
	if len(r.ContentGaps) == 0 {
		b.WriteString(`<div class="empty-hint">无内容缺口（品牌在所有查询中均被提及）</div>`)
		b.WriteString(`</div>`)
		return b.String()
	}
	b.WriteString(fmt.Sprintf(`<div style="font-size:12px;color:#6b7280;margin-bottom:12px">发现 %d 个高机会查询，竞品被提及而品牌缺席：</div>`, len(r.ContentGaps)))
	for _, g := range r.ContentGaps {
		competitors := strings.Join(g.CompetitorNamed, "、")
		b.WriteString(`<div class="gap-item">`)
		b.WriteString(`<div class="prompt">` + html.EscapeString(g.Prompt) + `</div>`)
		b.WriteString(`<div class="meta">引擎：` + html.EscapeString(engineDisplayName(string(g.Engine))) + ` · 竞品被提及：` + html.EscapeString(competitors) + `</div>`)
		if g.SuggestedTopic != "" {
			b.WriteString(`<div class="topic">建议话题：` + html.EscapeString(g.SuggestedTopic) + `</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// buildActions 构建运营行动建议（按优先级分组）。
func buildActions(r *brand.VisibilityReport) string {
	var b strings.Builder
	b.WriteString(`<div class="section">`)
	b.WriteString(`<h2><span class="num">5</span>运营行动建议</h2>`)
	if len(r.Actions) == 0 {
		b.WriteString(`<div class="empty-hint">暂无建议</div>`)
		b.WriteString(`</div>`)
		return b.String()
	}

	// 按优先级分组
	groups := map[string][]brand.ActionItem{"high": {}, "medium": {}, "low": {}}
	order := []string{"high", "medium", "low"}
	labels := map[string]string{"high": "高优先级", "medium": "中优先级", "low": "低优先级"}
	for _, a := range r.Actions {
		if _, ok := groups[a.Priority]; !ok {
			groups[a.Priority] = []brand.ActionItem{a}
			order = append(order, a.Priority)
			labels[a.Priority] = a.Priority
		} else {
			groups[a.Priority] = append(groups[a.Priority], a)
		}
	}

	for _, prio := range order {
		items := groups[prio]
		if len(items) == 0 {
			continue
		}
		b.WriteString(`<div class="action-group">`)
		b.WriteString(`<div class="group-title"><span class="prio ` + prio + `">` + labels[prio] + `</span><span style="color:#9ca3af;font-size:12px">` + fmt.Sprintf("%d 条", len(items)) + `</span></div>`)
		for _, a := range items {
			b.WriteString(`<div class="action-card">`)
			b.WriteString(`<div class="head">`)
			b.WriteString(`<span class="prio ` + prio + `">` + labels[prio] + `</span>`)
			if a.Category != "" {
				b.WriteString(`<span class="cat">[` + html.EscapeString(a.Category) + `]</span>`)
			}
			b.WriteString(`<span class="title">` + html.EscapeString(a.Title) + `</span>`)
			b.WriteString(`</div>`)
			if a.Detail != "" {
				b.WriteString(`<div class="detail">` + html.EscapeString(a.Detail) + `</div>`)
			}
			if len(a.Tasks) > 0 {
				b.WriteString(`<ul>`)
				for _, t := range a.Tasks {
					b.WriteString(`<li>` + html.EscapeString(t) + `</li>`)
				}
				b.WriteString(`</ul>`)
			}
			if a.ExpectedImpact != "" {
				b.WriteString(`<div class="impact">预期影响：` + html.EscapeString(a.ExpectedImpact) + `</div>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// buildFooter 构建页脚。
func buildFooter(r *brand.VisibilityReport) string {
	genTime := r.GeneratedAt
	if genTime.IsZero() {
		genTime = time.Now()
	}
	return `<div class="footer">
  报告生成时间：` + genTime.Local().Format("2006-01-02 15:04:05") + ` · 品牌：` + html.EscapeString(r.BrandName) + `<br>
  由 GEO 生成式引擎优化系统生成
</div>`
}

// ---------- SVG 图表 ----------

// svgScoreRing 生成 BVS 评分环形仪表盘（SVG）。
//
// 参考 web 前端 renderBrandScore 的 score-ring 风格：底环 + 进度环 + 中心数字。
func svgScoreRing(score float64, grade string) string {
	const r = 70
	const sw = 12
	cx, cy := 90, 90
	circumference := 2 * math.Pi * r
	// 限制 0-100
	pct := score
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	dash := pct / 100 * circumference
	color := gradeColor(score)

	return fmt.Sprintf(`<svg width="180" height="180" viewBox="0 0 180 180" xmlns="http://www.w3.org/2000/svg">
  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#f3f4f6" stroke-width="%d"/>
  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="%d"
    stroke-dasharray="%.2f %.2f" stroke-linecap="round"
    transform="rotate(-90 %d %d)"/>
  <text x="%d" y="%d" text-anchor="middle" font-size="40" font-weight="700" fill="%s">%.0f</text>
  <text x="%d" y="%d" text-anchor="middle" font-size="13" fill="#6b7280">BVS · %s 级</text>
</svg>`,
		cx, cy, r, sw,
		cx, cy, r, color, sw,
		dash, circumference-dash,
		cx, cy,
		cx, cy+8, color, score,
		cx, cy+30, grade,
	)
}

// svgBreakdownBars 生成 6 维评分柱状图（SVG 水平条）。
func svgBreakdownBars(dims []scoreDim) string {
	const (
		W      = 690
		rowH   = 46
		padTop = 8
		labelX = 8
		barX   = 150
		barW   = 460
		barH   = 20
	)
	H := padTop + len(dims)*rowH + 8
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" style="max-width:100%%">`, W, H, W, H))
	for i, d := range dims {
		y := padTop + i*rowH
		v := d.value
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		color := bandColor(v)
		// 维度名
		b.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="13" fill="#374151" font-weight="600">%s</text>`, labelX, y+15, html.EscapeString(d.name)))
		// 权重
		b.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="11" fill="#9ca3af">权重 %d%%</text>`, labelX, y+31, d.weight))
		// 背景条
		b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="4" fill="#f3f4f6"/>`, barX, y+6, barW, barH))
		// 进度条
		b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%.1f" height="%d" rx="4" fill="%s"/>`, barX, y+6, barW*v/100, barH, color))
		// 数值
		b.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="12" fill="#374151" font-weight="600">%.1f</text>`, barX+barW+8, y+20, v))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// svgSOVBars 生成竞品声量份额水平柱状图（SVG）。
func svgSOVBars(sovs []brand.CompetitorSOV, brandName string) string {
	const (
		W      = 690
		rowH   = 40
		padTop = 8
		labelX = 8
		barX   = 150
		barW   = 460
		barH   = 20
	)
	maxSOV := 1.0
	for _, s := range sovs {
		if s.SOV > maxSOV {
			maxSOV = s.SOV
		}
	}
	H := padTop + len(sovs)*rowH + 8
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg" style="max-width:100%%">`, W, H, W, H))
	for i, s := range sovs {
		y := padTop + i*rowH
		isBrand := s.Name == brandName
		nameColor := "#374151"
		barColor := "#9ca3af"
		if isBrand {
			nameColor = "#4f8cff"
			barColor = "#4f8cff"
		}
		// 名称
		label := s.Name
		if isBrand {
			label = s.Name + "（本品牌）"
		}
		b.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="12" fill="%s" font-weight="%s">%s</text>`, labelX, y+15, nameColor, weight(isBrand), html.EscapeString(truncate(label, 18))))
		// 背景条
		b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="4" fill="#f3f4f6"/>`, barX, y+6, barW, barH))
		// 进度条
		w := barW * s.SOV / maxSOV
		b.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%.1f" height="%d" rx="4" fill="%s"/>`, barX, y+6, w, barH, barColor))
		// 数值
		b.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="12" fill="#374151" font-weight="600">%.1f%% (%d次)</text>`, barX+barW+8, y+20, s.SOV, s.MentionCount))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// ---------- 辅助函数 ----------

// gradeColor 根据分数返回对应颜色。
func gradeColor(score float64) string {
	switch {
	case score >= 90:
		return "#16a34a" // 绿
	case score >= 80:
		return "#0891b2" // 青
	case score >= 70:
		return "#4f8cff" // 蓝
	case score >= 60:
		return "#d97706" // 橙
	default:
		return "#dc2626" // 红
	}
}

// bandColor 根据维度得分返回区间颜色。
func bandColor(v float64) string {
	switch {
	case v >= 70:
		return "#16a34a"
	case v >= 40:
		return "#f59e0b"
	default:
		return "#dc2626"
	}
}

// tierLabel 将梯队编码转为中文标签。
func tierLabel(tier string) string {
	switch tier {
	case "household":
		return "头部"
	case "midmarket":
		return "中坚"
	case "niche":
		return "长尾"
	default:
		if tier == "" {
			return "—"
		}
		return tier
	}
}

// engineDisplayName 将引擎类型编码转为可读名称（与前端 ENGINES 列表保持一致）。
func engineDisplayName(id string) string {
	names := map[string]string{
		"chatgpt":    "ChatGPT",
		"perplexity": "Perplexity",
		"gemini":     "Gemini",
		"claude":     "Claude",
		"qwen":       "通义千问",
		"glm":        "智谱GLM",
		"deepseek":   "DeepSeek",
		"kimi":       "Kimi",
		"wenxin":     "文心一言",
		"doubao":     "豆包",
		"xiaomi":     "小米",
		"xunfei":     "讯飞星火",
		"yuanbao":    "元宝/混元",
	}
	if n, ok := names[id]; ok {
		return n
	}
	return id
}

// weight 返回 SVG font-weight 属性值。
func weight(bold bool) string {
	if bold {
		return "700"
	}
	return "400"
}

// truncate 截断字符串到指定 rune 长度（SVG 标签防溢出）。
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
