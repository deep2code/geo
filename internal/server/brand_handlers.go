package server

import (
	"encoding/json"
	"fmt"
	"io"
	"my-geo/internal/brand"
	"my-geo/internal/brand/crawlability"
	"my-geo/internal/brand/discover"
	"my-geo/internal/brand/drift"
	"my-geo/internal/brand/externalsignals"
	"my-geo/internal/brand/knowledge"
	"my-geo/internal/brand/kol"
	"my-geo/internal/brand/localseo"
	"my-geo/internal/brand/market"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/readiness"
	"my-geo/internal/brand/report"
	"my-geo/internal/brand/social"
	"my-geo/internal/brand/topsource"
	"my-geo/internal/brand/vertical"
	"my-geo/internal/httputil"
	"my-geo/internal/mail"
	"my-geo/internal/optimizer/autorewriter"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// handleBrandAudit 处理品牌可见度审计请求。
//
// POST /api/v1/brand/audit
// 请求体为品牌画像 JSON（brand.BrandProfile）。
func (s *Server) handleBrandAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var profile brand.BrandProfile
	if err := readJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if profile.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name 不能为空"})
		return
	}
	if len(profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "prompts 不能为空"})
		return
	}
	// 配额校验：计费启用且能解析工作区时，拦截超套餐审计。
	if !s.checkAuditQuota(r) {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
			Error: "本月审计次数已达套餐上限，请升级套餐或下月再试",
		})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	report, err := s.brandEngine.Audit(r.Context(), profile)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	// 审计成功，记录用量（计费启用时）。
	s.recordAuditUsage(r)
	// P1-a：Top Source 执行闭环——把"品牌缺失的权威源"转化为可运营行动项。
	// 当 LLM 判定层可用时，缺失源基于语义识别（更准）；否则正则 URL。
	if ts := topsource.Analyze(profile.Name, report.Results, profile.Domain); ts != nil && len(ts.Recommendations) > 0 {
		report.Actions = append(report.Actions, brand.ActionItem{
			Priority:       "medium",
			Category:       "source",
			Title:          "补齐品牌在权威源上的曝光缺口",
			Detail:         fmt.Sprintf("AI 在回答中引用了 %d 个第三方域名，其中 %d 个品牌未曝光。入驻/外链这些站点是提升 AI 引用的高杠杆动作。", len(ts.TopSources), len(ts.MissingSources)),
			Tasks:          ts.Recommendations,
			ExpectedImpact: "预计相关查询的品牌引用率提升 10-20%",
		})
	}
	writeJSON(w, http.StatusOK, report)
}

// handleBrandMarkets 返回多语言/多市场审计支持的市场列表。
//
// GET /api/v1/brand/markets
//
// 返回 market.SupportedMarkets()，前端据此渲染"目标市场/查询语言"下拉框。
// 该接口不依赖品牌引擎，即便未配置任何 AI 引擎也能正常返回。
func (s *Server) handleBrandMarkets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"markets": market.SupportedMarkets(),
		"count":   len(market.SupportedMarkets()),
	})
}

// handleBrandReport 导出品牌可见度审计报告为自包含 HTML（可打印为 PDF）。
//
// GET /api/v1/brand/report/html?brand=xxx       在浏览器中打开 HTML 报告
// GET /api/v1/brand/report/download?brand=xxx   以附件形式下载 HTML 文件
//
// 从审计历史 DB 取最新一条审计记录的 report_json，调用 report.GenerateHTML 生成。
func (s *Server) handleBrandReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用（无法导出报告）"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "未找到该品牌的审计记录（请先执行一次品牌审计）",
		})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeInternalError(w, err, "解析审计报告")
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeInternalError(w, err, "生成 HTML 报告")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// download 端点设置附件头，触发浏览器下载
	if strings.HasSuffix(r.URL.Path, "/download") {
		filename := sanitizeFilename(brandName) + "_可见度报告.html"
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, htmlOut)
}

// handleBrandReportPDF 导出品牌可见度审计报告为 PDF。
//
// GET /api/v1/brand/report/pdf?brand=xxx
//
// 服务端使用 headless Chromium（chromedp）渲染 HTML 报告为 A4 PDF。
// 无 Chromium 环境时自动降级：返回 JSON 错误提示并附 HTML 报告下载链接。
func (s *Server) handleBrandReportPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用（无法导出报告）"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未找到该品牌的审计记录"})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeInternalError(w, err, "解析审计报告")
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeInternalError(w, err, "生成 HTML 报告")
		return
	}
	pdfBytes, err := report.GeneratePDF(r.Context(), htmlOut)
	if err != nil {
		// 降级：返回错误 + HTML 报告备用链接
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "PDF 渲染失败，建议使用 HTML 报告后在浏览器打印为 PDF：" + err.Error(),
			"html":  "/api/v1/brand/report/download?brand=" + url.QueryEscape(brandName),
		})
		return
	}
	filename := sanitizeFilename(brandName) + "_可见度报告.pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// handleBrandReportMarkdown 导出品牌可见度审计报告为 Markdown 格式。
//
// GET /api/v1/brand/report/markdown?brand=xxx
//
// 返回纯文本 Markdown，可直接粘贴到文档/Notion/飞书等平台。
func (s *Server) handleBrandReportMarkdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用（无法导出报告）"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未找到该品牌的审计记录"})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeInternalError(w, err, "解析审计报告")
		return
	}
	mdOut := report.GenerateMarkdown(&vr)
	filename := sanitizeFilename(brandName) + "_可见度报告.md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, mdOut)
}

