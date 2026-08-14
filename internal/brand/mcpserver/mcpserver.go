// Package mcpserver 将 GEO 系统的核心能力暴露为 MCP Server。
//
// 实现 JSON-RPC 2.0 over HTTP 协议（Streamable HTTP transport），
// 让 Claude / Cursor / TraeCode 等 MCP 客户端可以直接调用 GEO 的：
//   - 品牌可见度审计（BVS 评分 + 运营报告）
//   - 内容 GEO 优化（评分提升 + 策略应用）
//   - 离线工商库搜索（1978-2019，1000万+ 条）
//   - 实时工商核验（GSXT/SAMR 官方数据）
//   - AI 可见度就绪审计（robots.txt / llms.txt / 结构化数据等）
//
// 不依赖 MCP SDK，直接用标准库 net/http + encoding/json 手写 JSON-RPC 2.0，
// 参考 internal/brand/chinacheck 中已有的 MCP 客户端实现的反向逻辑。
//
// 端点：POST /mcp
// 协议版本：2025-06-18
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/brand/readiness"
	"my-geo/internal/models"
	"my-geo/pkg/geo"
)

const (
	// protocolVersion MCP 协议版本（与 chinacheck 客户端一致）。
	protocolVersion = "2025-06-18"
	// serverName MCP Server 名称。
	serverName = "geo-mcp-server"
	// serverVersion MCP Server 版本。
	serverVersion = "1.0.0"
	// maxBodyBytes 单次请求体读取上限（8MB，与 chinacheck 客户端一致）。
	maxBodyBytes = 8 << 20
)

// Server MCP Server，独立的 HTTP 服务，暴露 GEO 能力给 MCP 客户端。
//
// 与 internal/server.Server（REST API）独立运行在不同端口，互不冲突。
type Server struct {
	brandEngine *brand.Engine // 品牌可见度引擎（可为 nil，对应工具返回错误）
	geoEngine   *geo.Engine   // GEO 内容优化引擎（可为 nil，对应工具返回错误）
	addr        string        // 监听地址，如 ":9090"
}

// New 创建 MCP Server。
//
// brandEngine / geoEngine 可为 nil，对应的工具调用会返回友好的错误提示。
func New(brandEngine *brand.Engine, geoEngine *geo.Engine, addr string) *Server {
	return &Server{
		brandEngine: brandEngine,
		geoEngine:   geoEngine,
		addr:        addr,
	}
}

// Start 启动 MCP HTTP 服务（阻塞调用）。
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.ServeHTTP)
	return http.ListenAndServe(s.addr, mux)
}

// ServeHTTP 处理 MCP JSON-RPC 2.0 请求。
//
// 支持的方法：
//   - initialize：握手，返回 server info + capabilities + session id
//   - notifications/initialized：握手完成通知（无 id，不返回响应）
//   - tools/list：返回工具列表
//   - tools/call：执行工具调用
//   - ping：心跳检测
//
// 响应格式兼容纯 JSON 与 SSE（根据 Accept 头决定）。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 仅接受 POST
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, r, nil, -32700, "解析请求体失败: "+err.Error())
		return
	}

	// 解析 JSON-RPC 2.0 请求
	var req struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, r, nil, -32700, "JSON 解析失败: "+err.Error())
		return
	}

	// 通知（无 id 或 id 为 null）：接受但不返回 JSON-RPC 响应
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// 分发方法
	var result interface{}
	var rpcErr *rpcError

	switch req.Method {
	case "initialize":
		// 握手：生成并返回 session id
		sessionID := fmt.Sprintf("geo-mcp-%d", time.Now().UnixNano())
		w.Header().Set("Mcp-Session-Id", sessionID)
		w.Header().Set("Mcp-Protocol-Version", protocolVersion)
		result = s.handleInitialize()

	case "notifications/initialized":
		// 有 id 但方法名是通知 → 兼容部分客户端的奇怪行为，返回空结果
		result = map[string]interface{}{}

	case "tools/list":
		result = s.handleToolsList()

	case "tools/call":
		result, rpcErr = s.handleToolsCall(r.Context(), req.Params)

	case "ping":
		result = map[string]interface{}{}

	default:
		rpcErr = &rpcError{Code: -32601, Message: "未知方法: " + req.Method}
	}

	// 写响应
	if rpcErr != nil {
		s.writeError(w, r, req.ID, rpcErr.Code, rpcErr.Message)
		return
	}
	s.writeResult(w, r, req.ID, result)
}

// rpcError JSON-RPC 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleInitialize 返回 initialize 方法的响应结果。
func (s *Server) handleInitialize() interface{} {
	return map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    serverName,
			"version": serverVersion,
		},
	}
}

