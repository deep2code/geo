// Package attribution 提供"AI 引荐流量 / ROI 归因"能力。
//
// 商业 GEO 的分水岭：客户为"可见度分数"而来，为"ROI"续费。本包把品牌在 AI 引擎中的
// 可见度，归因到真实的网站流量、转化与收入，回答"被 AI 引用到底带来了多少生意"。
//
// 设计：
//   - 流量源可插拔：GA4 API / 服务器访问日志(referrer) / UTM 标记导出
//   - 归因模型：把"来自 AI 域名/AI UTM"的会话视为 AI 引荐；结合可见度时序做加权
//   - 时序落库：ai_traffic / ai_conversion 两张表（见 deploy/initdb/02-schema.sql）
//   - 仅依赖标准库，Store 可注入（内存/MySQL），便于测试与部署
package attribution

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// TrafficPoint 一个时间点的流量/转化/收入观测。
type TrafficPoint struct {
	Date        string  `json:"date"`         // YYYY-MM-DD
	Source      string  `json:"source"`        // 流量源标识（ga4/sitelog/utm）
	Sessions    int     `json:"sessions"`      // 会话数
	Conversions int     `json:"conversions"`   // 转化数
	Revenue     float64 `json:"revenue"`       // 收入（本币）
	AISourced   bool    `json:"ai_sourced"`    // 是否判定为 AI 引荐（utm_source 含 ai / referrer 属已知 AI 域名）
}

// Source 流量源接口。
//
// Fetch 返回 [from,to] 闭区间内的日粒度流量点。不同实现从 GA4 API、服务器日志、
// UTM 导出读取。实现应只返回"原始观测"，AI 引荐判定由 Tracker 统一完成。
type Source interface {
	// Name 流量源标识。
	Name() string
	// Configured 是否已正确配置（未配置时 Fetch 返回错误）。
	Configured() bool
	// Fetch 拉取 [from,to] 区间日粒度流量。
	Fetch(ctx context.Context, from, to time.Time) ([]TrafficPoint, error)
}

// AIReferrerDomains 已知属于 AI 引擎/AI 聚合的 referrer 域名（用于日志归因）。
var AIReferrerDomains = []string{
	"chat.openai.com", "chatgpt.com", "perplexity.ai", "bing.com", "copilot.microsoft.com",
	"gemini.google.com", "claude.ai", "anthropic.com", "you.com", "metac.ai",
	"kimi.moonshot.cn", "yuanbao.tencent.com", "doubao.com", "tongyi.aliyun.com",
	"baidu.com", "qianwen.aliyun.com", "xiaoyi.chat",
}

