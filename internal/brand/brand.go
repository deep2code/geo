// Package brand 提供企业/品牌/产品在 AI 搜索引擎中的可见度评分与报告。
//
// 与 pkg/geo（面向单篇内容优化）互补，本包面向品牌实体：
// 输入品牌信息+竞品+查询词，输出品牌可见度评分与运营行动报告。
//
// 用法：
//
//	engine := brand.New(brand.WithEngine(chatgptAdapter), brand.WithEngine(glmAdapter))
//	report, err := engine.Audit(ctx, brand.BrandProfile{
//	    Name: "Acme",
//	    Domain: "acme.com",
//	    Prompts: []string{"best CRM tools", "top project management software"},
//	    Competitors: []brand.Competitor{{Name: "CompetitorA"}},
//	})
package brand

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"my-geo/internal/adapter"
	"my-geo/internal/brand/chinacheck"
	"my-geo/internal/brand/crawler"
	"my-geo/internal/brand/history"
	"my-geo/internal/brand/knowledge"
	"my-geo/internal/brand/market"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/roi"
	"my-geo/internal/llm"
	"my-geo/internal/models"
)

// Engine 品牌可见度评估引擎。
type Engine struct {
	monitor    *Monitor
	scorer     *Scorer
	reporter   *Reporter
	llmMgr     *llm.Manager
	kb         *knowledge.Knowledge
	chinaCheck *chinacheck.Client      // 可选：工商注册实时核验（GSXT / SAMR，免鉴权免费）
	offlineDB  offlinedb.DB            // 可选：1978-2019 离线工商注册库（接口，多后端）
	historyDB  history.DB              // 可选：审计历史时间序列库（接口，多后端）
	crawler    *crawler.WebsiteCrawler // 可选：官网爬虫（默认自动初始化，无需外部配置）
	roiTracker *roi.Tracker            // token 用量与成本追踪（默认自动初始化）
	// configuredEngines 记录哪些引擎已配置真实 API Key。
	configuredEngines map[models.EngineType]bool
}

// Option Engine 配置选项。
type Option func(*Engine)

// WithEngine 添加一个引擎适配器。
func WithEngine(a adapter.Adapter) Option {
	return func(e *Engine) {
		e.monitor.adapters[a.Engine()] = a
		if a.Configured() {
			e.configuredEngines[a.Engine()] = true
		}
	}
}

// WithAdapters 批量添加引擎适配器。
func WithAdapters(adapters map[models.EngineType]adapter.Adapter) Option {
	return func(e *Engine) {
		for eng, a := range adapters {
			e.monitor.adapters[eng] = a
			if a.Configured() {
				e.configuredEngines[eng] = true
			}
		}
	}
}

// WithLLM 注入 LLM 管理器（用于品牌智能补全）。
func WithLLM(mgr *llm.Manager) Option {
	return func(e *Engine) { e.llmMgr = mgr }
}

// WithKnowledge 注入品牌知识库（默认会自动从内嵌数据集加载）。
func WithKnowledge(kb *knowledge.Knowledge) Option {
	return func(e *Engine) { e.kb = kb }
}

// WithChinaCheck 注入工商核验客户端（China-Check MCP，GSXT/SAMR 实时查询，免鉴权免费）。
// 未注入时 Autocomplete 跳过工商核验步骤，不影响其他功能。
func WithChinaCheck(cc *chinacheck.Client) Option {
	return func(e *Engine) { e.chinaCheck = cc }
}

// WithOfflineDB 注入离线工商注册存储（默认后端 MySQL（FULLTEXT ngram）；1978-2019 种子数据）。
// 未注入时 Autocomplete / 知识库联想跳过离线库。
func WithOfflineDB(db offlinedb.DB) Option {
	return func(e *Engine) { e.offlineDB = db }
}

// OfflineDB 返回离线工商存储接口（可能为 nil）。
func (e *Engine) OfflineDB() offlinedb.DB { return e.offlineDB }

// WithHistoryDB 注入审计历史时间序列存储（默认后端 MySQL）。
// 未注入时 Audit 结果不持久化（仅内存返回）。
func WithHistoryDB(db history.DB) Option {
	return func(e *Engine) { e.historyDB = db }
}

// HistoryDB 返回审计历史存储接口（可能为 nil）。
func (e *Engine) HistoryDB() history.DB { return e.historyDB }

// WithCrawler 注入官网爬虫（默认在 New() 中自动初始化，无需手动注入）。
// 调用方可传入自定义配置（如自定义 http.Client）的爬虫实例覆盖默认。
func WithCrawler(c *crawler.WebsiteCrawler) Option {
	return func(e *Engine) { e.crawler = c }
}

// Crawler 返回当前官网爬虫实例（可能为 nil）。
func (e *Engine) Crawler() *crawler.WebsiteCrawler { return e.crawler }

// ROITracker 返回 token 用量与成本追踪器（默认已初始化，不会为 nil）。
func (e *Engine) ROITracker() *roi.Tracker { return e.roiTracker }

