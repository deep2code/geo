// 本文件实现 UI 优先响应捕获适配器（WebUI 适配器）。
//
// 在 GEO（生成式引擎优化）场景中，LLM API 与 Web UI 渲染结果常存在差异：
//   - Web UI 包含引用卡片、UI 布局、实时排名等 API 不返回的信息
//   - 同一引擎的 API 版本与 Web 版本可能使用不同模型/参数
//   - 引用顺序、来源选择在 UI 与 API 间可能不一致
//
// 本文件提供基于 Playwright（通过外部进程或 MCP 浏览器工具）的 Web UI 捕获模式，
// 通过登录 ChatGPT / Gemini / Claude / Perplexity 的 Web 界面捕获渲染后的响应，
// 消除 LLM API 与 Web UI 版本间的差异。
//
// 由于 Go 无法直接引入 Playwright 作为依赖，浏览器自动化被抽象为 WebUICapturer 接口，
// 可由外部 Playwright 进程、MCP 浏览器工具或桩实现来满足。
package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-geo/internal/models"
)

// WebUICapturer 浏览器自动化抽象接口。
//
// 实现方需登录目标引擎的 Web 界面、输入查询、等待渲染并捕获结构化结果。
// 可由以下后端实现：
//   - 外部 Playwright 进程（通过 stdin/stdout 通信）
//   - MCP 浏览器工具（navigate + take_snapshot）
//   - StubCapturer（测试桩）
type WebUICapturer interface {
	// Capture 登录指定引擎的 Web 界面，输入查询并捕获渲染后的响应。
	Capture(ctx context.Context, engine models.EngineType, query string) (*WebUIResult, error)
	// Available 当前 capturer 是否可用（浏览器是否就绪、登录态是否存在）。
	Available() bool
}

// WebUIResult Web UI 捕获结果。
type WebUIResult struct {
	Answer       string            // 渲染后的回答文本
	Citations    []models.Citation // UI 中展示的引用列表（含卡片标题、片段）
	UISnapshot   string            // UI 快照的 JSON 字符串（布局/排名等结构化信息）
	RenderedHTML string            // 渲染后的完整 HTML
	CapturedAt   time.Time         // 捕获时间
}

// WebUIConfig WebUI 适配器配置。
type WebUIConfig struct {
	Enabled         bool          // 是否启用 WebUI 模式
	Capturer        WebUICapturer // 浏览器捕获器
	FallbackAdapter Adapter       // API 模式回退适配器
	CompareMode     bool          // 对比模式：同时调用 API 与 WebUI 并比较一致性
}

// ComparisonReport API 与 WebUI 响应对比报告。
type ComparisonReport struct {
	Consistent  bool     // 是否一致
	Differences []string // 差异描述列表
	APIScore    float64  // API 响应得分（0-1）
	WebUIScore  float64  // WebUI 响应得分（0-1）
}

// WebUIAdapter UI 优先响应捕获适配器，实现 Adapter 接口。
//
// 工作模式：
//   - API 模式（默认）：委托给 FallbackAdapter
//   - WebUI 模式：调用 WebUICapturer 捕获渲染响应
//   - 对比模式：同时调用两者并比较一致性
type WebUIAdapter struct {
	cfg         WebUIConfig
	fallback    Adapter
	capturer    WebUICapturer
	compareMode bool
	mu          sync.Mutex
	lastReport  *ComparisonReport // 最近一次对比报告（对比模式下填充）
}

// NewWebUIAdapter 创建 WebUI 适配器。
//
// cfg.Capturer 与 cfg.FallbackAdapter 均可为 nil（此时退化为纯 API 模式或返回错误）。
func NewWebUIAdapter(cfg WebUIConfig) *WebUIAdapter {
	return &WebUIAdapter{
		cfg:         cfg,
		fallback:    cfg.FallbackAdapter,
		capturer:    cfg.Capturer,
		compareMode: cfg.CompareMode,
	}
}

// 编译期断言：WebUIAdapter 必须实现 Adapter 接口。
var _ Adapter = (*WebUIAdapter)(nil)

// Engine 返回引擎类型（委托给 fallback 适配器）。
func (a *WebUIAdapter) Engine() models.EngineType {
	if a.fallback != nil {
		return a.fallback.Engine()
	}
	return ""
}

// Configured 是否已配置。
//
// WebUI 模式下检查 capturer 可用性；否则委托给 fallback 适配器。
func (a *WebUIAdapter) Configured() bool {
	if a.cfg.Enabled && a.capturer != nil && a.capturer.Available() {
		return true
	}
	if a.fallback != nil {
		return a.fallback.Configured()
	}
	return false
}