// handleBrandReportEmail 把品牌可见度审计报告（HTML + PDF 附件）发送邮件。
//
// POST /api/v1/brand/report/email
//
// JSON {"brand":"腾讯","to":["ops@x.com"],"cc":[],"format":"both"}
// format: pdf / html / both（默认 both）
func (s *Server) handleBrandReportEmail(w http.ResponseWriter, r *http.Request) {
	if s.mailSender == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "邮件未启用（请配置 GEO_SMTP_* 环境变量）"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Brand  string   `json:"brand"`
		To     []string `json:"to"`
		Cc     []string `json:"cc"`
		Format string   `json:"format"` // pdf/html/both
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Brand) == "" || len(body.To) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 与 to 必填"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.Brand)
	if err != nil || rec == nil || rec.ReportJSON == "" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未找到该品牌的审计记录"})
		return
	}
	var vr brand.VisibilityReport
	if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
		writeInternalError(w, err, "解析报告")
		return
	}
	htmlOut, err := report.GenerateHTML(&vr)
	if err != nil {
		writeInternalError(w, err, "生成报告")
		return
	}
	format := body.Format
	if format == "" {
		format = "both"
	}
	msg := &mail.Message{
		To:       body.To,
		Cc:       body.Cc,
		Subject:  fmt.Sprintf("GEO 品牌可见度报告 · %s（BVS %.1f %s）", body.Brand, vr.Score, vr.Grade),
		HTMLBody: htmlOut,
	}
	if format == "both" || format == "pdf" {
		if pdf, err := report.GeneratePDF(r.Context(), htmlOut); err == nil {
			msg.Attachments = append(msg.Attachments, mail.Attachment{
				Filename: sanitizeFilename(body.Brand) + "_可见度报告.pdf",
				Content:  pdf,
			})
		}
		// PDF 失败不阻塞，继续发送 HTML 版
	}
	if err := s.mailSender.Send(msg); err != nil {
		writeInternalError(w, err, "发送邮件")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"to":      body.To,
		"format":  format,
		"subject": msg.Subject,
	})
}

// handleMailStatus 返回邮件发送器启用状态。
// GET /api/v1/mail/status
func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.mailSender != nil && s.mailSender.Enabled(),
		"host":    ternary(s.mailSender != nil, s.mailSender.Host, ""),
		"port":    ternary(s.mailSender != nil, s.mailSender.Port, 0),
		"from":    ternary(s.mailSender != nil, s.mailSender.From, ""),
	})
}