// mcpTool 工具定义。
type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// handleToolsList 返回 tools/list 方法的响应结果。
func (s *Server) handleToolsList() interface{} {
	return map[string]interface{}{
		"tools": s.toolDefinitions(),
	}
}

// toolDefinitions 返回全部工具定义。
func (s *Server) toolDefinitions() []mcpTool {
	return []mcpTool{
		{
			Name:        "geo_brand_audit",
			Description: "品牌可见度审计：检测品牌在 AI 搜索引擎（ChatGPT/Claude/Perplexity/GLM 等）中的可见度，生成 BVS 评分（0-100）、等级（A-F）、6 维评分明细与运营行动建议。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"brand_name": map[string]interface{}{
						"type":        "string",
						"description": "品牌名称（必须），如「Acme」或「腾讯」",
					},
					"prompts": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "业务相关查询词列表（高意图 prompt），如 [\"最好的CRM工具\", \"项目管理软件推荐\"]",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "品牌官网域名（不含协议），如 example.com",
					},
					"competitors": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "竞争对手名称列表，用于声量份额（SOV）计算",
					},
				},
				"required": []string{"brand_name", "prompts"},
			},
		},
		{
			Name:        "geo_optimize_content",
			Description: "内容 GEO 优化：对给定内容应用 Princeton 论文 9 种优化策略，提升其在 AI 搜索引擎中的可见度。返回优化后内容、前后评分、应用的策略与建议。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "待优化的原始内容（必须）",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "内容标题（可选，用于生成结构化数据）",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "内容 URL（可选，用于生成 JSON-LD / llms.txt）",
					},
					"engines": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "目标引擎列表，如 [\"chatgpt\", \"perplexity\", \"gemini\", \"claude\"]，不指定则通用优化",
					},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:        "geo_search_companies",
			Description: "离线工商库搜索：在 MySQL 离线工商注册数据库（1978-2019，1000万+ 条，源自 guichong/- 仓库）中按名称/品牌/法人模糊搜索匹配的公司列表。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索词（必须），支持公司名/品牌/法人等",
					},
					"province": map[string]interface{}{
						"type":        "string",
						"description": "省份筛选（可选），如「广东」「北京」",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "返回条数（可选，默认 10）",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "geo_chinacheck",
			Description: "实时工商核验：通过 China-Check MCP 查询国家企业信用信息公示系统（GSXT/SAMR），返回公司工商快照（统一社会信用代码/法人/注册资本/登记状态/成立日期等 20+ 字段）。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"company_name": map[string]interface{}{
						"type":        "string",
						"description": "公司名称（必须），如「腾讯科技（深圳）有限公司」",
					},
				},
				"required": []string{"company_name"},
			},
		},
		{
			Name:        "geo_readiness_audit",
			Description: "AI 可见度就绪审计：检查网站对 AI 搜索引擎的可见度就绪度，覆盖 robots.txt AI 爬虫检查、llms.txt、结构化数据、sitemap.xml、页面性能（TTFB）5 个维度，返回综合评分与各检查项明细。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "网站 URL（必须），如 example.com 或 https://example.com",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// handleToolsCall 处理 tools/call 方法，执行工具调用并返回 MCP 标准的 content 数组。
func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "参数解析失败: " + err.Error()}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]interface{}{}
	}

	var text string
	var err error
	switch p.Name {
	case "geo_brand_audit":
		text, err = s.toolBrandAudit(ctx, p.Arguments)
	case "geo_optimize_content":
		text, err = s.toolOptimizeContent(ctx, p.Arguments)
	case "geo_search_companies":
		text, err = s.toolSearchCompanies(ctx, p.Arguments)
	case "geo_chinacheck":
		text, err = s.toolChinaCheck(ctx, p.Arguments)
	case "geo_readiness_audit":
		text, err = s.toolReadinessAudit(ctx, p.Arguments)
	default:
		return nil, &rpcError{Code: -32602, Message: "未知工具: " + p.Name}
	}

	// 工具执行错误：返回 MCP 标准的 isError content（非 JSON-RPC 协议错误）
	if err != nil {
		return map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": "工具执行失败: " + err.Error()},
			},
			"isError": true,
		}, nil
	}

	// 成功：返回 MCP 标准的 content 数组
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
		"isError": false,
	}, nil
}

// ---------- 工具实现 ----------

// toolBrandAudit 品牌可见度审计。
func (s *Server) toolBrandAudit(ctx context.Context, args map[string]interface{}) (string, error) {
	if s.brandEngine == nil {
		return "", fmt.Errorf("品牌审计引擎未初始化（请配置引擎 API Key 环境变量）")
	}
	profile := brand.BrandProfile{
		Name:    getString(args, "brand_name"),
		Domain:  getString(args, "domain"),
		Prompts: getStringSlice(args, "prompts"),
	}
	// 竞争对手
	for _, c := range getStringSlice(args, "competitors") {
		profile.Competitors = append(profile.Competitors, brand.Competitor{Name: c})
	}
	if profile.Name == "" {
		return "", fmt.Errorf("brand_name 不能为空")
	}
	if len(profile.Prompts) == 0 {
		return "", fmt.Errorf("prompts 不能为空")
	}

	report, err := s.brandEngine.Audit(ctx, profile)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	return string(b), nil
}