// SetCompareMode 动态开启/关闭对比模式。
func (a *WebUIAdapter) SetCompareMode(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.compareMode = enabled
}

// LastComparisonReport 返回最近一次对比报告（仅对比模式下有效）。
func (a *WebUIAdapter) LastComparisonReport() *ComparisonReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastReport
}

// Query 查询引擎，根据当前模式选择 WebUI 捕获或 API 委托。
func (a *WebUIAdapter) Query(ctx context.Context, query string) (*models.EngineResponse, error) {
	// 对比模式：同时调用 API 与 WebUI 并比较（compareMode 有并发写，加锁读）
	a.mu.Lock()
	compareMode := a.compareMode
	a.mu.Unlock()
	if compareMode {
		return a.queryWithComparison(ctx, query)
	}

	// WebUI 模式：capturer 可用时优先捕获
	if a.cfg.Enabled && a.capturer != nil && a.capturer.Available() {
		return a.queryViaWebUI(ctx, query)
	}

	// API 回退模式
	if a.fallback == nil {
		return nil, fmt.Errorf("WebUI 适配器未配置 fallback 适配器，且 capturer 不可用")
	}
	return a.fallback.Query(ctx, query)
}

// queryViaWebUI 通过 WebUI capturer 捕获响应，失败时回退到 API。
func (a *WebUIAdapter) queryViaWebUI(ctx context.Context, query string) (*models.EngineResponse, error) {
	engine := a.Engine()
	result, err := a.capturer.Capture(ctx, engine, query)
	if err != nil {
		// 捕获失败时回退到 API
		if a.fallback != nil {
			return a.fallback.Query(ctx, query)
		}
		return nil, fmt.Errorf("WebUI 捕获失败且无 fallback: %w", err)
	}
	return webUIResultToResponse(engine, result), nil
}

// queryWithComparison 对比模式：并行调用 API 与 WebUI，比较结果一致性。
//
// 并行调用可减少总耗时；对比报告存储于 lastReport，可由 LastComparisonReport 读取。
// 返回 WebUI 结果（更贴近真实用户所见），WebUI 失败时回退返回 API 结果。
func (a *WebUIAdapter) queryWithComparison(ctx context.Context, query string) (*models.EngineResponse, error) {
	var (
		wg      sync.WaitGroup
		apiResp *models.EngineResponse
		apiErr  error
		uiResp  *models.EngineResponse
		uiErr   error
	)

	engine := a.Engine()
	wg.Add(2)

	// API 调用
	go func() {
		defer wg.Done()
		if a.fallback != nil {
			apiResp, apiErr = a.fallback.Query(ctx, query)
		} else {
			apiErr = fmt.Errorf("未配置 fallback 适配器")
		}
	}()

	// WebUI 调用
	go func() {
		defer wg.Done()
		if a.capturer != nil && a.capturer.Available() {
			result, err := a.capturer.Capture(ctx, engine, query)
			if err != nil {
				uiErr = err
				return
			}
			uiResp = webUIResultToResponse(engine, result)
		} else {
			uiErr = fmt.Errorf("WebUI capturer 不可用")
		}
	}()

	wg.Wait()

	// WebUI 失败但 API 成功：回退返回 API 结果
	if uiErr != nil {
		if apiResp != nil {
			return apiResp, nil
		}
		return nil, fmt.Errorf("对比模式下 API 与 WebUI 均失败: api=%v, ui=%v", apiErr, uiErr)
	}

	// 生成对比报告并存储
	report := CompareResults(apiResp, uiResp)
	a.mu.Lock()
	a.lastReport = report
	a.mu.Unlock()

	// 对比模式优先返回 WebUI 结果
	return uiResp, nil
}

// CheckCitation 查询引擎并返回引用了 targetURL 的引用列表。
//
// 复用 checkCitationDefault，调用 Query 后筛选匹配 targetURL 的引用。
func (a *WebUIAdapter) CheckCitation(ctx context.Context, query, targetURL string) ([]models.Citation, error) {
	return checkCitationDefault(a, ctx, query, targetURL)
}

// webUIResultToResponse 将 WebUIResult 转换为 models.EngineResponse。
func webUIResultToResponse(engine models.EngineType, r *WebUIResult) *models.EngineResponse {
	if r == nil {
		return &models.EngineResponse{Engine: engine}
	}
	return &models.EngineResponse{
		Engine:    engine,
		Answer:    r.Answer,
		Citations: r.Citations,
	}
}

