// Package chinacheck 封装 China-Check MCP（Streamable HTTP JSON-RPC）客户端。
//
// 数据源：国家企业信用信息公示系统（GSXT / SAMR）公开注册数据。
// 服务端：https://www.china-check.com/api/mcp/mcp  —— 免鉴权、免费查询。
// 提供 2 个只读工具：
//   - search_chinese_company：按名称/品牌/域名/信用代码搜索匹配公司列表
//   - get_company_snapshot：按公司 ID 或名称查询工商注册快照（20+ 字段）
//
// 本实现不依赖 MCP SDK，直接用标准库 net/http + encoding/json 发 JSON-RPC 2.0，
// 避免增加第三方依赖；对 SSE / 纯 JSON 两种响应格式均兼容。
package chinacheck

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultURL     = "https://www.china-check.com/api/mcp/mcp"
	defaultLang    = "zh" // 枚举字段优先中文
	protocolVer    = "2025-06-18"
	clientName     = "geo-chinacheck-client"
	clientVersion  = "1.0.0"
	defaultTimeout = 30 * time.Second
)

// Client China-Check MCP 客户端（并发安全，单次查询可共享）。
type Client struct {
	baseURL    string
	language   string
	httpClient *http.Client
	cache      Cache // 可选：本地持久化缓存（nil 表示不缓存）

	sessionMu sync.Mutex   // P1-1：保护懒初始化握手（ensureSession）
	sessionID atomic.Value // string，首次 initialize 后填入
	idSeq     atomic.Int64 // JSON-RPC request id 自增
	flight    flightGroup  // P1-4：缓存 miss 合并（singleflight，防击穿）
}

// Option 配置选项。
type Option func(*Client)

// WithURL 自定义 MCP endpoint（默认官方公共端点）。
func WithURL(url string) Option {
	return func(c *Client) {
		if url != "" {
			c.baseURL = url
		}
	}
}

// WithLanguage 设置 enum/标签字段的翻译语言（ISO 代码）。
// 已知支持：zh, en, ru, ar, ja, ko, es, pt, vi, id, th。
func WithLanguage(lang string) Option {
	return func(c *Client) {
		if lang != "" {
			c.language = lang
		}
	}
}

// WithHTTPClient 注入自定义 HTTP Client（比如调超时、加代理）。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithCache 注入本地持久化缓存（nil 为禁用）。
// 缓存命中时跳过网络，极大降低延迟并避开官方限流风险。
// 推荐配合 server 的默认启用策略使用。
func WithCache(ca Cache) Option {
	return func(c *Client) { c.cache = ca }
}

// Cache 返回当前绑定的缓存实例（可能为 nil）。
func (c *Client) Cache() Cache { return c.cache }

// New 创建客户端。
//
// 注意：该函数不发起网络请求；实际 initialize 在首次查询时懒触发。
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultURL,
		language:   defaultLang,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Available 是否可用（懒初始化前总返回 true；失败后用户可用具体 error 判断）。
// 这里没有 ping，仅用于与品牌 Engine 其他适配器保持统一语义。
func (c *Client) Available() bool { return c != nil }

// ---------- 公有工具函数：Search + Snapshot ----------

// SearchResult 搜索结果。
type SearchResult struct {
	Companies []CompanyHit `json:"companies"`
	Total     int          `json:"total"`
}

// CompanyHit 搜索列表中的单个命中。
type CompanyHit struct {
	CompanyID         string  `json:"companyId"`
	NameZh            string  `json:"nameZh"`
	NameTranslated    string  `json:"nameTranslated"`
	RegistrationNo    string  `json:"registrationNo"` // 统一社会信用代码
	EstablishedAt     string  `json:"establishedAt"`  // YYYY-MM-DD
	LegalPersonName   string  `json:"legalPersonName"`
	RegisteredCapital string  `json:"regCapital"`  // 如 "CNY 41,141,131,820"
	CompanyType       string  `json:"companyType"` // 企业类型
	Base              string  `json:"base"`        // 省/地区
	ConfidenceScore   float64 `json:"-"`           // 本地计算，非接口返回
}