// toolOptimizeContent 内容 GEO 优化。
func (s *Server) toolOptimizeContent(ctx context.Context, args map[string]interface{}) (string, error) {
	if s.geoEngine == nil {
		return "", fmt.Errorf("GEO 优化引擎未初始化")
	}
	req := &models.OptimizationRequest{
		Content: getString(args, "content"),
		Title:   getString(args, "title"),
		URL:     getString(args, "url"),
	}
	for _, e := range getStringSlice(args, "engines") {
		req.TargetEngines = append(req.TargetEngines, models.EngineType(e))
	}
	if req.Content == "" {
		return "", fmt.Errorf("content 不能为空")
	}

	resp, err := s.geoEngine.Optimize(ctx, req)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return string(b), nil
}

// toolSearchCompanies 离线工商库搜索。
func (s *Server) toolSearchCompanies(ctx context.Context, args map[string]interface{}) (string, error) {
	if s.brandEngine == nil {
		return "", fmt.Errorf("品牌引擎未初始化")
	}
	odb := s.brandEngine.OfflineDB()
	if odb == nil {
		return "", fmt.Errorf("离线工商库未启用（设 GEO_OFFLINE_DB_ENABLED=true 以启用）")
	}
	opt := offlinedb.SearchOptions{
		Query:    getString(args, "query"),
		Province: getString(args, "province"),
		TopN:     getInt(args, "limit", 10),
	}
	if opt.Query == "" {
		return "", fmt.Errorf("query 不能为空")
	}

	results, err := odb.Search(ctx, opt)
	if err != nil {
		return "", err
	}
	out := map[string]interface{}{
		"query":   opt.Query,
		"count":   len(results),
		"results": results,
		"source":  "guichong/- JSON 分支（国家工商公示系统 1978-2019 公开历史数据）→ MySQL + FULLTEXT(ngram)",
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// toolChinaCheck 实时工商核验。
func (s *Server) toolChinaCheck(ctx context.Context, args map[string]interface{}) (string, error) {
	if s.brandEngine == nil {
		return "", fmt.Errorf("品牌引擎未初始化")
	}
	cc := s.brandEngine.ChinaCheck()
	if cc == nil {
		return "", fmt.Errorf("China-Check MCP 未启用（设 GEO_CHINACHECK_ENABLED=true 以启用）")
	}
	companyName := getString(args, "company_name")
	if companyName == "" {
		return "", fmt.Errorf("company_name 不能为空")
	}

	snap, err := cc.GetSnapshot(ctx, "", companyName)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(snap, "", "  ")
	return string(b), nil
}

// toolReadinessAudit AI 可见度就绪审计。
func (s *Server) toolReadinessAudit(ctx context.Context, args map[string]interface{}) (string, error) {
	rawURL := getString(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url 不能为空")
	}

	result, err := readiness.Audit(ctx, rawURL)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ---------- JSON-RPC 响应写入 ----------

// writeResult 写 JSON-RPC 成功响应（兼容 JSON 与 SSE）。
func (s *Server) writeResult(w http.ResponseWriter, r *http.Request, id json.RawMessage, result interface{}) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	s.writeResponse(w, r, resp)
}

// writeError 写 JSON-RPC 错误响应（兼容 JSON 与 SSE）。
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, id json.RawMessage, code int, message string) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	s.writeResponse(w, r, resp)
}

// writeResponse 根据 Accept 头选择 JSON 或 SSE 格式写入响应。
//
// MCP Streamable HTTP transport 允许服务端用纯 JSON 或 SSE 响应，
// 客户端通过 Accept 头声明可接受的格式。这里：
//   - Accept 含 text/event-stream → 用 SSE 格式（event: message \n data: <json> \n\n）
//   - 否则 → 纯 JSON
func (s *Server) writeResponse(w http.ResponseWriter, r *http.Request, resp interface{}) {
	data, err := json.Marshal(resp)
	if err != nil {
		// 序列化失败是服务端 bug，返回 500
		http.Error(w, "内部错误: "+err.Error(), http.StatusInternalServerError)
		return
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") {
		// SSE 格式响应
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		return
	}

	// 纯 JSON 响应
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ---------- 参数提取辅助函数 ----------

// getString 从 args 中安全提取字符串字段。
func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringSlice 从 args 中安全提取字符串切片。
func getStringSlice(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// getInt 从 args 中安全提取整数字段，未提供时返回默认值。
func getInt(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return def
}