// CompareResults 对比 API 与 WebUI 响应，返回对比报告。
//
// 评分维度：
//   - 文本丰富度：归一化的回答词数（上限 100 词得满分）
//   - 引用丰富度：归一化的引用数（上限 5 条得满分）
//
// 综合得分 = 0.5*文本丰富度 + 0.5*引用丰富度，取值 0-1。
//
// 一致性判断阈值：
//   - 回答文本词级 Jaccard 相似度 >= 0.5
//   - 引用数量差 <= 2
//   - 引用 URL 重叠度 >= 0.5（双方有引用时）
//   - 综合得分差 <= 0.2
//
// 任一阈值未达标记入 Differences，全部达标则 Consistent 为 true。
func CompareResults(apiResp, webUIResp *models.EngineResponse) *ComparisonReport {
	report := &ComparisonReport{}

	// nil 归一化：对比模式下一侧失败时可能传入 nil，避免后续解引用 panic
	if apiResp == nil {
		apiResp = &models.EngineResponse{}
	}
	if webUIResp == nil {
		webUIResp = &models.EngineResponse{}
	}

	apiScore := scoreResponse(apiResp)
	uiScore := scoreResponse(webUIResp)
	report.APIScore = apiScore
	report.WebUIScore = uiScore

	var diffs []string

	// 1. 回答文本相似度（词级 Jaccard）
	apiWords := wordSet(apiResp.Answer)
	uiWords := wordSet(webUIResp.Answer)
	similarity := jaccardSimilarity(apiWords, uiWords)
	if similarity < 0.5 {
		diffs = append(diffs, fmt.Sprintf("回答文本相似度较低: %.2f (阈值 0.5)", similarity))
	}

	// 2. 引用数量差异
	apiCiteCount := citationCount(apiResp)
	uiCiteCount := citationCount(webUIResp)
	if absInt(apiCiteCount-uiCiteCount) > 2 {
		diffs = append(diffs, fmt.Sprintf("引用数量差异较大: API=%d, WebUI=%d", apiCiteCount, uiCiteCount))
	}

	// 3. 引用 URL 重叠度（双方均有引用时才校验）
	apiURLs := citationURLSet(apiResp.Citations)
	uiURLs := citationURLSet(webUIResp.Citations)
	if apiCiteCount > 0 || uiCiteCount > 0 {
		citeOverlap := jaccardSimilarity(apiURLs, uiURLs)
		if citeOverlap < 0.5 {
			diffs = append(diffs, fmt.Sprintf("引用 URL 重叠度较低: %.2f (阈值 0.5)", citeOverlap))
		}
	}

	// 4. 综合得分差异
	if absFloat(apiScore-uiScore) > 0.2 {
		diffs = append(diffs, fmt.Sprintf("综合得分差异较大: API=%.2f, WebUI=%.2f", apiScore, uiScore))
	}

	report.Differences = diffs
	report.Consistent = len(diffs) == 0
	return report
}

// scoreResponse 计算响应的综合得分（0-1）。
//
// 空响应得 0 分；文本与引用各占 50% 权重。
func scoreResponse(resp *models.EngineResponse) float64 {
	if resp == nil {
		return 0
	}
	// 文本丰富度：词数归一化，上限 100 词得满分
	wordCount := len(strings.Fields(resp.Answer))
	textRichness := float64(wordCount) / 100.0
	if textRichness > 1.0 {
		textRichness = 1.0
	}
	// 引用丰富度：引用数归一化，上限 5 条得满分
	citeRichness := float64(len(resp.Citations)) / 5.0
	if citeRichness > 1.0 {
		citeRichness = 1.0
	}
	return 0.5*textRichness + 0.5*citeRichness
}

// citationCount 安全获取引用数量（nil 安全）。
func citationCount(resp *models.EngineResponse) int {
	if resp == nil {
		return 0
	}
	return len(resp.Citations)
}

// wordSet 将文本拆分为小写词集合。
func wordSet(text string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(text) {
		set[strings.ToLower(w)] = true
	}
	return set
}

// citationURLSet 提取引用 URL 集合。
func citationURLSet(citations []models.Citation) map[string]bool {
	set := make(map[string]bool)
	for _, c := range citations {
		if c.URL != "" {
			set[c.URL] = true
		}
	}
	return set
}

// jaccardSimilarity 计算两个集合的 Jaccard 相似度（交集/并集）。
//
// 两个空集合视为完全相似（返回 1.0）。
func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// absInt 整数绝对值。
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// absFloat 浮点数绝对值。
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// StubCapturer WebUICapturer 的桩实现，返回模拟数据。
//
// 用于测试或浏览器不可用时，保证系统可无浏览器运行。
type StubCapturer struct {
	available bool // 是否标记为可用
}