// Search 按名称 / 品牌 / 域名 / 统一社会信用代码 搜索公司。
// 启用缓存时：先命中缓存（<1ms），否则走网络并将结果写入缓存。
// P1-4：缓存未命中时同 key 并发请求通过 singleflight 合并，避免击穿打到外部 API。
func (c *Client) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("查询词不能为空")
	}
	key := searchKey(c.language, query, limit)
	// 缓存优先
	if c.cache != nil {
		if hit, ok := c.cache.GetSearch(c.language, query, limit); ok {
			return hit, nil
		}
	}
	v, err := c.flight.Do(key, func() (interface{}, error) {
		// 双检：等锁期间其他 goroutine 可能已回填缓存
		if c.cache != nil {
			if hit, ok := c.cache.GetSearch(c.language, query, limit); ok {
				return hit, nil
			}
		}
		args := map[string]interface{}{
			"query":    query,
			"language": c.language,
		}
		if limit > 0 {
			args["limit"] = limit
		}
		var out SearchResult
		if err := c.callTool(ctx, "search_chinese_company", args, &out); err != nil {
			return nil, err
		}
		// 写缓存（错误忽略：缓存失败不影响查询结果）；空结果也写，
		// 形成负缓存，避免不存在的品牌反复穿透网络。
		if c.cache != nil {
			_ = c.cache.SetSearch(c.language, query, limit, &out)
		}
		return &out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*SearchResult), nil
}

// Snapshot 工商注册快照。
type Snapshot struct {
	CompanyName         string   `json:"companyName"`
	LegalRepresentative string   `json:"legalRepresentative"`
	RegistrationStatus  string   `json:"registrationStatus"` // 在营/吊销/注销等
	EstablishedDate     string   `json:"establishedDate"`    // YYYY-MM-DD
	RegisteredCapital   string   `json:"registeredCapital"`
	PaidInCapital       string   `json:"paidInCapital"`
	CreditCode          string   `json:"creditCode"` // 统一社会信用代码
	RegistrationNumber  string   `json:"registrationNumber"`
	OrganizationCode    string   `json:"organizationCode"`
	TaxNumber           string   `json:"taxNumber"`
	CompanyType         string   `json:"companyType"`
	Industry            string   `json:"industry"` // 所属行业
	Province            string   `json:"province"`
	RegisteredAddress   string   `json:"registeredAddress"` // 注册地址
	BusinessScope       string   `json:"businessScope"`     // 经营范围
	StaffSize           string   `json:"staffSize"`
	ApprovedDate        string   `json:"approvedDate"`
	RegistrationAuth    string   `json:"registrationAuthority"`
	BusinessTerm        string   `json:"businessTerm"`
	FormerNames         []string `json:"formerNames"`
	// 可选顶层字段（工具返回的其他信息，我们忽略 report_options 等付费提示）
	Disclaimer string `json:"disclaimer,omitempty"`
	CompanyID  string `json:"companyId,omitempty"`
}

// SnapshotResponse get_company_snapshot 外层响应。
type SnapshotResponse struct {
	CompanyID  string    `json:"companyId"`
	Snapshot   *Snapshot `json:"snapshot"`
	Disclaimer string    `json:"disclaimer,omitempty"`
}

