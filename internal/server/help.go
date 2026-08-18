package server

import (
	"cmp"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// HelpArticle 帮助文章。
type HelpArticle struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"` // getting-started/audit/reports/settings/faq
	Order    int    `json:"order"`
}

// OnboardingStep 新手引导步骤。
type OnboardingStep struct {
	Step      int    `json:"step"`
	Title     string `json:"title"`
	Desc      string `json:"desc"`
	Action    string `json:"action"`    // action 类型：create-brand/run-audit/view-report/setup-alert
	Route     string `json:"route"`     // 前端路由
	Completed bool   `json:"completed"` // 是否已完成
}

// 帮助文章内存存储（init 初始化，只读）。
var helpArticles []HelpArticle

// 新手引导完成状态（内存存储，进程重启清空）。
var (
	onboardingMu   sync.Mutex
	onboardingDone = map[int]bool{}
)

func init() {
	helpArticles = []HelpArticle{
		{
			ID:       "getting-started",
			Title:    "快速开始",
			Category: "getting-started",
			Order:    1,
			Content:  "欢迎使用 GEO 品牌 AI 可见度优化平台。本指南帮助你快速上手：\n\n1. 在「品牌管理」中创建你的第一个品牌\n2. 在「品牌审计」中执行 AI 可见度审计\n3. 在「报告导出」中查看并导出审计报告\n4. 在「告警邮件」中配置定期告警\n\n整个流程只需 5 分钟即可完成首次审计。",
		},
		{
			ID:       "create-brand",
			Title:    "创建品牌",
			Category: "getting-started",
			Order:    2,
			Content:  "在「品牌管理」页面点击「新建品牌」，填写品牌名称、官网域名、行业类型等基础信息。\n\n支持多品牌管理：免费版最多 1 个品牌，专业版最多 10 个，企业版不限。\n\n创建品牌后，系统会自动生成品牌画像，可在此基础上补充提示词、竞品等细节。",
		},
		{
			ID:       "run-audit",
			Title:    "执行品牌审计",
			Category: "audit",
			Order:    3,
			Content:  "在「品牌审计」页面选择品牌与审计引擎（GLM/通义千问/Doubao 等），点击「开始审计」。\n\n系统会模拟真实用户的提问，检测品牌在各大 AI 引擎中的提及率、引用率、情感倾向等维度，生成 BVS（Brand Visibility Score）分数。\n\n支持定时审计：在调度器中配置 cron 表达式，实现每日/每周自动审计。",
		},
		{
			ID:       "understand-bvs",
			Title:    "理解 BVS 分数",
			Category: "audit",
			Order:    4,
			Content:  "BVS（Brand Visibility Score）是品牌 AI 可见度的综合评分，范围 0-100，对应 A-F 等级：\n\n- A（90-100）：品牌在 AI 引擎中表现卓越\n- B（80-89）：表现良好\n- C（70-79）：表现中等，有提升空间\n- D（60-69）：表现较弱\n- F（<60）：急需优化\n\nBVS 由提及率、引用率、引用位置、情感分、实体识别等多个子维度加权计算。",
		},
		{
			ID:       "export-report",
			Title:    "导出报告",
			Category: "reports",
			Order:    5,
			Content:  "在「报告导出」页面可以：\n\n- 查看 HTML 在线报告（可打印为 PDF）\n- 下载完整 PDF 报告\n- 通过邮件发送报告给团队成员\n\n报告包含品牌可见度分数、各引擎对比、趋势图表、优化建议等完整内容。",
		},
		{
			ID:       "setup-alert-email",
			Title:    "设置告警邮件",
			Category: "settings",
			Order:    6,
			Content:  "在「告警邮件」页面配置 SMTP 邮件服务器，设置告警规则：\n\n- 品牌分数下降超过阈值时告警\n- 定时发送周报/月报\n- 模型分歧异常告警（同一品牌在不同引擎中结果差异过大）\n\n支持配置多个收件人，支持白标邮件模板。",
		},
		{
			ID:       "competitor-benchmark",
			Title:    "竞品对标",
			Category: "audit",
			Order:    7,
			Content:  "在「竞品对标」页面选择你的品牌与多个竞品，系统会生成对标矩阵：\n\n- 各品牌 BVS 分数对比\n- 各引擎表现对比\n- 优势/劣势维度分析\n\n支持导出对标报告（HTML/JSON），帮助你在 AI 可见度上超越竞品。",
		},
		{
			ID:       "faq",
			Title:    "常见问题",
			Category: "faq",
			Order:    8,
			Content:  "Q: 审计需要多长时间？\nA: 单次审计通常 1-3 分钟，取决于引擎数量与提示词数量。\n\nQ: 数据存储在哪里？\nA: 审计历史存储在 MySQL 数据库，DSN 可通过 GEO_HISTORY_MYSQL_DSN 等环境变量配置。\n\nQ: 支持哪些 AI 引擎？\nA: 支持 GLM、通义千问、Doubao、文心一言、DeepSeek 等主流中文 AI 引擎。\n\nQ: 如何获取 API 访问权限？\nA: 专业版及以上方案支持 API 访问，联系客服获取 API Key。",
		},
	}
}