// handleMailSend 通用邮件发送接口（含测试/周报模板）。
//
// POST /api/v1/mail/send
// JSON:
//
//	{
//	  "to": ["a@x.com"], "subject": "...", "text": "...", "html": "...",
//	  "template": "alert|weekly",
//	  "template_data": {...}
//	}
//
// template_data 对应 mail.TemplateAlertData / mail.TemplateWeeklyData。
func (s *Server) handleMailSend(w http.ResponseWriter, r *http.Request) {
	if s.mailSender == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "邮件未启用（请配置 GEO_SMTP_* 环境变量）"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		To           []string          `json:"to"`
		Cc           []string          `json:"cc"`
		Subject      string            `json:"subject"`
		Text         string            `json:"text"`
		HTML         string            `json:"html"`
		Template     string            `json:"template"` // alert / weekly
		TemplateData map[string]any    `json:"template_data"`
		Attachments  []mail.Attachment `json:"attachments"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if len(body.To) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "to 必填"})
		return
	}
	msg := &mail.Message{
		To:          body.To,
		Cc:          body.Cc,
		Subject:     body.Subject,
		TextBody:    body.Text,
		HTMLBody:    body.HTML,
		Attachments: body.Attachments,
	}
	// 用模板渲染 HTML
	if body.Template != "" && len(body.TemplateData) > 0 {
		raw, _ := json.Marshal(body.TemplateData)
		switch body.Template {
		case "alert":
			var d mail.TemplateAlertData
			_ = json.Unmarshal(raw, &d)
			if d.Subject == "" {
				d.Subject = body.Subject
			}
			if d.ConsoleURL == "" {
				d.ConsoleURL = "http://localhost:" + strings.TrimPrefix(s.addr, ":")
			}
			h, err := mail.RenderAlertHTML(d)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "alert 模板渲染失败: " + err.Error()})
				return
			}
			msg.HTMLBody = h
			if msg.Subject == "" {
				msg.Subject = d.Subject
			}
		case "weekly":
			var d mail.TemplateWeeklyData
			_ = json.Unmarshal(raw, &d)
			if d.Subject == "" {
				d.Subject = body.Subject
			}
			if d.ConsoleURL == "" {
				d.ConsoleURL = "http://localhost:" + strings.TrimPrefix(s.addr, ":")
			}
			h, err := mail.RenderWeeklyHTML(d)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "weekly 模板渲染失败: " + err.Error()})
				return
			}
			msg.HTMLBody = h
			if msg.Subject == "" {
				msg.Subject = d.Subject
			}
		default:
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "未知 template: " + body.Template})
			return
		}
	}
	if msg.Subject == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "subject 或 template_data.subject 必填"})
		return
	}
	if err := s.mailSender.Send(msg); err != nil {
		writeInternalError(w, err, "发送")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": body.To, "subject": msg.Subject})
}

// handleBrandAutocomplete 处理品牌智能补全请求。
//
// POST /api/v1/brand/autocomplete
// 请求体: {"brand_name": "品牌名"}
// 返回: 品牌候选画像（domain/aliases/category/products/competitors/prompts/summary）
func (s *Server) handleBrandAutocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req brand.AutocompleteRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.BrandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	candidate, err := s.brandEngine.Autocomplete(r.Context(), req.BrandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, candidate)
}

// handleBrandProfileAutocomplete 处理品牌画像自动补全请求（GET 版本，返回完整 BrandProfile）。
//
// GET /api/v1/brand/profile/autocomplete?name=品牌名
//
// 与 POST /api/v1/brand/autocomplete 不同的是：
//   - 使用 GET 方法，便于浏览器直接调用与缓存
//   - 返回的是完整 brand.BrandProfile（而非 AutocompleteCandidate），可直接用于后续审计接口
//
// 内部调用 brandEngine.Autocomplete，将 AutocompleteCandidate 转换为 BrandProfile 返回。
func (s *Server) handleBrandProfileAutocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("name"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name 不能为空"})
		return
	}
	candidate, err := s.brandEngine.Autocomplete(r.Context(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	// 将 AutocompleteCandidate 转换为 BrandProfile
	profile := brand.BrandProfile{
		Name:        candidate.Name,
		Aliases:     candidate.Aliases,
		Domain:      candidate.Domain,
		Products:    candidate.Products,
		Company:     candidate.Company,
		Competitors: candidate.Competitors,
		Prompts:     candidate.Prompts,
		Industry:    candidate.Industry,
		Category:    candidate.Category,
	}
	if profile.Name == "" {
		profile.Name = brandName
	}
	if len(profile.Prompts) == 0 {
		// 兜底 prompts，避免返回的 BrandProfile 无法直接用于审计
		profile.Prompts = []string{
			fmt.Sprintf("最好的%s", firstNotEmpty(profile.Category, brandName)),
			fmt.Sprintf("%s推荐", brandName),
		}
	}
	writeJSON(w, http.StatusOK, profile)
}

// handleBrandKnowledgeSearch 搜索本地品牌知识库（SinoFacts CC BY 4.0）。
//
// GET  /api/v1/brand/knowledge/search?q=<query>&limit=5
// POST /api/v1/brand/knowledge/search JSON { "q": "...", "limit": 5 }
//
// 返回来自 383 家中国出海软件公司的离线匹配结果，零延迟。
func (s *Server) handleBrandKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	kb := s.brandEngine.Knowledge()
	if kb == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "知识库未加载"})
		return
	}
	var (
		q     string
		limit = 5
	)
	if r.Method == http.MethodGet {
		q = r.URL.Query().Get("q")
		_, limit = httputil.OffsetLimit(r, 5, 100)
	} else if r.Method == http.MethodPost {
		var body struct {
			Q     string `json:"q"`
			Limit int    `json:"limit"`
		}
		if err := readJSON(r, &body); err == nil {
			q = body.Q
			if body.Limit > 0 {
				limit = body.Limit
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	results := kb.Search(q, limit)
	// 转为对前端友好的扁平对象（去掉 brand.AutocompleteCandidate 中 Prompts/Competitors 减少传输量）
	type item struct {
		BrandName     string   `json:"brand_name"`
		BrandDomain   string   `json:"brand_domain,omitempty"`
		BrandAliases  []string `json:"brand_aliases,omitempty"`
		Industry      string   `json:"industry,omitempty"`
		Category      string   `json:"category,omitempty"`
		Products      []string `json:"products,omitempty"`
		CompanyName   string   `json:"company_name,omitempty"`
		CompanyDomain string   `json:"company_domain,omitempty"`
		HQ            string   `json:"hq,omitempty"`
		FoundedYear   int      `json:"founded_year,omitempty"`
		Desc          string   `json:"description,omitempty"`
		Source        string   `json:"source"`       // sinofacts | offlinedb
		SourceLabel   string   `json:"source_label"` // 前端显示的 badge 文案
		Score         float64  `json:"score"`        // 0-100
		// --- offlinedb 专属字段 ---
		CreditCode     string `json:"credit_code,omitempty"`
		LegalPerson    string `json:"legal_person,omitempty"`
		RegisteredDate string `json:"registered_date,omitempty"`
		Capital        string `json:"capital,omitempty"`
		Province       string `json:"province,omitempty"`
		City           string `json:"city,omitempty"`
		Address        string `json:"address,omitempty"`
		CompanyType    string `json:"company_type,omitempty"`
		BusinessScope  string `json:"business_scope,omitempty"`
	}
	out := make([]item, 0, limit*2)
	for _, r := range results {
		out = append(out, item{
			BrandName:     r.Entry.BrandName,
			BrandDomain:   r.Entry.BrandDomain,
			BrandAliases:  r.Entry.BrandAliases,
			Industry:      r.Entry.Industry,
			Category:      r.Entry.Category,
			Products:      r.Entry.Products,
			CompanyName:   r.Entry.CompanyName,
			CompanyDomain: r.Entry.CompanyDomain,
			HQ:            r.Entry.Headquarters,
			FoundedYear:   r.Entry.FoundedYear,
			Desc:          r.Entry.DescriptionZh,
			Source:        "sinofacts",
			SourceLabel:   "📚 品牌知识库（SinoFacts CC BY 4.0）",
			Score:         r.Score,
		})
	}
	// 追加：离线工商 MySQL 库匹配（用剩余配额）
	odbQuota := limit
	if odb := s.brandEngine.OfflineDB(); odb != nil && odbQuota > 0 {
		odbRes, err := odb.Search(r.Context(), offlinedb.SearchOptions{Query: q, TopN: odbQuota})
		if err == nil {
			for _, c := range odbRes {
				desc := c.BusinessScope
				if len(desc) > 120 {
					desc = desc[:120] + "..."
				}
				out = append(out, item{
					BrandName:   c.Name,
					Industry:    c.Province,
					CompanyName: c.Name,
					HQ:          c.City,
					FoundedYear: func() int {
						y := 0
						if len(c.RegistrationDay) >= 4 {
							y, _ = strconv.Atoi(c.RegistrationDay[:4])
						}
						return y
					}(),
					Desc:           desc,
					Source:         "offlinedb",
					SourceLabel:    "💾 离线工商库（1978-2019，guichong/- 种子数据）",
					Score:          c.Score,
					CreditCode:     c.Code,
					LegalPerson:    c.LegalRepresentative,
					RegisteredDate: c.RegistrationDay,
					Capital:        c.Capital,
					Province:       c.Province,
					City:           c.City,
					Address:        c.Address,
					CompanyType:    c.Character,
					BusinessScope:  c.BusinessScope,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":           kb.N,
		"query":           q,
		"result":          out,
		"sinofacts_count": len(results),
		"offlinedb_count": max(0, len(out)-len(results)),
		"license":         "SinoFacts dataset under CC BY 4.0 (https://sinofacts.com); 离线工商数据源自 guichong/- 仓库（国家工商公示系统 1978-2019 公开历史数据）。",
	})
}

// handleChinaCheckSearch 搜索工商注册公司（China-Check MCP 调试接口）。
//
// GET  /api/v1/brand/chinacheck/search?q=<query>&limit=5
// POST /api/v1/brand/chinacheck/search JSON { "q": "...", "limit": 5 }
//
// 返回来自国家企业信用信息公示系统（GSXT/SAMR）的公司匹配列表。
func (s *Server) handleChinaCheckSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "品牌审计引擎未初始化",
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	var (
		q     string
		limit = 5
	)
	if r.Method == http.MethodGet {
		q = r.URL.Query().Get("q")
		_, limit = httputil.OffsetLimit(r, 5, 100)
	} else if r.Method == http.MethodPost {
		var body struct {
			Q     string `json:"q"`
			Limit int    `json:"limit"`
		}
		if err := readJSON(r, &body); err == nil {
			q = body.Q
			if body.Limit > 0 {
				limit = body.Limit
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "q 不能为空"})
		return
	}
	result, err := cc.Search(r.Context(), q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":     fmt.Sprintf("China-Check 搜索失败: %v", err),
			"total":     0,
			"companies": []struct{}{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":      q,
		"total":      result.Total,
		"companies":  result.Companies,
		"source":     "国家企业信用信息公示系统（GSXT / SAMR） via China-Check MCP",
		"disclaimer": "本接口返回的数据来自国家企业信用信息公示系统公开信息，仅供参考，请以官方系统最新登记为准。",
	})
}

// handleChinaCheckSnapshot 获取单家公司的工商注册快照（China-Check MCP 调试接口）。
//
// GET  /api/v1/brand/chinacheck/snapshot?company_id=<ID>&q=<名称>
// POST /api/v1/brand/chinacheck/snapshot JSON { "company_id": "...", "q": "..." }
//
// company_id 和 q 至少传一个；同时传时优先 company_id（更精准）。
func (s *Server) handleChinaCheckSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "品牌审计引擎未初始化",
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
		})
		return
	}
	var (
		companyID string
		query     string
	)
	if r.Method == http.MethodGet {
		companyID = r.URL.Query().Get("company_id")
		query = r.URL.Query().Get("q")
	} else if r.Method == http.MethodPost {
		var body struct {
			CompanyID string `json:"company_id"`
			Q         string `json:"q"`
		}
		if err := readJSON(r, &body); err == nil {
			companyID = body.CompanyID
			query = body.Q
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if companyID == "" && strings.TrimSpace(query) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "company_id 和 q 至少提供一个"})
		return
	}
	snap, err := cc.GetSnapshot(r.Context(), companyID, query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("China-Check snapshot 失败: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"company_id": snap.CompanyID,
		"snapshot":   snap.Snapshot,
		"disclaimer": firstNotEmpty(snap.Disclaimer, "本接口返回的数据来自国家企业信用信息公示系统公开信息，仅供参考，请以官方系统最新登记为准。"),
		"source":     "国家企业信用信息公示系统（GSXT / SAMR） via China-Check MCP",
	})
}

// handleOfflineDBStats  GET /api/v1/brand/offlinedb/stats
func (s *Server) handleOfflineDBStats(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "品牌审计引擎未初始化"})
		return
	}
	odb := s.brandEngine.OfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用（GEO_OFFLINE_DB_ENABLED=true）"})
		return
	}
	st, err := odb.Stats(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleOfflineDBSearch GET ?q=腾讯&n=10&province=广东  POST JSON {q,n,province,city}
func (s *Server) handleOfflineDBSearch(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "品牌审计引擎未初始化", "result": []struct{}{}})
		return
	}
	odb := s.brandEngine.OfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用", "result": []struct{}{}})
		return
	}
	opt := offlinedb.SearchOptions{TopN: 10}
	if r.Method == http.MethodGet {
		opt.Query = r.URL.Query().Get("q")
		opt.Province = r.URL.Query().Get("province")
		opt.City = r.URL.Query().Get("city")
		if n := r.URL.Query().Get("n"); n != "" {
			if v, err := strconv.Atoi(n); err == nil && v > 0 {
				opt.TopN = v
			}
		}
	} else if r.Method == http.MethodPost {
		var in offlinedb.SearchOptions
		if err := readJSON(r, &in); err == nil {
			opt = in
			if opt.TopN <= 0 {
				opt.TopN = 10
			}
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET/POST"})
		return
	}
	start := time.Now()
	res, err := odb.Search(r.Context(), opt)
	if err != nil {
		writeInternalError(w, err, "离线工商检索")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":    opt.Query,
		"province": opt.Province,
		"city":     opt.City,
		"count":    len(res),
		"took_ms":  time.Since(start).Milliseconds(),
		"result":   res,
		"source":   "guichong/- JSON 分支（国家工商公示系统 1978-2019 公开历史数据）→ MariaDB + Meilisearch 中文全文检索",
	})
}

// handleOfflineDBClear POST /api/v1/brand/offlinedb/clear 清空库（清空表）
func (s *Server) handleOfflineDBClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅 POST"})
		return
	}
	if !s.requireDataAdmin(w, r) {
		return
	}
	if s.brandEngine == nil || s.brandEngine.OfflineDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用"})
		return
	}
	before, _ := s.brandEngine.OfflineDB().Stats(r.Context())
	if err := s.brandEngine.OfflineDB().Clear(r.Context()); err != nil {
		writeInternalError(w, err, "")
		return
	}
	after, _ := s.brandEngine.OfflineDB().Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"before_count": before.Count,
		"after_count":  after.Count,
		"before_size":  before.FileSize,
		"after_size":   after.FileSize,
	})
}

// handleOfflineDBProvinces GET /api/v1/brand/offlinedb/provinces 返回数据库内所有省份（下拉框用）
func (s *Server) handleOfflineDBProvinces(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.OfflineDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "离线工商库未启用"})
		return
	}
	list, err := s.brandEngine.OfflineDB().Provinces(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"provinces": list})
}

// ---------- handleChinaCheckCache：缓存管理接口 ----------
//
// GET  /api/v1/brand/chinacheck/cache?action=stats               查看缓存统计
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "clear" } 清空缓存
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "compact" } 压缩/去重缓存文件
// POST /api/v1/brand/chinacheck/cache  JSON { "action": "import", "queries": ["腾讯","阿里","字节跳动"] }
//
// import 动作：按列表依次执行 Search+Snapshot 预热缓存（可指定 limit/并发度）。
func (s *Server) handleChinaCheckCache(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "品牌审计引擎未初始化",
		})
		return
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）",
		})
		return
	}
	ca := cc.Cache()
	if ca == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "China-Check 缓存未启用（设 GEO_CHINACHECK_CACHE_ENABLED=true 以启用）",
		})
		return
	}

	// 解析 action
	action := ""
	if r.Method == http.MethodGet {
		action = strings.ToLower(r.URL.Query().Get("action"))
		if action == "" {
			action = "stats"
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Action  string   `json:"action"`
			Queries []string `json:"queries,omitempty"`
			Limit   int      `json:"limit,omitempty"`
		}
		if err := readJSON(r, &body); err == nil {
			action = strings.ToLower(body.Action)
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}

	switch action {
	case "stats":
		writeJSON(w, http.StatusOK, ca.Stats())
	case "clear":
		if !s.requireDataAdmin(w, r) {
			return
		}
		if err := ca.Clear(); err != nil {
			writeInternalError(w, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "缓存已清空",
			"stats":   ca.Stats(),
		})
	case "compact":
		if !s.requireDataAdmin(w, r) {
			return
		}
		if err := ca.Compact(); err != nil {
			writeInternalError(w, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "缓存已压缩/去重",
			"stats":   ca.Stats(),
		})
	case "import":
		// 预热：读取请求中的 queries 列表
		var body struct {
			Queries []string `json:"queries"`
			Limit   int      `json:"limit"`
		}
		body.Limit = 3
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "解析失败: " + err.Error()})
			return
		}
		if len(body.Queries) == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "queries 不能为空"})
			return
		}
		ctx := r.Context()
		done := 0
		errors := map[string]string{}
		for _, q := range body.Queries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			sr, err := cc.Search(ctx, q, body.Limit)
			if err != nil {
				errors[q] = err.Error()
				continue
			}
			// 只对 Top1 拉 snapshot（最常用命中）
			if len(sr.Companies) > 0 {
				best := sr.Companies[0]
				if _, err := cc.GetSnapshot(ctx, best.CompanyID, ""); err != nil {
					errors[q+"/snapshot"] = err.Error()
				}
			}
			done++
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":          true,
			"imported":    done,
			"total":       len(body.Queries),
			"errors":      errors,
			"stats_after": ca.Stats(),
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "未知 action，支持: stats / clear / compact / import",
		})
	}
}

// handleHistoryList 查询指定品牌的审计历史（按时间降序）。
// GET/POST /api/v1/brand/history/list?brand=腾讯&limit=50
func (s *Server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	brandName := r.URL.Query().Get("brand")
	if brandName == "" {
		var body struct {
			Brand string `json:"brand"`
		}
		if r.Method == http.MethodPost {
			_ = readJSON(r, &body)
			brandName = body.Brand
		}
	}
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand 参数"})
		return
	}
	offset, limit := httputil.OffsetLimit(r, 50, 500)
	records, err := s.brandEngine.HistoryDB().List(r.Context(), brandName, limit, offset)
	if err != nil {
		writeInternalError(w, err, "读取审计历史")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"brand":   brandName,
		"count":   len(records),
		"offset":  offset,
		"limit":   limit,
		"records": records,
	})
}

// handleHistoryGet 查询单条审计记录的完整信息（含 report_json）。
// GET /api/v1/brand/history/get?id=123
func (s *Server) handleHistoryGet(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 id 参数"})
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id 参数无效"})
		return
	}
	rec, err := s.brandEngine.HistoryDB().GetByID(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "记录不存在"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleHistoryStats 返回历史库统计信息。
// GET /api/v1/brand/history/stats
func (s *Server) handleHistoryStats(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	st, err := s.brandEngine.HistoryDB().Stats(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleHistoryStatsDaily 返回过去 N 天的按天聚合计数（给 Dashboard 30 天趋势）。
// GET /api/v1/brand/history/stats/daily?days=30
func (s *Server) handleHistoryStatsDaily(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	days := 30
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	out, err := s.brandEngine.HistoryDB().DailyCounts(r.Context(), days)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	// 兼容字段：为前端 chart 直接可绑，再返回一个聚合 summary。
	var (
		totalRecords int64
		sumScore     float64
		scoreDays    int
	)
	for i := range out {
		totalRecords += out[i].Count
		if out[i].AvgScore >= 0 {
			sumScore += out[i].AvgScore
			scoreDays++
		}
	}
	var avgScore *float64
	if scoreDays > 0 {
		v := sumScore / float64(scoreDays)
		avgScore = &v
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"days":    days,
		"records": out,
		"summary": map[string]interface{}{
			"total_records":     totalRecords,
			"score_days":        scoreDays,
			"avg_score_daily":   avgScore,
			"daily_avg_records": float64(totalRecords) / float64(days),
		},
	})
}

// handleHistoryBrands 列出所有有审计记录的品牌。
// GET /api/v1/brand/history/brands
func (s *Server) handleHistoryBrands(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	names, err := s.brandEngine.HistoryDB().Brands(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":  len(names),
		"brands": names,
	})
}

// handleHistoryClear 清空历史库。
// POST /api/v1/brand/history/clear
func (s *Server) handleHistoryClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅 POST"})
		return
	}
	if !s.requireDataAdmin(w, r) {
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	if err := s.brandEngine.HistoryDB().Clear(r.Context()); err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "历史库已清空"})
}

// handleSchedulerStatus 返回调度器状态。
// GET /api/v1/brand/scheduler/status
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
			"message": "调度器未启用（设置 GEO_SCHEDULER_ENABLED=true + GEO_SCHEDULER_CONFIG=/path/to/config.json 启用）",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
	})
}

// handleSchedulerTrigger 手动触发一次指定品牌的定时审计。
// POST /api/v1/brand/scheduler/trigger  body: {"brand_name": "...", "profile": {...}}
func (s *Server) handleSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌引擎未初始化"})
		return
	}
	var body struct {
		BrandName string             `json:"brand_name"`
		Profile   brand.BrandProfile `json:"profile"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请求体解析失败: " + err.Error()})
		return
	}
	if body.BrandName == "" {
		body.BrandName = body.Profile.Name
	}
	if body.BrandName == "" || len(body.Profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand_name 或 profile.prompts"})
		return
	}
	// 取上一次审计的引擎统计（用于模型分歧告警对比）
	var prevStats []brand.EngineStats
	if s.brandEngine.HistoryDB() != nil {
		if rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName); err == nil && rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var prev brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &prev); err == nil {
				prevStats = prev.EngineStats
			}
		}
	}
	// 直接执行审计
	report, err := s.brandEngine.Audit(r.Context(), body.Profile)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	resp := map[string]interface{}{
		"ok":     true,
		"report": report,
	}
	// 模型分歧告警：对比当前与上次审计，检测 5 类异常信号
	if s.scheduler != nil && len(prevStats) > 0 {
		if mr := s.scheduler.Monitor(report.EngineStats, prevStats); mr != nil {
			resp["monitor_result"] = mr
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReadinessAudit 处理 AI 可见度就绪审计请求。
//
// POST /api/v1/brand/readiness  JSON {"url": "example.com"}
//
// 检查目标网站对 AI 搜索引擎的可见度就绪度（robots.txt / llms.txt /
// 结构化数据 / sitemap.xml / TTFB），返回 readiness.AuditResult。
func (s *Server) handleReadinessAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		URL    string `json:"url"`
		Domain string `json:"domain"` // 与前端 BrandProfile.domain 对齐的别名
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	rawURL := body.URL
	if rawURL == "" {
		rawURL = body.Domain
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url 不能为空"})
		return
	}
	result, err := readiness.Audit(r.Context(), rawURL)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleCrawlabilityAudit 处理 AI 可爬取性审计请求。
//
// POST /api/v1/brand/crawlability  JSON {"url": "https://example.com"}
//
// 审计 27 个 AI 爬虫的 robots.txt 放行状态、JSON-LD schema 丰富度、
// llms.txt 存在性、知识图谱（Wikidata/Wikipedia/百度百科）存在性。
func (s *Server) handleCrawlabilityAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		URL    string `json:"url"`
		Domain string `json:"domain"` // 与前端 BrandProfile.domain 对齐的别名
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	rawURL := body.URL
	if rawURL == "" {
		rawURL = body.Domain
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url 不能为空"})
		return
	}
	result, err := crawlability.Audit(r.Context(), rawURL)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleDriftAudit 处理 diff/drift 回归检测请求。
//
// GET  /api/v1/brand/drift?brand_name=腾讯
// GET  /api/v1/brand/drift?brand_name=腾讯&prev_id=10&cur_id=12
// POST /api/v1/brand/drift  JSON {"brand_name":"腾讯","prev_id":10,"cur_id":12}
//
// 对比两次审计历史记录，检测各维度漂移与回归。
// 未指定 prev_id/cur_id 时自动取该品牌最近两条记录对比。
func (s *Server) handleDriftAudit(w http.ResponseWriter, r *http.Request) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	var (
		brandName     string
		prevID, curID int64
	)
	if r.Method == http.MethodGet {
		brandName = r.URL.Query().Get("brand_name")
		if p := r.URL.Query().Get("prev_id"); p != "" {
			prevID, _ = strconv.ParseInt(p, 10, 64)
		}
		if c := r.URL.Query().Get("cur_id"); c != "" {
			curID, _ = strconv.ParseInt(c, 10, 64)
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			BrandName string `json:"brand_name"`
			PrevID    int64  `json:"prev_id"`
			CurID     int64  `json:"cur_id"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		brandName = body.BrandName
		prevID = body.PrevID
		curID = body.CurID
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(brandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}

	// 按 ID 取指定两条记录对比
	if prevID > 0 && curID > 0 {
		prev, err := s.brandEngine.HistoryDB().GetByID(r.Context(), prevID)
		if err != nil || prev == nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("prev_id=%d 记录不存在", prevID)})
			return
		}
		cur, err := s.brandEngine.HistoryDB().GetByID(r.Context(), curID)
		if err != nil || cur == nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("cur_id=%d 记录不存在", curID)})
			return
		}
		writeJSON(w, http.StatusOK, drift.Compare(*prev, *cur))
		return
	}

	// 默认取最近两条对比
	report, err := drift.CompareLatest(r.Context(), s.brandEngine.HistoryDB(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if report == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("品牌 %s 历史记录不足两条，无法对比", brandName)})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleSocialMonitor 处理社媒情感监控请求。
//
// GET  /api/v1/brand/social/monitor?brand_name=腾讯&platforms=reddit,weibo,youtube&limit=20
// POST /api/v1/brand/social/monitor  JSON {"brand_name": "...", "platforms": ["reddit","weibo","youtube"], "limit": 20}
//
// 在 Reddit / 微博 / YouTube 等社媒平台并行搜索品牌提及，
// 执行规则引擎情感分析，返回提及列表 + 情感评分 + 各平台统计。
// Twitter / 小红书 适配器预留接口，未配置 API Key 时返回提示错误。
func (s *Server) handleSocialMonitor(w http.ResponseWriter, r *http.Request) {
	var (
		brandName string
		platforms []string
		limit     = 20
	)
	if r.Method == http.MethodGet {
		brandName = r.URL.Query().Get("brand_name")
		if p := r.URL.Query().Get("platforms"); p != "" {
			platforms = strings.Split(p, ",")
		}
		_, limit = httputil.OffsetLimit(r, 20, 200)
	} else if r.Method == http.MethodPost {
		var body struct {
			BrandName string   `json:"brand_name"`
			Platforms []string `json:"platforms"`
			Limit     int      `json:"limit"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		brandName = body.BrandName
		platforms = body.Platforms
		if body.Limit > 0 {
			limit = body.Limit
		}
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(brandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}
	if len(platforms) == 0 {
		// 默认全平台
		platforms = []string{"reddit", "weibo", "youtube", "twitter", "xiaohongshu"}
	}
	// 清理平台标识（去空白、转小写）
	cleaned := make([]string, 0, len(platforms))
	for _, p := range platforms {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	platforms = cleaned

	result, err := social.Monitor(r.Context(), brandName, platforms, limit)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleKOLAnalyze 处理 KOL/创作者情报分析请求。
//
// POST /api/v1/brand/kol/analyze
// 请求体: {"brand_name": "...", "results": [...], "competitors": [...]}
//
// results 可从请求体直接传入（前端审计完成后直接传审计结果）；
// 若未传 results 但提供了 brand_name，则从 history DB 最新审计记录中取。
// competitors 可选，用于识别竞品引用源（生成"竞品引用源，需关注"推荐）。
func (s *Server) handleKOLAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		BrandName   string               `json:"brand_name"`
		Results     []brand.PromptResult `json:"results"`
		Competitors []brand.Competitor   `json:"competitors"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}

	results := body.Results
	// results 为空时，从 history DB 最新审计记录中取
	if len(results) == 0 && s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName)
		if err != nil {
			writeInternalError(w, err, "读取审计历史")
			return
		}
		if rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var vr brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err == nil {
				results = vr.Results
			}
		}
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "results 为空且无可用审计历史记录"})
		return
	}

	report := kol.AnalyzeWithCompetitors(body.BrandName, results, body.Competitors)
	writeJSON(w, http.StatusOK, report)
}

// handleTopSourceAnalyze 处理 Top Source 归因分析请求。
//
// POST /api/v1/brand/topsource/analyze
// 请求体: {"brand_name": "...", "results": [...], "brand_domain": "example.com"}
//
// results 可从请求体直接传入；若未传但提供了 brand_name，则从 history DB
// 最新审计记录中取。brand_domain 可选，用于判定品牌是否已在该域名上曝光。
func (s *Server) handleTopSourceAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		BrandName   string               `json:"brand_name"`
		Results     []brand.PromptResult `json:"results"`
		BrandDomain string               `json:"brand_domain"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}

	results := body.Results
	// results 为空时，从 history DB 最新审计记录中取
	if len(results) == 0 && s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		rec, err := s.brandEngine.HistoryDB().Latest(r.Context(), body.BrandName)
		if err != nil {
			writeInternalError(w, err, "读取审计历史")
			return
		}
		if rec != nil && strings.TrimSpace(rec.ReportJSON) != "" {
			var vr brand.VisibilityReport
			if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err == nil {
				results = vr.Results
			}
		}
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "results 为空且无可用审计历史记录"})
		return
	}

	report := topsource.Analyze(body.BrandName, results, body.BrandDomain)
	writeJSON(w, http.StatusOK, report)
}