// NewStubCapturer 创建桩捕获器。
//
// available 控制 Available() 返回值，决定是否真正参与 WebUI 捕获。
func NewStubCapturer(available bool) *StubCapturer {
	return &StubCapturer{available: available}
}

// Capture 返回模拟的 WebUI 捕获结果。
func (s *StubCapturer) Capture(ctx context.Context, engine models.EngineType, query string) (*WebUIResult, error) {
	return &WebUIResult{
		Answer: fmt.Sprintf("[Stub 捕获] 引擎=%s 查询=%q 的模拟 Web UI 回答。"+
			"此结果来自 StubCapturer，仅用于测试，不代表真实 Web UI 渲染内容。", engine, query),
		Citations: []models.Citation{
			{URL: "https://example.com/stub-1", Title: "模拟引用1", Position: 1},
			{URL: "https://example.com/stub-2", Title: "模拟引用2", Position: 2},
		},
		UISnapshot:   fmt.Sprintf(`{"mock":true,"engine":"%s"}`, engine),
		RenderedHTML: "<div>stub rendered html</div>",
		CapturedAt:   time.Now(),
	}, nil
}

// Available 桩捕获器是否可用。
func (s *StubCapturer) Available() bool {
	return s.available
}

// MCPBrowserCapturer 基于 MCP 浏览器工具的 WebUICapturer 实现（桩）。
//
// 设计为通过 MCP 浏览器工具链（navigate_page + take_snapshot + fill 等）完成 Web UI 捕获。
// 当前为桩实现：Capture 返回错误并打印 MCP 集成指引，实际的 MCP 浏览器调用
// 应由宿主环境注入（宿主检测到 MCP 浏览器可用后调用 SetAvailable(true)）。
type MCPBrowserCapturer struct {
	// EngineURLs 各引擎 Web UI 入口地址。
	EngineURLs map[models.EngineType]string
	// availableFlag 是否标记为可用（宿主注入 MCP 后置为 true）。
	availableFlag bool
}

// NewMCPBrowserCapturer 创建 MCP 浏览器捕获器，预置各引擎 Web UI 入口。
func NewMCPBrowserCapturer() *MCPBrowserCapturer {
	return &MCPBrowserCapturer{
		EngineURLs: map[models.EngineType]string{
			models.EngineChatGPT:    "https://chatgpt.com",
			models.EngineGemini:     "https://gemini.google.com",
			models.EngineClaude:     "https://claude.ai",
			models.EnginePerplexity: "https://www.perplexity.ai",
		},
	}
}

// Capture 通过 MCP 浏览器工具捕获 Web UI 响应（当前为桩实现）。
//
// 完整实现需宿主环境提供 MCP 浏览器工具并通过 run_mcp 调用：
//  1. navigate_page 打开引擎 Web UI 入口
//  2. 等待登录态就绪（fill 凭据或使用已保存会话）
//  3. 定位输入框并 fill 查询文本
//  4. 等待响应渲染完成（wait_for 选择器）
//  5. take_snapshot 捕获 UI 快照，提取回答文本与引用卡片
//  6. 将快照转换为 WebUIResult 返回
//
// 当前未实际接入，返回集成指引错误。
func (m *MCPBrowserCapturer) Capture(ctx context.Context, engine models.EngineType, query string) (*WebUIResult, error) {
	entryURL, ok := m.EngineURLs[engine]
	if !ok {
		return nil, fmt.Errorf("MCPBrowserCapturer: 未配置引擎 %s 的 Web UI 入口", engine)
	}

	instructions := fmt.Sprintf(
		"MCP 浏览器捕获未实际接入。集成步骤：\n"+
			"  1. 通过 MCP 浏览器工具 navigate_page 打开 %s\n"+
			"  2. 等待登录态就绪（fill 用户名/密码或使用已保存会话）\n"+
			"  3. 定位输入框并 fill 查询: %q\n"+
			"  4. 等待响应渲染完成（wait_for 选择器）\n"+
			"  5. take_snapshot 捕获 UI 快照，提取回答文本与引用卡片\n"+
			"  6. 将快照转换为 WebUIResult 返回\n"+
			"当前为桩实现，请由宿主环境注入 MCP 浏览器调用后启用",
		entryURL, query,
	)
	return nil, fmt.Errorf("%s", instructions)
}

// Available MCP 浏览器捕获器是否可用。
func (m *MCPBrowserCapturer) Available() bool {
	return m.availableFlag
}

// SetAvailable 由宿主环境在注入 MCP 浏览器后调用，标记为可用。
func (m *MCPBrowserCapturer) SetAvailable(available bool) {
	m.availableFlag = available
}