// IsAIReferrer 判断 referrer 是否来自已知 AI 域名。
//
// 解析 referrer 的 host 后做等值/子域后缀匹配，避免子串匹配误判：
// 如 "fishing.bing.com.evil.cn"（攻击域）或 "example.com/?ref=chatgpt.com"（查询串）。
func IsAIReferrer(referrer string) bool {
	r := strings.ToLower(strings.TrimSpace(referrer))
	if r == "" {
		return false
	}
	u, err := url.Parse(r)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	for _, d := range AIReferrerDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// UTMMatcher 判定 UTM 参数是否属于 AI 引荐。
//
// 约定：utm_source 含 "ai"/"chatgpt"/"perplexity"/"llm" 等关键字，或 utm_medium="ai"。
type UTMMatcher struct {
	// ExtraKeywords 额外命中关键字（小写）。
	ExtraKeywords []string
}

// Match 判定一组 UTM 参数是否为 AI 引荐。
//
// 采用 token 级匹配避免子串误判（如 "ai" 不应命中 "email"）：
// 先把 utm_source/utm_medium 按非字母数字切分为 token，再比对关键字；
// 关键字长度 ≥4 时支持前缀匹配（如 "chatgpt_ads" 命中 "chatgpt"）。
func (m UTMMatcher) Match(utmSource, utmMedium string) bool {
	keys := append([]string{"ai", "chatgpt", "perplexity", "llm", "generative"}, m.ExtraKeywords...)
	isAlnum := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	split := func(v string) []string {
		return strings.FieldsFunc(v, func(r rune) bool { return !isAlnum(r) })
	}
	for _, v := range []string{utmSource, utmMedium} {
		for _, tok := range split(v) {
			tl := strings.ToLower(tok)
			for _, k := range keys {
				kl := strings.ToLower(k)
				if kl == "" {
					continue
				}
				if tl == kl {
					return true
				}
				if len(kl) >= 4 && strings.HasPrefix(tl, kl) {
					return true
				}
			}
		}
	}
	return false
}

// Tracker 归因计算器。
type Tracker struct {
	sources []Source
	utm     UTMMatcher
	// AIAttributionWeight 当某日可见度上升时，AI 引荐流量的归因权重（0-1）。
	// 用于把"AI 引荐会话"乘以可见度弹性系数，得到"由 GEO 努力带来的增量"。
	AIAttributionWeight float64
}

// NewTracker 创建归因计算器，sources 可为空（仅做 referrer/UTM 判定）。
func NewTracker(sources []Source) *Tracker {
	return &Tracker{
		sources:              sources,
		utm:                  UTMMatcher{},
		AIAttributionWeight: 0.7,
	}
}

// AddSource 追加流量源。
func (t *Tracker) AddSource(s Source) { t.sources = append(t.sources, s) }

// Compute 拉取各源流量，判定 AI 引荐，结合可见度时序做归因。
//
// visibilitySeries 为历史可见度评分（按日期），用于计算"AI 引荐中可归因于 GEO 的部分"。
// 返回聚合的 AttributionReport。
func (t *Tracker) Compute(ctx context.Context, brandID string, from, to time.Time, visibility map[string]float64) (*AttributionReport, error) {
	report := &AttributionReport{
		BrandID:   brandID,
		From:      from,
		To:        to,
		GeneratedAt: time.Now(),
	}
	var all []TrafficPoint
	for _, s := range t.sources {
		if !s.Configured() {
			continue
		}
		pts, err := s.Fetch(ctx, from, to)
		if err != nil {
			return nil, fmt.Errorf("流量源 %s 拉取失败: %w", s.Name(), err)
		}
		all = append(all, pts...)
	}

	// 聚合到日粒度
	type dayAgg struct {
		sessions, conv   int
		revenue          float64
		aiSessions       int
		aiConv           int
		aiRevenue        float64
	}
	byDay := map[string]*dayAgg{}
	for _, p := range all {
		a, ok := byDay[p.Date]
		if !ok {
			a = &dayAgg{}
			byDay[p.Date] = a
		}
		a.sessions += p.Sessions
		a.conv += p.Conversions
		a.revenue += p.Revenue
		// AI 引荐判定：源已标记，或 referrer/UTM 命中（由具体 Source 在 Fetch 时设置 AISourced）
		if p.AISourced {
			a.aiSessions += p.Sessions
			a.aiConv += p.Conversions
			a.aiRevenue += p.Revenue
		}
	}

	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)

	for _, d := range days {
		a := byDay[d]
		// 可归因于 GEO 的增量 = AI 引荐 × 权重 × (可见度/100 弹性)。
		// 区分"无可见度数据"（用中性 0.5 兜底）与"实测可见度为 0"（弹性 0，不可归因），
		// 否则零可见度日的归因反而高于低可见度日，排序完全颠倒。
		vis, hasVis := visibility[d]
		elastic := vis / 100
		if !hasVis {
			elastic = 0.5
		}
		attributedSessions := int(float64(a.aiSessions) * t.AIAttributionWeight * elastic)
		attributedConv := int(float64(a.aiConv) * t.AIAttributionWeight * elastic)
		attributedRevenue := a.aiRevenue * t.AIAttributionWeight * elastic
		report.Daily = append(report.Daily, DailyAttribution{
			Date:               d,
			Visibility:         vis,
			AISessions:         a.aiSessions,
			AIConversions:      a.aiConv,
			AIRevenue:          a.aiRevenue,
			AttributedSessions: attributedSessions,
			AttributedConv:     attributedConv,
			AttributedRevenue:  attributedRevenue,
		})
		report.TotalSessions += a.sessions
		report.TotalConversions += a.conv
		report.TotalRevenue += a.revenue
		report.AISessions += a.aiSessions
		report.AIConversions += a.aiConv
		report.AIRevenue += a.aiRevenue
		report.AttributedSessions += attributedSessions
		report.AttributedConversions += attributedConv
		report.AttributedRevenue += attributedRevenue
	}
	if report.TotalSessions > 0 {
		report.AIShare = float64(report.AISessions) / float64(report.TotalSessions) * 100
	}
	if report.TotalConversions > 0 {
		report.AIConvRate = float64(report.AIConversions) / float64(report.TotalConversions) * 100
	}
	return report, nil
}