// onboardingSteps 新手引导步骤定义。
var onboardingSteps = []OnboardingStep{
	{Step: 1, Title: "创建你的第一个品牌", Desc: "添加品牌名称与基础信息，开始 AI 可见度管理", Action: "create-brand", Route: "/brand-management"},
	{Step: 2, Title: "执行品牌审计", Desc: "选择引擎并运行首次 AI 可见度审计", Action: "run-audit", Route: "/brand-audit"},
	{Step: 3, Title: "查看审计报告", Desc: "查看 BVS 分数与各维度分析，导出报告", Action: "view-report", Route: "/report-export"},
	{Step: 4, Title: "设置告警邮件", Desc: "配置 SMTP 并开启分数下降告警与周报", Action: "setup-alert", Route: "/alert-email"},
}

// handleHelpArticles 帮助文章列表（支持 ?category= 过滤）。
// GET /api/v1/help/articles
func (s *Server) handleHelpArticles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	var list []HelpArticle
	for _, a := range helpArticles {
		if category != "" && a.Category != category {
			continue
		}
		list = append(list, a)
	}
	// 按 Order 升序
	slices.SortFunc(list, func(a, b HelpArticle) int { return cmp.Compare(a.Order, b.Order) })
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":    len(list),
		"articles": list,
	})
}

// handleHelpArticleDetail 文章详情。
// GET /api/v1/help/articles/{id}
func (s *Server) handleHelpArticleDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/help/articles/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少文章 ID"})
		return
	}
	for _, a := range helpArticles {
		if a.ID == id {
			writeJSON(w, http.StatusOK, a)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "文章不存在"})
}

// handleHelpOnboarding 获取新手引导步骤（含完成状态）。
// GET /api/v1/help/onboarding
func (s *Server) handleHelpOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	onboardingMu.Lock()
	steps := make([]OnboardingStep, len(onboardingSteps))
	for i, st := range onboardingSteps {
		st.Completed = onboardingDone[st.Step]
		steps[i] = st
	}
	onboardingMu.Unlock()
	completed := 0
	for _, st := range steps {
		if st.Completed {
			completed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":     len(steps),
		"completed": completed,
		"steps":     steps,
	})
}

// handleHelpOnboardingComplete 标记步骤完成。
// POST /api/v1/help/onboarding/complete  body: {"step": 1}
func (s *Server) handleHelpOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Step int `json:"step"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if body.Step < 1 || body.Step > len(onboardingSteps) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "step 超出范围",
			Code:  "INVALID_STEP",
		})
		return
	}
	onboardingMu.Lock()
	onboardingDone[body.Step] = true
	onboardingMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"step": body.Step,
	})
}
