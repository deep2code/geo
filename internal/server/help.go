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
	Summary  string `json:"summary"`  // 列表页摘要
	Content  string `json:"content"`
	Category string `json:"category"` // quickstart/audit/features/report/settings/faq
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
		// ───────────── 快速开始 ─────────────
		{
			ID:       "overview",
			Title:    "平台功能总览",
			Summary:  "一张图看懂 MyGEO 能为你做什么",
			Category: "quickstart",
			Order:    1,
			Content:  "MyGEO 是面向「生成式引擎优化（GEO）」的平台：让品牌在 ChatGPT、文心、通义、DeepSeek 等 AI 回答中被正确提及与引用。\n\n核心模块一览：\n\n- 品牌管理：维护品牌画像（名称、域名、行业、提示词、竞品）\n- 品牌审计：模拟真实提问，检测各 AI 引擎的提及率、引用率、情感与位置，得出 BVS 分数\n- 内容优化器：针对单篇内容给出提升 AI 可见度的具体建议\n- 关键词发现：从种子词拓展高价值提问词\n- 竞品对标：与竞品横向对比 AI 可见度\n- 引擎来源研究：统计每个大模型更常引用哪些来源（如评测站、文档、社媒）\n- 外部提交分析：采集真实用户与 AI 的对话，定时抽取情感/主题/来源，反哺优化\n- 报告导出 / 告警邮件：分享与持续监控\n- 管理后台：租户、用量、系统设置、外部提交等运维能力\n\n建议新用户按「快速开始」四步走，15 分钟内跑通首次审计。",
		},
		{
			ID:       "getting-started",
			Title:    "5 分钟快速开始",
			Summary:  "创建品牌 → 执行审计 → 查看报告",
			Category: "quickstart",
			Order:    2,
			Content:  "只需四步即可完成首次 AI 可见度审计：\n\n## 1. 创建品牌\n进入「品牌管理」，点击「新建品牌」，填写品牌名称、官网域名与行业。创建后系统生成基础画像。\n\n## 2. 配置查询词（Prompts）\n为品牌补充业务相关的提问词，例如「XX 品牌值得买吗」「XX 和 YY 哪个好」。这些是 AI 用户最可能问的问题。\n\n## 3. 执行审计\n进入「品牌审计」，选择品牌与引擎，点击「开始审计」。系统模拟用户提问，1-3 分钟内给出结果。\n\n## 4. 查看与导出报告\n在「报告导出」查看 BVS 分数、各引擎对比与优化建议，可导出 HTML / PDF 或邮件发送。\n\n提示：开启「联网搜索」可让 AI 引用你的官网与内容；开启「多次采样」可得到更稳定的结论。",
		},

		// ───────────── 品牌审计 ─────────────
		{
			ID:       "run-audit",
			Title:    "执行品牌审计",
			Summary:  "模拟真实提问，检测 AI 中的品牌可见度",
			Category: "audit",
			Order:    3,
			Content:  "在「品牌审计」页面选择品牌与目标引擎（如 GLM、通义千问、Doubao、文心、DeepSeek 等），点击「开始审计」。\n\n系统会针对每个提示词，模拟真实用户的提问，检测品牌在各大 AI 引擎中的：\n\n- 提及率（是否被提到）\n- 引用率（回答是否引用了你的内容/官网）\n- 引用位置（出现在第几条、是否靠前）\n- 情感倾向（正面 / 中性 / 负面）\n\n最终汇总为 BVS（Brand Visibility Score）分数与各引擎对比。\n\n支持定时审计：在调度器配置 cron 表达式，实现每日 / 每周自动审计。",
		},
		{
			ID:       "understand-bvs",
			Title:    "理解 BVS 分数",
			Summary:  "BVS 评分维度与 A-F 等级说明",
			Category: "audit",
			Order:    4,
			Content:  "BVS（Brand Visibility Score）是品牌 AI 可见度的综合评分，范围 0-100，对应 A-F 等级：\n\n- A（90-100）：品牌在 AI 引擎中表现卓越\n- B（80-89）：表现良好\n- C（70-79）：表现中等，有提升空间\n- D（60-69）：表现较弱\n- F（<60）：急需优化\n\nBVS 由提及率、引用率、引用位置、情感分、实体识别等多个子维度加权计算，并区分「被提及」与「被引用」——被引用（内容真正进入 AI 回答）价值远高于仅被提及。",
		},
		{
			ID:       "web-search",
			Title:    "联网搜索：让 AI 引用你的内容",
			Summary:  "开启联网后，审计会模拟真实联网检索",
			Category: "audit",
			Order:    5,
			Content:  "各 AI 引擎大多具备联网搜索能力。开启「联网搜索」后，审计会模拟真实联网检索场景：AI 在回答时可能引用你的官网、博客、新闻等公开页面。\n\n这能更真实地反映「用户开启联网时，你的品牌是否被引用」，是 GEO 优化的关键信号。\n\n不同引擎的联网接口不同（OpenAI 系用 tools web_search、通义用 enable_search、文心用请求体 web_search、DeepSeek 用 Responses API），系统已内置适配；只需在服务端正确配置各引擎 API Key 即可。",
		},
		{
			ID:       "multi-sampling",
			Title:    "多次采样与一致性",
			Summary:  "重复提问取多数票，结论更稳定",
			Category: "audit",
			Order:    6,
			Content:  "单次提问可能因模型随机性得到不稳定结论。开启「多次采样」（GEO_AUDIT_SAMPLES，默认 1）后，系统对每个提示词重复提问 N 次，按多数票判定「是否被提及 / 引用」，并给出一致性指标 Consistency。\n\n- MentionVotes：被提及的票数\n- Consistency：有效样本中被提及的比例\n\n一致性过低时，说明该问题答案不稳定，可结合人工修正进一步校准。",
		},
		{
			ID:       "manual-correction",
			Title:    "人工修正判定结果",
			Summary:  "对误判的提及/引用/情感就地修正并留痕",
			Category: "audit",
			Order:    7,
			Content:  "AI 判定并非百分百准确。在「品牌审计」结果中，点击任意一条结果的「✏️ 修正」，可手动调整：\n\n- 是否被提及（mentioned）\n- 是否被引用（cited）\n- 情感倾向（positive / neutral / negative）\n- 引用位置（position）\n\n提交后会就地更新该条记录并自动全量重算报告，同时保留修正前后的留痕（correction.prev_*、修正人、时间、理由），保证审计可追溯。所有修正需管理后台「数据管理」权限。",
		},
		{
			ID:       "engine-source-study",
			Title:    "引擎来源研究：谁在引用你的内容",
			Summary:  "统计每个大模型更常采用哪些来源",
			Category: "audit",
			Order:    8,
			Content:  "「🧠 引擎来源研究」Tab 记录并分析：每个大模型（引擎）在回答中更常引用哪些来源域名，以及其分类（评测站 / 技术文档 / 社交问答 / 新闻 / 博客 / 视频 / 其他）。\n\n它能回答：\n\n- 我的内容被哪些引擎引用？引用占比多少？\n- 竞品的内容主要在哪些来源被引用？\n- 近 30 / 90 / 180 天引用趋势如何？\n\n数据随每次审计自动沉淀（append-only 历史），可按引擎、品牌、域名下钻查看趋势，是制定「内容该发到哪」策略的依据。",
		},

		// ───────────── 功能详解 ─────────────
		{
			ID:       "external-submissions",
			Title:    "外部提交分析：采集真实用户对话",
			Summary:  "外部系统提交 AI 对话，定时抽取洞察",
			Category: "features",
			Order:    9,
			Content:  "「📥 外部提交分析」（管理后台）用于把真实世界里用户与大模型的对话采集进来：外部系统通过接口提交「大模型名称 / 问题 / 回答 / 会话分享链接」，系统入库后由后台定时任务抽取：\n\n- 情感倾向（正面 / 中性 / 负面）\n- 主题分类\n- 中文摘要\n- 被提及的实体\n- 回答中引用的来源域名及分类\n\n在管理后台的「外部提交分析」Tab 可查看总数 / 待分析 / 已分析统计，按状态过滤，查看每条详情，也可点击「立即分析」手动触发一轮抽取。该能力帮助你把真实用户对话转化为 GEO 优化洞察。\n\n接口需通过专用外部密钥（GEO_EXTERNAL_API_KEY，请求头 X-GEO-External-Key）鉴权。",
		},
		{
			ID:       "content-optimizer",
			Title:    "内容优化器",
			Summary:  "针对单篇内容给出 AI 可见度优化建议",
			Category: "features",
			Order:    10,
			Content:  "在「内容优化器」粘贴或输入一篇内容（网页 / 文章），选择目标引擎与领域类型，系统会基于 GEO 规则与 AI 评判，给出提升「被 AI 引用概率」的具体修改建议：结构、实体清晰度、引用信号、可抓取性等。\n\n适合在发布前对重点内容进行预检与打磨。",
		},
		{
			ID:       "keyword-discovery",
			Title:    "关键词发现",
			Summary:  "从种子词拓展高价值提问词",
			Category: "features",
			Order:    11,
			Content:  "在「关键词发现」输入若干种子关键词，系统会拓展出用户更可能向 AI 提出的提问词（自然语言问题），用于补充品牌审计的 Prompts，覆盖更多真实检索场景。\n\n支持按市场（market）与语言（language）配置，结果可一键加入某品牌的提示词库。",
		},
		{
			ID:       "competitor-benchmark",
			Title:    "竞品对标",
			Summary:  "与竞品横向对比 AI 可见度",
			Category: "features",
			Order:    12,
			Content:  "在「竞品对标」选择你的品牌与多个竞品，系统生成对标矩阵：\n\n- 各品牌 BVS 分数对比\n- 各引擎表现对比\n- 优势 / 劣势维度分析\n\n支持导出对标报告（HTML / JSON），帮助你在 AI 可见度上找准超越竞品的发力点。",
		},
		{
			ID:       "rules-evaluate",
			Title:    "规则集与评测",
			Summary:  "内置 GEO 规则，离线评测内容质量",
			Category: "features",
			Order:    13,
			Content:  "「规则集」是一组可校验的 GEO 内容规则（如实体清晰度、引用密度、结构信号）。「评测」可用一份数据集（Markdown / JSON）批量跑分，验证内容在各项规则上的表现，适合把 GEO 标准纳入团队的发布前检查（CI）。\n\n规则可用内置默认集，也可在「系统设置」中查看与维护。",
		},
		{
			ID:       "offline-db",
			Title:    "离线工商库",
			Summary:  "导入企业工商信息，丰富品牌画像",
			Category: "features",
			Order:    14,
			Content:  "「离线工商库」可导入企业工商注册信息（支持上传 JSON 或直连 GitHub 数据集），用于丰富品牌画像与实体核验（如公司主体、地域、行业）。\n\n数据存储在独立 MySQL 库（GEO_OFFLINE_MYSQL_DSN），与审计主库隔离，便于大规模导入与管理。",
		},
		{
			ID:       "external-signals",
			Title:    "外部信号监测",
			Summary:  "监测品牌在外部的可见度信号",
			Category: "features",
			Order:    15,
			Content:  "「外部信号监测」针对品牌官网域名，采集其在外部生态中的关键信号（如索引情况、引用来源、社媒提及等），作为 AI 可见度评分的补充输入。\n\n可与品牌审计结果结合，形成更完整的可见度视图。",
		},

		// ───────────── 报告 ─────────────
		{
			ID:       "export-report",
			Title:    "导出与分享报告",
			Summary:  "HTML / PDF 在线报告与邮件发送",
			Category: "report",
			Order:    16,
			Content:  "在「报告导出」页面可以：\n\n- 查看 HTML 在线报告（可打印为 PDF）\n- 下载完整 PDF 报告\n- 通过邮件发送报告给团队成员\n\n报告包含品牌可见度分数、各引擎对比、趋势图表、内容缺口与优化建议等完整内容，便于向管理层或客户汇报。",
		},

		// ───────────── 系统设置 ─────────────
		{
			ID:       "api-keys",
			Title:    "配置 AI 引擎 API Key",
			Summary:  "在服务端环境变量配置各引擎密钥",
			Category: "settings",
			Order:    17,
			Content:  "MyGEO 需要各 AI 引擎的 API Key 才能发起审计。请在服务端环境变量中配置，例如：\n\n- GEO_CHATGPT_KEY\n- GEO_CLAUDE_KEY\n- GEO_PERPLEXITY_KEY\n- GEO_GEMINI_KEY\n- GEO_QWEN_KEY\n- GEO_GLM_KEY\n- GEO_DEEPSEEK_KEY\n- GEO_WENXIN_KEY（文心）\n\n在「系统设置 > API」页面可查看各引擎的配置状态（已配置 / 未配置）。未配置的引擎在审计时将自动跳过。",
		},
		{
			ID:       "setup-alert-email",
			Title:    "设置告警邮件",
			Summary:  "分数下降与异常时自动通知",
			Category: "settings",
			Order:    18,
			Content:  "在「告警邮件」页面配置 SMTP 邮件服务器，设置告警规则：\n\n- 品牌分数下降超过阈值时告警\n- 定时发送周报 / 月报\n- 模型分歧异常告警（同一品牌在不同引擎中结果差异过大）\n\n支持配置多个收件人，支持白标邮件模板。",
		},
		{
			ID:       "system-settings",
			Title:    "系统设置（数据库配置）",
			Summary:  "通过管理后台修改运行时配置",
			Category: "settings",
			Order:    19,
			Content:  "「系统设置」Tab 集中管理运行时配置，存储于 MySQL app_settings，读取顺序为：数据库 > 环境变量 > 默认值。\n\n常见可配置项：审计采样次数、各模块数据库连接开关、白标信息、默认语言等。修改即时生效（标注「需重启」的项除外）。引导类变量（如数据库 DSN、初始管理员）只能通过环境变量设置，此处只读。",
		},
		{
			ID:       "admin-overview",
			Title:    "管理后台概览",
			Summary:  "租户、用量、系统信息与外部提交",
			Category: "settings",
			Order:    20,
			Content:  "「管理后台」面向运维与运营，包含：\n\n- 租户管理：查看各租户品牌数 / 审计次数 / 邮件数，启停租户\n- 用量统计：总品牌、总审计、活跃租户等\n- 公告管理：发布系统通知 / 功能更新 / 维护公告\n- 系统信息：构建版本、Go 版本、资源监控\n- 系统设置：运行时配置（见上文）\n- 外部提交分析：查看与触发外部对话的分析（见功能详解）\n\n进入管理后台需在「管理员登录」配置管理员 Key（GEO_ADMIN_KEY 或账号体系 PermManageData 权限）。",
		},

		// ───────────── 常见问题 ─────────────
		{
			ID:       "faq",
			Title:    "常见问题",
			Summary:  "审计时长、数据存储、引擎支持等",
			Category: "faq",
			Order:    21,
			Content:  "Q: 审计需要多长时间？\nA: 单次审计通常 1-3 分钟，取决于引擎数量与提示词数量；开启多次采样会相应增加。\n\nQ: 数据存储在哪里？\nA: 审计历史、来源研究、外部提交等分别存储在 MySQL（DSN 通过 GEO_HISTORY_MYSQL_DSN、GEO_SOURCE_MYSQL_DSN、GEO_EXTERNAL_MYSQL_DSN 等配置）。\n\nQ: 支持哪些 AI 引擎？\nA: 支持 GLM、通义千问、Doubao、文心一言、DeepSeek、ChatGPT、Claude、Gemini、Perplexity 等主流引擎，按服务端配置的 API Key 启用。\n\nQ: 如何获取 API 访问权限？\nA: 专业版及以上方案支持 API 访问，在「系统设置 / 管理员登录」获取对应 Key。\n\nQ: 联网搜索为什么有时没效果？\nA: 需确认对应引擎已正确配置 API Key 且支持联网；若接口返回错误，系统会自动降级为无网查询并在日志提示。",
		},
	}
}

// onboardingSteps 新手引导步骤定义（与前端步骤编号一致，用于持久化完成状态）。
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