// DailyAttribution 单日归因明细。
type DailyAttribution struct {
	Date               string  `json:"date"`
	Visibility         float64 `json:"visibility"`
	AISessions         int     `json:"ai_sessions"`
	AIConversions      int     `json:"ai_conversions"`
	AIRevenue          float64 `json:"ai_revenue"`
	AttributedSessions int     `json:"attributed_sessions"`
	AttributedConv     int     `json:"attributed_conversions"`
	AttributedRevenue  float64 `json:"attributed_revenue"`
}

// AttributionReport AI 引荐流量 / ROI 归因报告。
type AttributionReport struct {
	BrandID   string  `json:"brand_id"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	GeneratedAt time.Time `json:"generated_at"`
	Daily     []DailyAttribution `json:"daily"`

	TotalSessions     int     `json:"total_sessions"`
	TotalConversions  int     `json:"total_conversions"`
	TotalRevenue      float64 `json:"total_revenue"`

	AISessions    int     `json:"ai_sessions"`
	AIConversions int     `json:"ai_conversions"`
	AIRevenue     float64 `json:"ai_revenue"`

	AttributedSessions    int     `json:"attributed_sessions"`
	AttributedConversions int     `json:"attributed_conversions"`
	AttributedRevenue     float64 `json:"attributed_revenue"`

	AIShare   float64 `json:"ai_share"`    // AI 引荐会话占比 %
	AIConvRate float64 `json:"ai_conv_rate"` // AI 引荐转化占比 %
}

// Store 归因数据持久化接口（注入式，便于测试与多后端）。
type Store interface {
	SaveTraffic(ctx context.Context, brandID string, points []TrafficPoint) error
	ListTraffic(ctx context.Context, brandID string, from, to time.Time) ([]TrafficPoint, error)
	SaveReport(ctx context.Context, r *AttributionReport) error
}

// MemoryStore 内存实现（测试/单机用）。
type MemoryStore struct {
	traffic map[string][]TrafficPoint // brandID -> points
	reports map[string][]*AttributionReport
}

// NewMemoryStore 创建内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		traffic: map[string][]TrafficPoint{},
		reports: map[string][]*AttributionReport{},
	}
}

// SaveTraffic 写入流量点。
func (m *MemoryStore) SaveTraffic(_ context.Context, brandID string, points []TrafficPoint) error {
	m.traffic[brandID] = append(m.traffic[brandID], points...)
	return nil
}

// ListTraffic 列出区间流量点。
func (m *MemoryStore) ListTraffic(_ context.Context, brandID string, from, to time.Time) ([]TrafficPoint, error) {
	out := make([]TrafficPoint, 0)
	for _, p := range m.traffic[brandID] {
		d, err := time.Parse("2006-01-02", p.Date)
		if err != nil {
			continue
		}
		if !d.Before(from) && !d.After(to) {
			out = append(out, p)
		}
	}
	return out, nil
}

// SaveReport 保存报告。
func (m *MemoryStore) SaveReport(_ context.Context, r *AttributionReport) error {
	m.reports[r.BrandID] = append(m.reports[r.BrandID], r)
	return nil
}
