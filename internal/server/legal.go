package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"my-geo/internal/util"
)

// ── 法务计划 #80 & #81：合规统一入口 ─────────────────────────────────
//
// 本文件承载：
//   1. 对外爬虫声明（/legal/bot）：UA、频率、遵循 robots、退出邮箱（避风港 + 爬虫合规）
//   2. 自有服务 robots.txt（/robots.txt）：明确 MyGEOBot 不回环访问自身；公开索引允许
//   3. 数据权利 3 个占位接口（访问/导出/删除）：落审计日志，返回处理时限
//   4. 合规元数据接口（/api/v1/meta/compliance）：前端可拉取显示

const (
	dataRightsMinWorkDays = 15 // 访问/导出承诺 SLA
	dataRightsMaxWorkDays = 30 // 删除承诺 SLA
)

// handleLegalBot 返回爬虫说明页（避风港合规要求的人类可读信息页）。
//
// 信息页内容来自搜索行业通用做法（参考 Googlebot / Bingbot / GPTBot 信息页）：
//   - 爬虫身份（User-Agent）
//   - 爬虫目的（品牌可见度审计 / 非训练）
//   - 请求频率（默认每主机 >=600ms，可按 robots Crawl-delay 尊重）
//   - robots 遵循声明（MyGEOBot 专用组优先，其次 *）
//   - 退出/联系邮箱（compliance@mygeo.ai）
//   - 法律依据（合理访问 + 版权避风港）
func (s *Server) handleLegalBot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ua := util.MyGEOUserAgent
	contact := util.MyGEOComplianceEmail
	// 说明：下面原始字符串中存在大量 CSS "%"字符（如 width:100%、margin:40% 等），
	// 对 fmt.Fprintf 都是"未知 verb"。直接改用 strings.Builder + fmt.Fprint
	// 输出字面 HTML，用字符串插值替换少数参数（ua / contact / 年 / 日期）。
	year := time.Now().Year()
	updated := time.Now().Format("2006-01-02")
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html lang="zh-CN"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>MyGEOBot · 爬虫声明（合规信息页）</title>
<style>
 body{font-family:-apple-system,"PingFang SC","Microsoft YaHei",Arial,sans-serif;max-width:820px;margin:40px auto;padding:24px;color:#1f2937;line-height:1.7;background:#fff}
 h1{font-size:24px;margin:0 0 8px}
 h2{font-size:17px;margin:24px 0 10px;border-left:3px solid #4f8cff;padding-left:10px}
 code,pre{background:#f3f4f6;border-radius:6px;padding:2px 6px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;color:#111827}
 pre{padding:12px 14px;overflow-x:auto;white-space:pre-wrap;word-break:break-word}
 table{width:100%;border-collapse:collapse;font-size:14px}
 th,td{padding:10px 12px;text-align:left;border-bottom:1px solid #e5e7eb}
 th{background:#f9fafb;color:#374151;font-weight:600}
 .muted{color:#6b7280;font-size:13px}
 a{color:#2563eb;text-decoration:none}
 .footer{margin-top:32px;padding-top:16px;border-top:1px solid #e5e7eb;color:#6b7280;font-size:12px;text-align:center}
</style></head><body>
<h1>🤖 MyGEOBot 爬虫声明</h1>
<div class="muted">本文档是 <code>MyGEOBot</code> 的合规信息页，对应 User-Agent 中 <code>+/legal/bot</code> 引用。</div>

<h2>1. 爬虫身份（User-Agent）</h2>
<p>所有对外 HTTP 请求统一使用以下 User-Agent 字符串（含信息页 + 联系邮箱，符合 RFC 7231 与行业避风港格式）：</p>
<pre>`)
	sb.WriteString(ua)
	sb.WriteString(`</pre>

<h2>2. 爬虫目的</h2>
<p>MyGEOBot 仅用于"品牌 AI 可见度审计"（GEO = Generative Engine Optimization），具体包括：</p>
<ul>
 <li>访问品牌官网主页 / robots.txt / llms.txt / 公开 sitemap，评估网站对 AI 搜索引擎的可访问性</li>
 <li>读取公开的知识图谱条目（维基百科 / Wikidata / 百度百科），辅助品牌画像补全</li>
 <li>不做任何模型训练数据采集；不存储整站快照；仅保存合规范围内的最小化摘要字段（标题、描述、H1/H2 产品词）</li>
</ul>

<h2>3. 请求频率（礼貌爬取）</h2>
<table>
 <thead><tr><th>维度</th><th>策略</th></tr></thead>
 <tbody>
  <tr><td>同一主机最小间隔</td><td>默认 600ms（可按 robots.txt <code>Crawl-delay</code> 延长，不缩短）</td></tr>
  <tr><td>单页最大读取</td><td>1MB（官网爬取）/ 2MB（结构化页面），超出部分主动丢弃</td></tr>
  <tr><td>超时</td><td>单次请求 10s，robots.txt 5s</td></tr>
 </tbody>
</table>

<h2>4. robots.txt 遵循声明</h2>
<ul>
 <li>优先遵守 <code>User-agent: MyGEOBot</code> 分组的 <code>Disallow</code> 规则</li>
 <li>若无 MyGEOBot 分组，则遵守 <code>User-agent: *</code> 的 <code>Disallow</code> 前缀规则</li>
 <li>遇到 robots.txt 返回 5xx 时，保守判定为"禁止访问"，避免误爬</li>
 <li>404 视为"无限制"（符合 RFC 9309 惯例）</li>
</ul>
<p>站点管理员示例（禁止 MyGEOBot 爬取全站）：</p>
<pre>User-agent: MyGEOBot
Disallow: /</pre>
<p>仅禁止爬取 <code>/private/</code> 目录示例：</p>
<pre>User-agent: MyGEOBot
Disallow: /private/</pre>

<h2>5. 版权避风港 / 退出联系</h2>
<p>若您是网站权利方并希望 MyGEOBot 停止访问您的站点，可通过以下任一方式：</p>
<ol>
 <li>在 robots.txt 中为 <code>MyGEOBot</code> 增加 <code>Disallow: /</code>（通常在 24h 内生效）</li>
 <li>发送邮件至 <a href="mailto:`)
	sb.WriteString(contact)
	sb.WriteString(`">`)
	sb.WriteString(contact)
	sb.WriteString(`</a>，请注明域名 + 联系信息 + 权利证明，我们将在 3 个工作日内处理</li>
 <li>若存在版权投诉（删除/断链），同样通过以上邮箱联系，我们将在收到合格通知后按避风港流程处理</li>
</ol>

<h2>6. 法律依据</h2>
<ul>
 <li>仅爬取公开可访问的网页（不登录、不绕过访问控制）</li>
 <li>严格遵守 robots.txt 协议与频控策略，避免对目标服务器造成不合理负担</li>
 <li>数据用途：为品牌客户生成"可见度审计与优化建议"，不向第三方转售原始内容</li>
 <li>所有 AI 产出内容统一标注"AI 生成，仅供参考"，附引用链接，不构成商业/法律建议</li>
</ul>

<div class="footer">
 © `)
	sb.WriteString(fmt.Sprintf("%d", year))
	sb.WriteString(` 崛起GEO · 合规联系 <a href="mailto:`)
	sb.WriteString(contact)
	sb.WriteString(`">`)
	sb.WriteString(contact)
	sb.WriteString(`</a> · 本页最后更新：`)
	sb.WriteString(updated)
	sb.WriteString(`
</div>
</body></html>`)
	fmt.Fprint(w, sb.String())
}

// handleRobotsTxt 返回服务自有 robots.txt：
//   - 允许主流搜索引擎（*）访问所有公开路由
//   - 禁止 MyGEOBot 回环访问自己（避免审计到自身）
//   - Sitemap 目前未生成，暂不填
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, `# 崛起GEO robots.txt
# 生成时间: `+time.Now().Format(time.RFC3339)+`

User-agent: *
Allow: /

# 不允许自身爬虫回环访问服务（避免自爬）
User-agent: MyGEOBot
Disallow: /

# 退出/投诉：compliance@mygeo.ai
`)
}

// ── 数据权利接口（占位实现，配合设置页 UI 形成合规闭环） ──────────

// dataRightsResp 三类接口统一返回格式。
type dataRightsResp struct {
	OK         bool   `json:"ok"`
	RequestID  string `json:"request_id"`  // 请求编号（审计追踪用）
	Action     string `json:"action"`      // access / export / delete
	AcceptedAt string `json:"accepted_at"` // 受理时间
	SLA        string `json:"sla"`         // 承诺处理时限
	Contact    string `json:"contact"`     // 补材料联系邮箱
	Note       string `json:"note"`        // 给用户的说明
}

func newDataRightsResp(action, sla, note string) dataRightsResp {
	return dataRightsResp{
		OK:         true,
		RequestID:  fmt.Sprintf("DR-%d-%s", time.Now().Unix(), randShortID()),
		Action:     action,
		AcceptedAt: time.Now().Format(time.RFC3339),
		SLA:        sla,
		Contact:    util.MyGEOComplianceEmail,
		Note:       note,
	}
}

func randShortID() string {
	// 非加密短 ID，仅用来让用户有追踪号（SLA 场景不需要强随机）
	const alph = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	t := time.Now().UnixNano()
	out := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		out[i] = alph[int(t%int64(len(alph)))]
		t /= int64(len(alph))
	}
	return string(out)
}

// handleLegalDataAccess 受理"访问我的数据"。
// GET /api/v1/legal/data-access
func (s *Server) handleLegalDataAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	_ = s.recordDataRightsEvent("access", r) // 占位：保留审计日志
	sla := fmt.Sprintf("%d 个工作日", dataRightsMinWorkDays)
	note := "当前版本为单租户无账号模式。我们将按请求编号整理该 IP 提交的审计历史、品牌档案与告警配置，" +
		"发送至您后续邮件中提供的收件地址。"
	writeJSON(w, http.StatusOK, newDataRightsResp("access", sla, note))
}

// handleLegalDataExport 受理"导出我的数据"。
// GET /api/v1/legal/data-export
func (s *Server) handleLegalDataExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	_ = s.recordDataRightsEvent("export", r)
	sla := fmt.Sprintf("%d 个工作日", dataRightsMinWorkDays)
	note := "导出内容按 GDPR 可携带权 / 个保法第 45 条范围：品牌档案、审计历史、设置项，" +
		"格式为 zip（JSON + CSV）。完成后将发送下载链接到联系邮箱。"
	writeJSON(w, http.StatusOK, newDataRightsResp("export", sla, note))
}

// handleLegalDataDelete 受理"删除我的数据"。
// POST /api/v1/legal/data-delete（语义化：产生副作用用 POST）。
func (s *Server) handleLegalDataDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	_ = s.recordDataRightsEvent("delete", r)
	sla := fmt.Sprintf("%d 个工作日", dataRightsMaxWorkDays)
	note := "删除前我们会发送确认邮件以验证权利归属；删除范围包含：品牌档案、审计历史、" +
		"本地告警规则、工单内容。部分依法需保留的数据（财务、合规日志）会保留至法定期限届满后清除。"
	writeJSON(w, http.StatusOK, newDataRightsResp("delete", sla, note))
}

// recordDataRightsEvent 占位：后续接入账号体系后写入审计表。
// 当前实现仅确保编译通过；未来将替换为调用 audit log store。
func (s *Server) recordDataRightsEvent(action string, r *http.Request) error {
	// TODO(#80): 写入审计日志（Admin Audit Log）表：
	//   event_type = "data_rights." + action
	//   actor_ip   = real client IP（from X-Forwarded-For / RemoteAddr）
	//   meta       = {user_agent, accept_language, referer}
	// 目前保持空实现以避免在未接入 store 时引入错误。
	_ = action
	_ = r
	return nil
}

// handleMetaCompliance 返回前端 Footer / 设置页可展示的合规元数据。
// GET /api/v1/meta/compliance
func (s *Server) handleMetaCompliance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	out := map[string]any{
		"user_agent":              util.MyGEOUserAgent,
		"crawler_info_url":        util.MyGEOCrawlInfoURL,
		"compliance_email":        util.MyGEOComplianceEmail,
		"ai_generated_disclaimer": aiGeneratedDisclaimerShort,
		"legal_routes": map[string]string{
			"terms":   "/terms",
			"privacy": "/privacy",
			"dpa":     "/dpa",
			"bot":     util.MyGEOCrawlInfoURL,
		},
		"data_rights_sla_days": map[string]int{
			"access": dataRightsMinWorkDays,
			"export": dataRightsMinWorkDays,
			"delete": dataRightsMaxWorkDays,
		},
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, out)
}