// handleVerticalDetect 处理行业类型自动识别请求。
//
// POST /api/v1/brand/vertical/detect
// 请求体: 品牌画像字段（industry/category/domain/products/company 等任意组合）
//
// 返回检测到的行业类型、中文标签与差异化评分权重。
func (s *Server) handleVerticalDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var profile map[string]interface{}
	if err := readJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	v := vertical.Detect(profile)
	cfg := vertical.GetConfig(v)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vertical":      v,
		"label":         cfg.Label,
		"description":   cfg.Description,
		"score_weights": cfg.ScoreWeights,
	})
}

// handleVerticalList 返回全部已知的业务垂直行业列表。
//
// GET /api/v1/brand/vertical/list
func (s *Server) handleVerticalList(w http.ResponseWriter, r *http.Request) {
	vs := vertical.AllVerticals()
	out := make([]map[string]interface{}, 0, len(vs))
	for _, v := range vs {
		cfg := vertical.GetConfig(v)
		out = append(out, map[string]interface{}{
			"vertical":      v,
			"label":         cfg.Label,
			"description":   cfg.Description,
			"score_weights": cfg.ScoreWeights,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"verticals": out,
		"count":     len(out),
	})
}

// handleLocalSEOAudit 处理本地 SEO / GMB 审计请求。
//
// POST /api/v1/brand/localseo/audit
// 请求体: {"brand_name": "...", "nap": {"name": "...", "address": "...", "phone": "...", "website": "..."}}
//
// 检查 NAP 一致性、GMB 资料完整度、本地引用收录情况，返回综合评分与建议。
func (s *Server) handleLocalSEOAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		BrandName string                 `json:"brand_name"`
		NAP       localseo.NAPInfo       `json:"nap"`
		Profile   map[string]interface{} `json:"profile,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.BrandName) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_name 不能为空"})
		return
	}
	// nap.name 为空时用 brand_name 兜底
	if strings.TrimSpace(body.NAP.Name) == "" {
		body.NAP.Name = body.BrandName
	}
	report, err := localseo.Audit(r.Context(), body.BrandName, body.NAP)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleExternalSignals 处理外部信号采集请求。
//
// GET  /api/v1/brand/externalsignals/report?domain=example.com&keywords=kw1,kw2
// POST /api/v1/brand/externalsignals/report  JSON {"domain": "...", "keywords": ["..."]}
//
// 调用 DataForSEO（付费，需 GEO_DFS_APIKEY/GEO_DFS_EMAIL）或 Common Crawl（免费）
// 采集关键词搜索量/难度、反链与 SERP 特性。无 API Key 时返回模拟数据并标注。
func (s *Server) handleExternalSignals(w http.ResponseWriter, r *http.Request) {
	var (
		domain   string
		keywords []string
	)
	if r.Method == http.MethodGet {
		domain = r.URL.Query().Get("domain")
		if k := r.URL.Query().Get("keywords"); k != "" {
			keywords = strings.Split(k, ",")
		}
	} else if r.Method == http.MethodPost {
		var body struct {
			Domain   string   `json:"domain"`
			Keywords []string `json:"keywords"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		domain = body.Domain
		keywords = body.Keywords
	} else {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	if strings.TrimSpace(domain) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "domain 不能为空"})
		return
	}
	client := externalsignals.NewFromEnv()
	report, err := client.FullReport(r.Context(), domain, keywords)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleAutoRewriteRules 返回 AutoGEO 默认规则集（含 Princeton PWC 提升值）。