// New 创建品牌可见度评估引擎。
//
// 默认自动加载内嵌的 SinoFacts 知识库（CC BY 4.0），
// 加载失败不影响其他功能（Autocomplete 会退化为纯 LLM 模式）。
// China-Check 默认不启用，如需工商实时核验需显式 WithChinaCheck。
// 官网爬虫默认自动初始化（无需外部配置），可通过 WithCrawler 覆盖。
func New(opts ...Option) *Engine {
	e := &Engine{
		monitor:           NewMonitor(nil),
		scorer:            NewScorer(),
		reporter:          NewReporter(),
		configuredEngines: map[models.EngineType]bool{},
		crawler:           crawler.New(),
		roiTracker:        roi.NewTracker(),
	}
	for _, opt := range opts {
		opt(e)
	}
	// 将 ROI 追踪器注入 Monitor，自动记录每次引擎查询的 token 用量
	e.monitor = e.monitor.WithROITracker(e.roiTracker)
	if e.kb == nil {
		if kb, err := knowledge.Load(); err == nil {
			e.kb = kb
		}
	}
	return e
}

// Knowledge 返回当前使用的知识库。
func (e *Engine) Knowledge() *knowledge.Knowledge { return e.kb }

// ChinaCheck 返回当前工商核验客户端（可能为 nil）。
func (e *Engine) ChinaCheck() *chinacheck.Client { return e.chinaCheck }

// Close 释放引擎持有的资源（离线库、历史库等连接）。
// 应在服务退出时调用，避免连接泄漏。
func (e *Engine) Close() {
	if e.offlineDB != nil {
		_ = e.offlineDB.Close()
	}
	if e.historyDB != nil {
		_ = e.historyDB.Close()
	}
}