// ── AI 生成标识（字符串常量，供报告/邮件/服务端 HTML 模板复用） ──
//
// 按法务 #81 要求：所有 LLM 产出（报告/PDF/邮件/改写建议/自动摘要）
// 统一附此声明 + 引用追溯 + 不构成商业/法律建议。

const (
	// aiGeneratedDisclaimerShort 简短版（用于邮件 footer、前端 toast 等紧凑位置）。
	aiGeneratedDisclaimerShort = "内容由 AI 生成，仅供参考，不构成商业或法律建议。请以原始引用来源为准。"

	// aiGeneratedDisclaimerFull 完整版（用于 PDF/HTML 报告页脚等显著位置）。
	aiGeneratedDisclaimerFull = `⚠️ AI 生成内容声明：本报告/邮件中的品牌评分、摘要、改写建议、运营行动项均由 AI (大语言模型) 生成，仅供内部参考，不构成任何投资、商业、法律或税务建议。崛起GEO 对 AI 生成内容的准确性、完整性与及时性不做保证。所有结论建议您结合品牌实际情况与原始引用来源（已在报告正文中附引用链接与引擎来源）进行独立验证与判断。若发现事实错误或权利投诉，请联系 compliance@mygeo.ai，我们将在 3 个工作日内完成核实与修正/删除。`
)