//
// GET  /api/v1/autorewriter/rules
// POST /api/v1/autorewriter/rules  JSON {"query": "...", "doc": "...", "citation_result": "..."}
//
// POST 时若 LLM 可用，则基于文档与引用结果动态提取规则；否则返回默认规则集。
func (s *Server) handleAutoRewriteRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"rules":  autorewriter.DefaultRules(),
			"source": "princeton",
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / POST"})
		return
	}
	var body struct {
		Query          string `json:"query"`
		Doc            string `json:"doc"`
		CitationResult string `json:"citation_result"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	rw := s.newAutoRewriter()
	rs, err := rw.ExtractRules(r.Context(), body.Query, body.Doc, body.CitationResult)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// handleAutoRewrite 依据规则改写内容并执行 GEU 校验。
//
// POST /api/v1/autorewriter/rewrite
// 请求体: {"content": "...", "query": "...", "engine": "...", "preserve_facts": true, "rules": [...]}
//
// rules 为空时使用默认规则。返回改写后内容、应用规则、预估 PWC 提升与 GEU 校验结果。
func (s *Server) handleAutoRewrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Content       string              `json:"content"`
		Query         string              `json:"query"`
		Engine        string              `json:"engine"`
		PreserveFacts bool                `json:"preserve_facts"`
		Rules         []autorewriter.Rule `json:"rules"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
		return
	}
	rw := s.newAutoRewriter()
	req := &autorewriter.RewriteRequest{
		Content:       body.Content,
		Query:         body.Query,
		Engine:        body.Engine,
		PreserveFacts: body.PreserveFacts,
		Rules:         body.Rules,
	}
	result, err := rw.Rewrite(r.Context(), req)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAutoRewriteGEU 对原文与改写文执行 GEU 校验（标准阈值）。