// entryToCandidate 将知识库 Entry 转换为品牌引擎 AutocompleteCandidate。
// 放在 brand 包内，knowledge 保持 leaf 无循环依赖。
func entryToCandidate(e *knowledge.Entry) *AutocompleteCandidate {
	c := &AutocompleteCandidate{
		Name:     e.BrandName,
		Domain:   e.BrandDomain,
		Aliases:  append([]string{}, e.BrandAliases...),
		Industry: e.Industry,
		Category: e.Category,
		Products: append([]string{}, e.Products...),
		Summary:  e.DescriptionZh,
	}
	c.Company = &Company{
		Name:         e.CompanyName,
		Aliases:      append([]string{}, e.CompanyAliases...),
		Domain:       e.CompanyDomain,
		Description:  e.CompanyDescription,
		Industry:     e.CompanyIndustry,
		Headquarters: e.Headquarters,
		FoundedYear:  e.FoundedYear,
	}
	c.Prompts = defaultPromptsForEntry(e.Industry, e.Category, e.BrandName)
	return c
}
func defaultPromptsForEntry(industry, category, brandName string) []string {
	prompts := []string{}
	if industry != "" {
		prompts = append(prompts,
			fmt.Sprintf("推荐几个%s行业的知名厂商", industry),
			fmt.Sprintf("最好的%s公司", industry),
			fmt.Sprintf("%s怎么样？", brandName),
			fmt.Sprintf("%s产品介绍", brandName),
		)
	}
	if category != "" {
		cat := category
		prompts = append(prompts,
			fmt.Sprintf("%s软件哪个好？", cat),
			fmt.Sprintf("推荐几款好用的%s工具", cat),
			fmt.Sprintf("%s和竞品对比", brandName),
			fmt.Sprintf("%s和其他同类产品有什么区别", brandName),
			fmt.Sprintf("%s和类似的公司有哪些", brandName),
		)
	} else {
		prompts = append(prompts,
			fmt.Sprintf("%s怎么样？", brandName),
			fmt.Sprintf("%s产品介绍", brandName),
			fmt.Sprintf("和%s类似的公司", brandName),
		)
	}
	// 去重
	seen := map[string]bool{}
	out := make([]string, 0, len(prompts))
	for _, p := range prompts {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Audit 执行品牌可见度审计，生成完整报告。
func (e *Engine) Audit(ctx context.Context, profile BrandProfile) (*VisibilityReport, error) {
	if profile.Name == "" {
		return nil, fmt.Errorf("品牌名称不能为空")
	}
	if len(profile.Prompts) == 0 {
		return nil, fmt.Errorf("至少需要一个查询词（prompts）")
	}

	// 0. 多语言/多市场审计（#8）：若指定了非中文语言，先将查询词本地化。
	//    LLM 可用时优先用 LLM 翻译，否则退化为预设映射表（best-effort）。
	//    翻译失败不阻塞主流程，保留原查询词继续审计。
	if profile.Language != "" && profile.Language != "zh" {
		if localized, err := market.LocalizePrompts(ctx, profile.Prompts, profile.Language, e.llmMgr); err == nil && len(localized) > 0 {
			profile.Prompts = localized
		}
	}

	// 1. 监控：查询各引擎并检测信号
	results, err := e.monitor.Run(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("监控执行失败: %w", err)
	}

	// 2. 评分：聚合统计并计算 BVS（公司信息完备度纳入实体识别维度）
	stats := e.scorer.Aggregate(results, profile, e.configuredEngines)
	entCompleteness := EntityCompleteness(profile)
	score, grade, tier, breakdown := e.scorer.ScoreWithProfile(stats, &profile, entCompleteness)

	// 3. 报告：生成运营行动建议
	report := e.reporter.Build(profile, results, stats, score, grade, tier, breakdown)

	// 4. 时间序列持久化（可选，未注入 historyDB 时跳过）
	if e.historyDB != nil {
		e.saveHistory(ctx, &report)
	}

	return &report, nil
}

// saveHistory 将审计报告写入历史库（best-effort，失败记录 warning 日志不影响审计结果）。
func (e *Engine) saveHistory(ctx context.Context, report *VisibilityReport) {
	reportJSON, err := history.MarshalReport(report)
	if err != nil {
		slog.Warn("审计报告序列化失败，跳过历史写入",
			slog.String("brand", report.BrandName), slog.String("err", err.Error()))
		return
	}
	if _, err := e.historyDB.Save(ctx, history.Record{
		BrandName:          report.BrandName,
		Generated:          report.GeneratedAt.Unix(),
		Score:              report.Score,
		Grade:              report.Grade,
		Tier:               report.Tier,
		EntityCompleteness: report.EntityCompletenessScore,
		MentionRate:        report.ScoreBreakdown.MentionRate,
		CitationRate:       report.ScoreBreakdown.CitationRate,
		ShareOfVoice:       report.ScoreBreakdown.ShareOfVoice,
		CitationPosition:   report.ScoreBreakdown.CitationPosition,
		Sentiment:          report.ScoreBreakdown.Sentiment,
		EntityRecognition:  report.ScoreBreakdown.EntityRecognition,
		ContentGaps:        len(report.ContentGaps),
		CompetitorCount:    len(report.CompetitorSOV),
		NegativeCount:      len(report.NegativeMentions),
		ActionCount:        len(report.Actions),
		ReportJSON:         reportJSON,
	}); err != nil {
		slog.Warn("审计历史写入失败",
			slog.String("brand", report.BrandName), slog.String("err", err.Error()))
	}
}

// AutocompleteRequest 品牌智能补全请求。
type AutocompleteRequest struct {
	BrandName string `json:"brand_name"`
}

// AutocompleteCandidate 品牌智能补全候选画像。
type AutocompleteCandidate struct {
	Name     string   `json:"name"`
	Domain   string   `json:"domain,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	Industry string   `json:"industry,omitempty"`
	Category string   `json:"category,omitempty"`
	Products []string `json:"products,omitempty"`
	// 关联公司信息（母公司/集团）。
	Company     *Company     `json:"company,omitempty"`
	Competitors []Competitor `json:"competitors,omitempty"`
	Prompts     []string     `json:"prompts,omitempty"`
	// 搜索摘要：AI 引擎返回的品牌描述。
	Summary string `json:"summary,omitempty"`
}

// Autocomplete 品牌智能补全：输入品牌名→知识库匹配→联网搜索→LLM分析→返回候选画像。
//
// 流程（优先离线，再联网；多层降级，保证零配置也能出结果）：
//  1. 本地知识库（SinoFacts CC BY 4.0，383 家中国软件公司）精确/模糊搜索
//     → 命中 Top1 分数 ≥60 且 无 LLM：直接返回离线候选
//  2. 联网搜索补充信息（公司/竞品等）
//     → 无联网但有高置信知识库候选：直接返回
//  3. LLM 结构化分析（联网结果 + 知识库候选一起传入，LLM 决定是否采纳）
//     → LLM 失败：退回知识库候选
func (e *Engine) Autocomplete(ctx context.Context, brandName string) (*AutocompleteCandidate, error) {
	if brandName == "" {
		return nil, fmt.Errorf("品牌名称不能为空")
	}

	var kbContext string
	var kbCandidate *AutocompleteCandidate
	var kbSource string
	// 步骤 1：本地知识库搜索（零依赖、速度快、数据经过事实核验）
	if e.kb != nil {
		// 若用户输入形似域名，直接域名查找
		if strings.Contains(brandName, ".") {
			if ent := e.kb.LookupByDomain(brandName); ent != nil {
				kbCandidate = entryToCandidate(ent)
				kbSource = fmt.Sprintf("SinoFacts (sinofacts.com), CC BY 4.0 — 匹配: %s", ent.Canonical)
			}
		}
		if kbCandidate == nil {
			results := e.kb.Search(brandName, 3)
			if len(results) > 0 {
				top := results[0]
				if top.Score >= 60 {
					kbCandidate = entryToCandidate(&top.Entry)
					kbSource = fmt.Sprintf("SinoFacts 离线库匹配(%.0f%%) — %s", top.Score, top.Source)
				}
				// 即便达不到高置信度，Top3 也作为 LLM 的上下文
				var b strings.Builder
				for _, r := range results {
					fmt.Fprintf(&b, "- [匹配度%.0f%%] %s（%s / %s，官网 %s）\n",
						r.Score, r.Entry.BrandName, r.Entry.Industry,
						r.Entry.Category, r.Entry.BrandDomain)
				}
				kbContext = b.String()
			}
		}
		// 高置信命中 + 无 LLM 可用 → 直接返回离线候选
		if kbCandidate != nil && (e.llmMgr == nil || !e.llmMgr.HasAvailable()) {
			if kbSource != "" {
				kbCandidate.Summary = kbSource + "\n\n" + kbCandidate.Summary
			}
			return kbCandidate, nil
		}
	}

	// 步骤 1b：离线工商注册库（1978-2019，1000万+ 条 MySQL + FULLTEXT ngram 索引）
	//  - 若知识库已给出 Company 但缺硬字段（信用代码/法人/成立日期等），用离线库补全
	//  - 若知识库完全没命中，离线库也能直接给出公司信息（LLM / 后续步骤的 ground truth）
	var odbContext string
	var odbCompanies []offlinedb.Company
	if e.offlineDB != nil {
		if res, err := e.offlineDB.Search(ctx, offlinedb.SearchOptions{Query: brandName, TopN: 5}); err == nil && len(res) > 0 {
			odbCompanies = res
			var b strings.Builder
			fmt.Fprintf(&b, "离线工商注册库（guichong/- JSON 分支，1978-2019 官方公开历史数据，MySQL）命中 %d 条：\n", len(res))
			for i, r := range res {
				fmt.Fprintf(&b, "  [%d] [匹配度%.0f%%] %s（信用代码 %s，法人 %s，成立 %s，省 %s 市 %s，资本 %s）\n",
					i+1, r.Score, r.Name, r.Code, r.LegalRepresentative, r.RegistrationDay,
					r.Province, r.City, r.Capital)
			}
			odbContext = b.String()
			// 如果知识库没有公司信息，直接用离线库 Top1 构造 Company 兜底
			if kbCandidate != nil && kbCandidate.Company == nil {
				kbCandidate.Company = offlineToBrandCompany(&res[0])
			}
		}
	}

	// 步骤 2：联网搜索补充（公司/竞品/行业信息等）
	searchResult := ""
	for eng, ad := range e.monitor.adapters {
		if !ad.Configured() {
			continue
		}
		resp, err := ad.Query(ctx, fmt.Sprintf("请简要介绍品牌「%s」：所属母公司/集团（含公司全称、官网域名）、公司介绍、主要产品、行业领域、主要竞争对手。", brandName))
		if err != nil {
			continue
		}
		searchResult = resp.Answer
		_ = eng
		break
	}
	// 步骤 2b：工商注册实时核验（GSXT / SAMR 官方数据，免费、免鉴权）
	// — 仅在启用 ChinaCheck 时执行；失败不影响其他路径（降级继续）。
	// — 以官方数据为准：公司名/法人/成立日期/资本/行业/注册地址覆盖掉候选值
	var ccContext string
	var ccSource string
	var ccSnap *chinacheck.Snapshot
	if e.chinaCheck != nil {
		if snap, err := enrichWithChinaCheck(ctx, e.chinaCheck, brandName, kbCandidate); err == nil && snap != nil {
			ccSnap = snap.Snapshot
			ccSource = fmt.Sprintf("工商核验信息（国家企业信用信息公示系统 / SAMR，经由 China-Check MCP 查询）：%s（登记状态：%s），统一社会信用代码 %s，成立 %s，注册资本 %s，行业 %s，注册地址 %s",
				firstNonEmpty(ccSnap.CompanyName, ccSnap.CompanyName),
				ccSnap.RegistrationStatus,
				ccSnap.CreditCode,
				ccSnap.EstablishedDate,
				ccSnap.RegisteredCapital,
				ccSnap.Industry,
				ccSnap.RegisteredAddress,
			)
			ccContext = ccSource
		}
	}

	// 步骤 2.5：官网爬虫自动补全（在知识库搜索之后、LLM 调用之前）
	//  - 优先用知识库中已有的域名；缺则并行猜测候选域名（brandname.com 等）
	//  - 爬取首页 HTML，提取 title/meta description/keywords/H1/H2/nav 产品线索
	//  - 补全候选画像中缺失的 domain 与 products 字段
	//  - 爬虫失败不影响主流程（降级继续 LLM 路径）
	var crawlerContext string
	if e.crawler != nil {
		domain := ""
		if kbCandidate != nil && kbCandidate.Domain != "" {
			domain = kbCandidate.Domain
		}
		if domain == "" {
			// 猜测域名
			if guessed, err := e.crawler.GuessDomain(ctx, brandName); err == nil {
				domain = guessed
			}
		}
		if domain != "" {
			if info, err := e.crawler.Crawl(ctx, domain); err == nil {
				crawlerContext = fmt.Sprintf("官网爬虫（%s）：\n- 标题: %s\n- 描述: %s\n- 关键词: %v\n- 产品线索: %v",
					domain, info.Title, info.Description, info.Keywords, info.ProductHints)
				// 补全缺失字段
				if kbCandidate != nil {
					if kbCandidate.Domain == "" {
						kbCandidate.Domain = domain
					}
					if len(kbCandidate.Products) == 0 && len(info.ProductHints) > 0 {
						kbCandidate.Products = info.ProductHints
					}
				}
			}
		}
	}

	// 无联网但有高置信知识库候选 → 直接返回（若有工商核验，合并后返回）
	if searchResult == "" && kbCandidate != nil && (e.llmMgr == nil || !e.llmMgr.HasAvailable()) {
		if ccSnap != nil {
			mergeGSXT(kbCandidate, ccSnap)
			kbCandidate.Summary = ccSource + "\n\n" + kbCandidate.Summary
		}
		if kbSource != "" {
			kbCandidate.Summary = kbSource + "\n\n" + kbCandidate.Summary
		}
		return kbCandidate, nil
	}
	if e.llmMgr == nil || !e.llmMgr.HasAvailable() {
		// 无 LLM：合并知识库+工商核验
		if kbCandidate != nil {
			if ccSnap != nil {
				mergeGSXT(kbCandidate, ccSnap)
				kbCandidate.Summary = ccSource + "\n\n" + kbCandidate.Summary
			}
			return kbCandidate, nil
		}
		if ccSnap != nil {
			// 知识库未命中，但工商直接出结果 → 至少给出公司信息
			c := &AutocompleteCandidate{Name: ccSnap.CompanyName}
			c.Company = &Company{
				Name:         ccSnap.CompanyName,
				Domain:       "", // 工商数据不含域名
				Description:  ccSnap.BusinessScope,
				Industry:     ccSnap.Industry,
				Headquarters: ccSnap.Province + " " + ccSnap.RegisteredAddress,
			}
			c.Company.CreditCode = ccSnap.CreditCode
			if y := parseYear(ccSnap.EstablishedDate); y > 0 {
				c.Company.FoundedYear = y
			}
			c.Summary = ccSource
			return c, nil
		}
		return nil, fmt.Errorf("品牌智能补全需要配置 LLM（GEO_LLM_KEY 环境变量）或命中离线知识库")
	}

	// 步骤 3：LLM 结构化分析（知识库上下文 + 建议候选 + 联网 + 工商核验 + 官网爬虫 全量传入）
	prompt := buildAutocompletePrompt(brandName, kbContext, kbCandidate, odbContext, ccContext, crawlerContext, searchResult)
	raw, err := e.llmMgr.Rewrite(ctx, prompt, "")
	if err != nil {
		if kbCandidate != nil {
			if ccSnap != nil {
				mergeGSXT(kbCandidate, ccSnap)
			}
			if len(odbCompanies) > 0 && kbCandidate.Company == nil {
				kbCandidate.Company = offlineToBrandCompany(&odbCompanies[0])
			}
			return kbCandidate, nil
		}
		return nil, fmt.Errorf("LLM 分析失败: %w", err)
	}
	// 步骤 4：解析 LLM JSON（失败退回 知识库+工商）
	candidate, err := parseAutocompleteJSON(raw, brandName)
	if err != nil {
		if kbCandidate != nil {
			if ccSnap != nil {
				mergeGSXT(kbCandidate, ccSnap)
			}
			if len(odbCompanies) > 0 && kbCandidate.Company == nil {
				kbCandidate.Company = offlineToBrandCompany(&odbCompanies[0])
			}
			return kbCandidate, nil
		}
		return nil, fmt.Errorf("解析 LLM 返回失败: %w", err)
	}
	// 最终合并：官方工商数据优先级最高（覆盖 LLM/知识库可能的幻觉）
	if ccSnap != nil {
		mergeGSXT(candidate, ccSnap)
	} else if len(odbCompanies) > 0 && candidate.Company != nil {
		// 没有实时 China-Check，但有离线历史数据 → 作为兜底（比 LLM 编的更靠谱）
		mergeOfflineIntoBrand(candidate.Company, &odbCompanies[0])
	}
	// 如果 LLM 完全没给出 Company，但离线库里有 → 兜底
	if candidate.Company == nil && len(odbCompanies) > 0 {
		candidate.Company = offlineToBrandCompany(&odbCompanies[0])
	}
	summary := candidate.Summary
	if searchResult != "" && summary == "" {
		summary = searchResult
	}
	if ccSource != "" {
		summary = ccSource + "\n\n" + summary
	}
	if odbContext != "" && ccSource == "" {
		// 只有在没有实时 China-Check 时才展示离线库来源（避免重复）
		summary = odbContext + "\n\n" + summary
	}
	if kbSource != "" {
		summary = kbSource + "\n\n" + summary
	}
	candidate.Summary = summary
	return candidate, nil
}

// buildAutocompletePrompt 构造品牌智能补全的 LLM 提示词。
//
// kbContext: 知识库模糊命中的候选列表（低置信度但仍可能有用）
// kbCandidate: 知识库高置信候选（≥60 分），作为"建议画像"给 LLM 参考
// odbContext: 1978-2019 离线工商库（历史 ground truth，比知识库/LLM 更高可信，字段是国家工商公示的历史登记值）
// gsxtContext: 来自 GSXT/SAMR（国家工商公示系统）的**实时**官方核验信息（可能为空）—— LLMs 应**完全信任**其中名称/信用代码/成立日期等字段，视为最高 ground truth
// crawlerContext: 来自品牌官网爬虫的首页信息（title/meta description/keywords/产品线索），用于补全 domain/products/aliases
// searchResult: 联网搜索到的品牌信息
func buildAutocompletePrompt(brandName, kbContext string, kbCandidate *AutocompleteCandidate, odbContext, gsxtContext, crawlerContext, searchResult string) string {
	prompt := fmt.Sprintf(`你是一位品牌分析专家。请根据以下信息，为品牌「%s」生成一份结构化的品牌画像 JSON。

`, brandName)
	if gsxtContext != "" {
		prompt += fmt.Sprintf(`【工商核验 · 最高可信】来自国家企业信用信息公示系统（GSXT/SAMR）实时官方数据。
以下字段请视为 ground truth：公司全称、统一社会信用代码、成立日期、注册资本、所属行业、登记状态、注册地址。
若与其他来源冲突，请以工商核验为准。
---
%s
---

`, gsxtContext)
	}
	if odbContext != "" {
		prompt += fmt.Sprintf(`【离线工商库 · 次高可信】来自 guichong/- 仓库（国家工商公示系统 1978-2019 年的公开历史数据），已导入 MySQL 并建立 FULLTEXT(ngram) 全文索引。
可信度：高于 LLM 自身知识与一般联网信息；低于上方【工商核验】实时数据（若实时数据与离线历史冲突，以实时为准）。
注意离线数据截止到 2019 年，登记状态/资本/经营范围可能有变化，但公司全称/信用代码/法人/成立日期/省份通常不变。
---
%s
---

`, odbContext)
	}
	if crawlerContext != "" {
		prompt += fmt.Sprintf(`【官网爬虫 · 参考信号】来自品牌官网首页（由爬虫自动抓取并解析）。
其中域名与产品线索（H1/H2/nav）可作为 domain/products 字段的参考；
title/description/keywords 可用于校验行业与品类。注意官网文案可能存在营销夸大，请结合其他来源交叉验证。
---
%s
---

`, crawlerContext)
	}
	if kbCandidate != nil {
		// 建议画像（SinoFacts 离线库，CC BY 4.0）：LLM 应优先采纳正确字段，
		// 仅在联网搜索或自身知识明显更准确时修正。
		b, _ := json.MarshalIndent(kbCandidate, "  ", "  ")
		prompt += fmt.Sprintf(`【建议画像】来自经过事实核验的离线知识库（SinoFacts CC BY 4.0）。
请优先采纳其中正确的字段，仅在联网信息或你的知识明确冲突时修正：
--- 建议画像 JSON ---
%s
--------------------

`, string(b))
	}
	if kbContext != "" {
		prompt += fmt.Sprintf(`【知识库候选】离线库中模糊匹配到的相近品牌（可作为竞品或行业参考）：
---
%s
---

`, kbContext)
	}
	if searchResult != "" {
		prompt += fmt.Sprintf(`【联网搜索结果】来自 AI 搜索引擎的实时信息：
---
%s
---

`, searchResult)
	}
	prompt += `请基于以上所有信息（离线工商库+知识库+官网爬虫+联网+你的知识），综合生成如下 JSON 格式的品牌画像（仅输出 JSON，不要其他文字）。
注意：
  【工商核验实时数据】>【离线工商历史数据】>【SinoFacts 事实核验知识库】>【官网爬虫信号】>【一般联网搜索】>【LLM 自身知识】
  当多个来源给出相同硬字段时，以可信度更高的来源为准，不要混用；信息不足时留空，不要编造。
{
  "name": "品牌全称",
  "domain": "品牌官网域名（不含协议，如 example.com）",
  "aliases": ["品牌别名1", "品牌简称2"],
  "industry": "品牌所属行业（大品类），如 企业软件 / 金融科技 / 电子商务 / 在线教育",
  "category": "品牌细分品类，如 CRM / 项目管理 / 在线支付",
  "products": ["主要产品1", "主要产品2"],
  "company": {
    "name": "所属母公司/集团全称，如 Salesforce, Inc.",
    "aliases": ["公司简称1", "公司别名2"],
    "domain": "公司官网域名（不含协议，如 salesforce.com）",
    "description": "公司简介（1-2句话）",
    "industry": "公司所属行业，如企业软件 / 电商 / 金融科技",
    "headquarters": "总部所在地，如 美国旧金山",
    "founded_year": 2000
  },
  "competitors": [
    {"name": "竞品品牌名", "domain": "竞品品牌域名", "company": {"name": "竞品所属公司名", "domain": "竞品公司官网"}}
  ],
  "prompts": [
    "潜在客户可能向 AI 提问的高意图查询词（5-8个），如「最好的XX工具」「XX软件推荐」"
  ]
}

要求：
- company 字段尽量完整，品牌所属的母公司/集团信息很重要；如果离线工商库或实时核验已给出公司全称、法人、成立日期、信用代码等，请直接沿用
- 竞品也尽量补齐 company 字段（所属公司）
- prompts 至少 5 个，覆盖品牌所在品类的主要搜索意图
- competitors 列出 3-5 个主要竞品
- 如果信息不足以确定某字段，留空或省略（不要编造），例如 founded_year 不确定就省略
- 仅输出纯 JSON，不要 markdown 代码块标记`
	return prompt
}

// parseAutocompleteJSON 从 LLM 返回文本中提取 JSON 并解析为候选画像。
func parseAutocompleteJSON(raw, brandName string) (*AutocompleteCandidate, error) {
	// 去除可能的 markdown 代码块标记
	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("LLM 返回中未找到有效 JSON")
	}
	var c AutocompleteCandidate
	if err := json.Unmarshal([]byte(jsonStr), &c); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w (原始: %s)", err, truncateStr(raw, 200))
	}
	if c.Name == "" {
		c.Name = brandName
	}
	if len(c.Prompts) == 0 {
		// 兜底：生成通用查询词
		c.Prompts = []string{
			fmt.Sprintf("最好的%s", c.Category),
			fmt.Sprintf("%s推荐", brandName),
		}
	}
	return &c, nil
}

// extractJSON 从可能包含 markdown 代码块的文本中提取 JSON 字符串。
func extractJSON(s string) string {
	// 尝试提取 ```json ... ``` 代码块
	start := indexOf(s, "```json")
	if start >= 0 {
		start += len("```json")
		end := indexOf(s[start:], "```")
		if end > 0 {
			return trimSpace(s[start : start+end])
		}
	}
	// 尝试提取 ``` ... ``` 代码块
	start = indexOf(s, "```")
	if start >= 0 {
		start += len("```")
		// 跳过可能的语言标记行
		if start < len(s) && s[start] != '\n' {
			nl := indexOf(s[start:], "\n")
			if nl >= 0 {
				start += nl + 1
			}
		}
		end := indexOf(s[start:], "```")
		if end > 0 {
			return trimSpace(s[start : start+end])
		}
	}
	// 尝试直接找 { ... } 结构
	braceStart := indexOf(s, "{")
	if braceStart >= 0 {
		braceEnd := lastIndexOf(s, "}")
		if braceEnd > braceStart {
			return s[braceStart : braceEnd+1]
		}
	}
	return ""
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------- 工商核验（China-Check MCP）辅助函数 ----------

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

// parseYear 从 "YYYY-MM-DD" 格式字符串中提取年份，失败返回 0。
func parseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year := 0
	for i := 0; i < 4; i++ {
		c := date[i]
		if c < '0' || c > '9' {
			return 0
		}
		year = year*10 + int(c-'0')
	}
	if year < 1900 || year > 2100 {
		return 0
	}
	return year
}

// enrichWithChinaCheck 通过 China-Check MCP 执行工商核验：
//  1. 先用品牌名/知识库中的公司名搜索匹配公司列表
//  2. 选最匹配的一条（或知识库已给出公司名时精确匹配）拉取完整 snapshot
//
// 出错不阻塞主流程，返回 (nil, nil) 调用方当作未命中即可。
func enrichWithChinaCheck(ctx context.Context, cc *chinacheck.Client, brandName string, kbCandidate *AutocompleteCandidate) (*chinacheck.SnapshotResponse, error) {
	if cc == nil {
		return nil, nil
	}
	// 构造查询词：优先知识库中已有的公司名（更精准），否则用品牌名
	query := brandName
	if kbCandidate != nil && kbCandidate.Company != nil && kbCandidate.Company.Name != "" {
		query = kbCandidate.Company.Name
	}
	// 步骤 1：搜索匹配
	sr, err := cc.Search(ctx, query, 5)
	if err != nil || sr == nil || len(sr.Companies) == 0 {
		// 搜索失败：退化为直接按品牌名 snapshot（工具支持按名称查询）
		snap, err2 := cc.GetSnapshot(ctx, "", brandName)
		if err2 != nil {
			return nil, err2
		}
		return snap, nil
	}
	// 步骤 2：从搜索结果中选最匹配的一条（简单策略：取第一条，或与知识库公司名完全相同的）
	bestID := sr.Companies[0].CompanyID
	if kbCandidate != nil && kbCandidate.Company != nil {
		canonical := strings.TrimSpace(kbCandidate.Company.Name)
		for _, h := range sr.Companies {
			if strings.TrimSpace(h.NameZh) == canonical {
				bestID = h.CompanyID
				break
			}
		}
	}
	// 步骤 3：获取完整 snapshot
	return cc.GetSnapshot(ctx, bestID, "")
}

// mergeGSXT 将官方工商 snapshot 数据合并到 AutocompleteCandidate 中。
// 官方数据优先级最高：公司名/信用代码/法人/成立日期/资本/行业/地址等直接覆盖。
// 品牌级字段（brand domain/aliases/products 等）不动，避免工商信息中没有这些而清空。
func mergeGSXT(c *AutocompleteCandidate, snap *chinacheck.Snapshot) {
	if c == nil || snap == nil {
		return
	}
	// 确保有 Company 容器
	if c.Company == nil {
		c.Company = &Company{}
	}
	co := c.Company
	// 公司名（工商数据中的是"注册全称"，比常见品牌更准确）
	if snap.CompanyName != "" {
		co.Name = snap.CompanyName
	}
	// 法定代表人
	if snap.LegalRepresentative != "" {
		co.LegalRepresentative = snap.LegalRepresentative
	}
	// 登记状态
	if snap.RegistrationStatus != "" {
		co.RegistrationStatus = snap.RegistrationStatus
	}
	// 成立日期 & 年份
	if snap.EstablishedDate != "" {
		co.EstablishedDate = snap.EstablishedDate
		if y := parseYear(snap.EstablishedDate); y > 0 {
			co.FoundedYear = y
		}
	}
	// 注册资本
	if snap.RegisteredCapital != "" {
		co.RegisteredCapital = snap.RegisteredCapital
	}
	if snap.PaidInCapital != "" {
		co.PaidInCapital = snap.PaidInCapital
	}
	// 统一社会信用代码
	if snap.CreditCode != "" {
		co.CreditCode = snap.CreditCode
	}
	// 企业类型
	if snap.CompanyType != "" {
		co.CompanyType = snap.CompanyType
	}
	// 行业（优先工商登记行业，比推断更准确）
	if snap.Industry != "" {
		co.Industry = snap.Industry
		// 如果品牌级 Industry 之前为空，也同步填充（合理默认）
		if c.Industry == "" {
			c.Industry = snap.Industry
		}
	}
	// 省份 + 注册地址
	if snap.Province != "" {
		co.Province = snap.Province
	}
	if snap.RegisteredAddress != "" {
		co.RegisteredAddress = snap.RegisteredAddress
		// 总部所在地的合理兜底
		if co.Headquarters == "" {
			if snap.Province != "" {
				co.Headquarters = snap.Province + " · " + snap.RegisteredAddress
			} else {
				co.Headquarters = snap.RegisteredAddress
			}
		}
	}
	// 经营范围：作为公司简介的 fallback（如果之前没有描述）
	if snap.BusinessScope != "" && co.Description == "" {
		co.Description = snap.BusinessScope
	}
	// 人员规模
	if snap.StaffSize != "" {
		co.StaffSize = snap.StaffSize
	}
}

// ---------- 离线工商库（offlinedb）辅助函数 ----------

// offlineToBrandCompany 将 offlinedb.Company（10 字段历史工商记录）转换为 brand.Company。
// 离线数据截止到 2019，缺少"登记状态"等新增字段，填充到能填的就好。
func offlineToBrandCompany(r *offlinedb.Company) *Company {
	if r == nil {
		return nil
	}
	co := &Company{
		Name:                r.Name,
		CreditCode:          r.Code,
		RegisteredCapital:   r.Capital,
		LegalRepresentative: r.LegalRepresentative,
		EstablishedDate:     r.RegistrationDay,
		CompanyType:         r.Character,
		BusinessScope:       r.BusinessScope,
		Province:            r.Province,
		RegisteredAddress:   r.Address,
		Headquarters:        firstNonEmpty(fmt.Sprintf("%s%s", r.Province, r.City), r.City, r.Province),
		Description:         r.BusinessScope,
	}
	if y := parseYear(r.RegistrationDay); y > 0 {
		co.FoundedYear = y
	}
	return co
}

// mergeOfflineIntoBrand 用离线工商记录硬字段覆盖 brand.Company 中"空的或可信度更低的"值。
// 注意离线数据仅截止 2019，所以**只有目标字段为空才覆盖**（避免把 China-Check 实时数据抹掉）。
func mergeOfflineIntoBrand(dst *Company, src *offlinedb.Company) {
	if dst == nil || src == nil {
		return
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.CreditCode == "" {
		dst.CreditCode = src.Code
	}
	if dst.RegisteredCapital == "" {
		dst.RegisteredCapital = src.Capital
	}
	if dst.LegalRepresentative == "" {
		dst.LegalRepresentative = src.LegalRepresentative
	}
	if dst.EstablishedDate == "" {
		dst.EstablishedDate = src.RegistrationDay
		if y := parseYear(src.RegistrationDay); y > 0 && dst.FoundedYear == 0 {
			dst.FoundedYear = y
		}
	}
	if dst.CompanyType == "" {
		dst.CompanyType = src.Character
	}
	if dst.BusinessScope == "" {
		dst.BusinessScope = src.BusinessScope
	}
	if dst.Province == "" {
		dst.Province = src.Province
	}
	if dst.RegisteredAddress == "" {
		dst.RegisteredAddress = src.Address
	}
	if dst.Description == "" {
		dst.Description = src.BusinessScope
	}
	if dst.Headquarters == "" {
		dst.Headquarters = firstNonEmpty(fmt.Sprintf("%s%s", src.Province, src.City), src.City, src.Province)
	}
}