// GetSnapshot 按公司 ID（优先，更精准）或查询词获取注册快照。
// 启用缓存时：先命中缓存（<1ms），否则走网络并将结果写入缓存（双 key：ID + 查询词）。
// P1-4：缓存未命中时同 key 并发请求通过 singleflight 合并。
func (c *Client) GetSnapshot(ctx context.Context, companyID, query string) (*SnapshotResponse, error) {
	if companyID == "" && strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("company_id 或 query 至少提供一个")
	}
	key := snapshotKeyByID(companyID)
	if companyID == "" {
		key = snapshotKeyByQuery(query)
	}
	// 缓存优先
	if c.cache != nil {
		if hit, ok := c.cache.GetSnapshot(companyID, query); ok {
			return hit, nil
		}
	}
	v, err := c.flight.Do(key, func() (interface{}, error) {
		// 双检：等锁期间其他 goroutine 可能已回填缓存
		if c.cache != nil {
			if hit, ok := c.cache.GetSnapshot(companyID, query); ok {
				return hit, nil
			}
		}
		args := map[string]interface{}{
			"language": c.language,
		}
		if companyID != "" {
			args["companyId"] = companyID
		} else {
			args["query"] = query
		}
		var out SnapshotResponse
		if err := c.callTool(ctx, "get_company_snapshot", args, &out); err != nil {
			return nil, err
		}
		if out.Snapshot != nil {
			out.Snapshot.CompanyID = out.CompanyID
		}
		// 写缓存（缓存失败不影响结果）；空快照也写，形成负缓存
		if c.cache != nil {
			_ = c.cache.SetSnapshot(companyID, query, &out)
		}
		return &out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*SnapshotResponse), nil
}

// ---------- JSON-RPC / MCP 底层 ----------

// callTool 走完整 JSON-RPC 工具调用：懒 initialize → call → 解包 content[0].text → 解析 JSON。
func (c *Client) callTool(ctx context.Context, tool string, args map[string]interface{}, out interface{}) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	params := map[string]interface{}{
		"name":      tool,
		"arguments": args,
	}
	var raw json.RawMessage
	if err := c.rpc(ctx, "tools/call", params, true, &raw); err != nil {
		return fmt.Errorf("调用 %s 失败: %w", tool, err)
	}
	// MCP tool result schema: { "content": [{ "type": "text", "text": <JSON字符串> }] }
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("解析 MCP content 失败: %w (raw=%s)", err, truncate(raw, 500))
	}
	if wrapper.IsError {
		return fmt.Errorf("%s 返回错误: %s", tool, truncate(raw, 400))
	}
	if len(wrapper.Content) == 0 || wrapper.Content[0].Text == "" {
		return fmt.Errorf("%s 返回空结果", tool)
	}
	text := wrapper.Content[0].Text
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("解析 %s 业务 JSON 失败: %w (text=%s)", tool, err, truncate([]byte(text), 500))
	}
	return nil
}