//
// POST /api/v1/autorewriter/geu
// 请求体: {"original": "...", "rewritten": "..."}
func (s *Server) handleAutoRewriteGEU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Original  string `json:"original"`
		Rewritten string `json:"rewritten"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Original) == "" || strings.TrimSpace(body.Rewritten) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "original 与 rewritten 均不能为空"})
		return
	}
	rw := s.newAutoRewriter()
	geu, err := rw.CheckGEU(r.Context(), body.Original, body.Rewritten)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, geu)
}

// handleReadinessCIGate 处理 AI 就绪度 CI 门禁判定请求。
//
// POST /api/v1/brand/readiness/ci-gate  JSON {"url": "example.com", "threshold": 80}
//
// 先执行 8 维就绪审计，再按 threshold（默认 60）判定门禁是否通过。
// 返回 readiness.CIGateResult，含 blocking_issues 与人类可读汇总。
// CI/CD 集成时可直接根据 passed 字段决定流水线是否中断。
func (s *Server) handleReadinessCIGate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		URL       string  `json:"url"`
		Domain    string  `json:"domain"` // 与前端 BrandProfile.domain 对齐的别名
		Threshold float64 `json:"threshold"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	threshold := readiness.DefaultCIThreshold()
	if body.Threshold > 0 {
		threshold = body.Threshold
	}
	rawURL := body.URL
	if rawURL == "" {
		rawURL = body.Domain
	}
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "url 不能为空"})
		return
	}
	result, err := readiness.Audit(r.Context(), rawURL)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	gate := readiness.CIGateReportWithThreshold(result, threshold)
	writeJSON(w, http.StatusOK, gate)
}