// ── 辅助：统一给响应加 X-AI-Generated / X-Content-Source 响应头 ──
//
// 由 middleware 对下列类型响应注入：
//   - /report/html  /report/pdf  /report/email
//   - /analyze  /optimize  /autorewriter/* （内容改写、评分、建议类 LLM 接口）
//   - handleWebSPA 返回的所有 SPA 页面（Footer 已声明，响应头额外提供机器可读声明）

var aiGeneratedHeaderPaths = []string{
	"/api/v1/analyze", "/api/v1/score", "/api/v1/optimize",
	"/api/v1/brand/audit", "/api/v1/brand/readiness", "/api/v1/brand/crawlability",
	"/api/v1/brand/report/html", "/api/v1/brand/report/download", "/api/v1/brand/report/pdf", "/api/v1/brand/report/email",
	"/api/v1/autorewriter/",
}

// shouldMarkAIGenerated 判断路径是否需要加 AI 生成头（由 middleware 调用）。
func shouldMarkAIGenerated(path string) bool {
	if path == "/" || strings.HasPrefix(path, "/dashboard") ||
		strings.HasPrefix(path, "/terms") || strings.HasPrefix(path, "/privacy") ||
		strings.HasPrefix(path, "/dpa") || strings.HasPrefix(path, "/legal/") ||
		strings.HasPrefix(path, "/landing") || strings.HasPrefix(path, "/help") ||
		strings.HasPrefix(path, "/tickets") || strings.HasPrefix(path, "/report") ||
		strings.HasPrefix(path, "/settings") || strings.HasPrefix(path, "/compare") ||
		strings.HasPrefix(path, "/leaderboard") || strings.HasPrefix(path, "/content-optimizer") ||
		strings.HasPrefix(path, "/brand-") || strings.HasPrefix(path, "/keyword") ||
		strings.HasPrefix(path, "/alert") || strings.HasPrefix(path, "/admin") {
		// SPA catch-all 路由全部声明"页面中的 AI 产出可能已被标注"
		return true
	}
	for _, p := range aiGeneratedHeaderPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