// ensureSession 确保已完成 initialize 握手（幂等，并发安全）。
//
// P1-1：原实现"读-判-写"无锁，并发请求会重复握手并互相覆盖 session。
// 现在用 sessionMu 串行化握手，配合 double-check 避免重复 initialize；
// notifications/initialized 改为同步发送（3s 短超时），不再 spawn 脱离
// ctx 控制的 goroutine（原实现调用量大时会堆积且无法取消）。
func (c *Client) ensureSession(ctx context.Context) error {
	if sid, _ := c.sessionID.Load().(string); sid != "" {
		return nil
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	// double-check：等待锁期间可能已有别的 goroutine 完成握手
	if sid, _ := c.sessionID.Load().(string); sid != "" {
		return nil
	}
	initParams := map[string]interface{}{
		"protocolVersion": protocolVer,
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    clientName,
			"version": clientVersion,
		},
	}
	// 用一次完整 round-trip 来拿 Mcp-Session-Id 头。
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params":  initParams,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("initialize 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("initialize 返回 %d: %s", resp.StatusCode, string(b))
	}
	// 响应头里拿 session id（Streamable HTTP）
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		sid = resp.Header.Get("mcp-session-id")
	}
	// 吞掉响应体（JSON-RPC 初始化结果对我们不重要，只要 session）
	io.Copy(io.Discard, resp.Body)
	if sid == "" {
		// 部分实现可能没返回，我们仍继续（非 session 模式也可能工作），
		// 但写入一个占位避免重复握手。
		sid = "no-session"
	}
	c.sessionID.Store(sid)

	// 发送 notifications/initialized（无 id，不期待响应）。
	// P1-1：同步发送 + 3s 短超时兜底——best-effort 通知不值得为之泄漏 goroutine；
	// 发送失败仅警告，不阻塞握手主流程（多数服务器不需要该通知）。
	notif, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	// WithoutCancel：通知属于会话建立的一部分，不受调用方 ctx 提前取消影响
	notifCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	req2, err := http.NewRequestWithContext(notifCtx, http.MethodPost, c.baseURL, bytes.NewReader(notif))
	if err != nil {
		return nil // 构造通知请求失败不阻塞握手主流程，仅跳过通知
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "no-session" {
		req2.Header.Set("Mcp-Session-Id", sid)
		req2.Header.Set("Mcp-Protocol-Version", protocolVer)
	}
	r, err := c.httpClient.Do(req2)
	if r != nil {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
	if err != nil {
		// best-effort：通知失败不影响会话可用
		fmt.Printf("[chinacheck 警告] notifications/initialized 发送失败: %v\n", err)
	}
	return nil
}

// rpc 发 JSON-RPC 请求，解析 result 到 out（同时兼容纯 JSON 与 SSE 响应）。
func (c *Client) rpc(ctx context.Context, method string, params interface{}, needID bool, out *json.RawMessage) error {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	if needID {
		payload["id"] = c.nextID()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid, _ := c.sessionID.Load().(string); sid != "" && sid != "no-session" {
		req.Header.Set("Mcp-Session-Id", sid)
		req.Header.Set("Mcp-Protocol-Version", protocolVer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return c.readSSE(resp.Body, out)
	}
	// 纯 JSON
	var rpcResp struct {
		Result *json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ID json.RawMessage `json:"id"`
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB cap
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}
	if err := json.Unmarshal(b, &rpcResp); err != nil {
		return fmt.Errorf("解析 JSON-RPC 响应失败: %w (body=%s)", err, truncate(b, 500))
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("JSON-RPC 错误 %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		return fmt.Errorf("JSON-RPC 响应无 result (body=%s)", truncate(b, 500))
	}
	*out = *rpcResp.Result
	return nil
}

// readSSE 解析 SSE（text/event-stream）响应体，提取最新一条 data: JSON。
func (c *Client) readSSE(r io.Reader, out *json.RawMessage) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var latest json.RawMessage
	var buf strings.Builder
	inData := false
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			// event 结束：处理
			if inData && buf.Len() > 0 {
				// data: 行可能是 JSON-RPC 2.0 响应对象，或只是字符串
				text := strings.TrimSpace(buf.String())
				var candidate struct {
					Result *json.RawMessage `json:"result"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(text), &candidate); err == nil {
					if candidate.Error != nil {
						return fmt.Errorf("SSE RPC 错误: %s", candidate.Error.Message)
					}
					if candidate.Result != nil {
						latest = *candidate.Result
					}
				}
				buf.Reset()
				inData = false
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			inData = true
			buf.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		// event: / id: / retry: 忽略
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("SSE 读取失败: %w", err)
	}
	// 如果上面在 data: 行收集但没遇到空行，再兜底解析一次
	if latest == nil && buf.Len() > 0 {
		var candidate struct {
			Result *json.RawMessage          `json:"result"`
			Error  *struct{ Message string } `json:"error"`
		}
		if err := json.Unmarshal([]byte(buf.String()), &candidate); err == nil {
			if candidate.Error != nil {
				return fmt.Errorf("SSE RPC 错误: %s", candidate.Error.Message)
			}
			if candidate.Result != nil {
				latest = *candidate.Result
			}
		}
	}
	if latest == nil {
		return fmt.Errorf("SSE 响应中未提取到有效 result")
	}
	*out = latest
	return nil
}

func (c *Client) nextID() int64 { return c.idSeq.Add(1) }

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