// handleBrandDiscover 关键词→公司推断搜索。
// POST /api/v1/brand/discover
// 请求体: {"keyword":"短视频"}
// 响应: {"keyword":"短视频","candidates":[...]}
func (s *Server) handleBrandDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Keyword string `json:"keyword"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Keyword) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "keyword 不能为空"})
		return
	}

	var offlineDB offlinedb.DB
	if s.brandEngine != nil {
		offlineDB = s.brandEngine.OfflineDB()
	}
	var kb *knowledge.Knowledge
	if s.brandEngine != nil {
		kb = s.brandEngine.Knowledge()
	}

	result, err := discover.Discover(r.Context(), body.Keyword, offlineDB, kb)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleBrandDiscoverReport 基于选中的公司候选生成完整 GEO 报告。
// POST /api/v1/brand/discover/report
// 请求体: {"candidate":{...},"keyword":"短视频"}
// 响应: 完整 GEOReport（品牌画像 + 审计 + 就绪度 + 建议）
func (s *Server) handleBrandDiscoverReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Candidate discover.Candidate `json:"candidate"`
		Keyword   string             `json:"keyword"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(body.Candidate.Name) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "candidate.name 不能为空"})
		return
	}

	report, err := discover.GenerateReport(r.Context(), &body.Candidate, body.Keyword, s.brandEngine)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
