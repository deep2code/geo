// domestic.go 国内信号源适配器：在 DataForSEO 等海外信号之外，
// 接入百度指数、微信指数、知乎热榜、小红书蒲公英、抖音云图、新闻通稿、CRM 等
// 国内品牌情报源。所有适配器实现 DomesticSignalProvider 接口，
// 由 DomesticAggregator 统一编排。
//
// 容错策略与 externalsignals.go 一致：未配置 API Key 时返回带 "estimated"
// 标记的确定性估算数据（同一品牌每次结果一致），保证管线在无 Key 环境下可运行。
// 仅依赖标准库，零第三方依赖。
package externalsignals

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// DomesticSignalProvider 国内信号源统一接口。
type DomesticSignalProvider interface {
	// Name 返回供应商标识（如 "baidu_index"）。
	Name() string
	// Available 是否已配置有效凭据（即可走真实 API）。
	Available() bool
	// Fetch 采集指定品牌的国内信号；未配置 Key 时返回估算数据。
	Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error)
}

// DomesticSignal 国内信号数据。
type DomesticSignal struct {
	Provider     string        `json:"provider"`
	Brand        string        `json:"brand"`
	IndexScore   float64       `json:"index_score"`             // 指数分（0-100）
	Trend        []float64     `json:"trend"`                   // 7日趋势
	Mentions     int           `json:"mentions"`                // 提及量
	Sentiment    float64       `json:"sentiment"`               // -1~1
	TopKeywords  []string      `json:"top_keywords"`            // 热门关联词
	Demographics *Demographics `json:"demographics,omitempty"`
	FetchedAt    time.Time     `json:"fetched_at"`
	Source       string        `json:"source"` // "api" | "estimated"
}

// Demographics 人群画像。
type Demographics struct {
	AgeGroups map[string]float64 `json:"age_groups"` // "18-24": 0.25
	Gender    map[string]float64 `json:"gender"`     // "male": 0.55
	Regions   map[string]float64 `json:"regions"`    // "广东": 0.15
}

// domesticHTTPClient 国内信号源共享的 HTTP 客户端，各适配器复用。
var domesticHTTPClient = &http.Client{Timeout: 30 * time.Second}

// ---------------- 百度指数 ----------------

// BaiduIndex 百度指数适配器。
type BaiduIndex struct {
	apiKey string
	client *http.Client
}

// NewBaiduIndex 从环境变量 GEO_BAIDU_INDEX_KEY 构造百度指数适配器。
func NewBaiduIndex(client *http.Client) *BaiduIndex {
	return &BaiduIndex{
		apiKey: strings.TrimSpace(os.Getenv("GEO_BAIDU_INDEX_KEY")),
		client: client,
	}
}

// Name 返回供应商标识。
func (b *BaiduIndex) Name() string { return "baidu_index" }

// Available 是否已配置百度指数 Key。
func (b *BaiduIndex) Available() bool { return b != nil && b.apiKey != "" }

// Fetch 采集百度指数。未配置 Key 时返回估算数据。
// 真实接口：https://index.baidu.com/api/ChannelApi/getMultiChannelData（仅注释标注，预留未实现）
func (b *BaiduIndex) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !b.Available() {
		return estimatedSignal(b.Name(), brand, keywords), nil
	}
	// TODO: 调用 https://index.baidu.com/api/ChannelApi/getMultiChannelData
	// 当前预留接口，不实际调用外部，返回估算数据。
	return estimatedSignal(b.Name(), brand, keywords), nil
}

// ---------------- 微信指数 ----------------

// WeChatIndex 微信指数适配器。
type WeChatIndex struct {
	apiKey string
	client *http.Client
}

// NewWeChatIndex 从环境变量 GEO_WECHAT_INDEX_KEY 构造微信指数适配器。
func NewWeChatIndex(client *http.Client) *WeChatIndex {
	return &WeChatIndex{
		apiKey: strings.TrimSpace(os.Getenv("GEO_WECHAT_INDEX_KEY")),
		client: client,
	}
}

func (w *WeChatIndex) Name() string    { return "wechat_index" }
func (w *WeChatIndex) Available() bool { return w != nil && w.apiKey != "" }

// Fetch 采集微信指数。未配置 Key 时返回估算数据。
// 真实接口预留（微信指数开放平台），暂未实现。
func (w *WeChatIndex) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !w.Available() {
		return estimatedSignal(w.Name(), brand, keywords), nil
	}
	// TODO: 调用微信指数开放平台接口，当前预留，返回估算数据。
	return estimatedSignal(w.Name(), brand, keywords), nil
}

// ---------------- 知乎热榜 ----------------

// ZhihuHot 知乎热榜适配器，抓取热榜中匹配品牌关键词的条目。
type ZhihuHot struct {
	apiKey string
	client *http.Client
}

// NewZhihuHot 从环境变量 GEO_ZHIHU_HOT_KEY 构造知乎热榜适配器。
func NewZhihuHot(client *http.Client) *ZhihuHot {
	return &ZhihuHot{
		apiKey: strings.TrimSpace(os.Getenv("GEO_ZHIHU_HOT_KEY")),
		client: client,
	}
}

func (z *ZhihuHot) Name() string    { return "zhihu_hot" }
func (z *ZhihuHot) Available() bool { return z != nil && z.apiKey != "" }

// Fetch 抓取知乎热榜中匹配品牌关键词的条目。未配置 Key 时返回估算数据。
func (z *ZhihuHot) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !z.Available() {
		return estimatedSignal(z.Name(), brand, keywords), nil
	}
	// TODO: 抓取知乎热榜并匹配品牌关键词，当前预留，返回估算数据。
	return estimatedSignal(z.Name(), brand, keywords), nil
}

// ---------------- 小红书蒲公英 ----------------

// Xiaohongshu 小红书蒲公英适配器，采集笔记数/互动量/达人数。
type Xiaohongshu struct {
	apiKey string
	client *http.Client
}

// NewXiaohongshu 从环境变量 GEO_XHS_KEY 构造小红书蒲公英适配器。
func NewXiaohongshu(client *http.Client) *Xiaohongshu {
	return &Xiaohongshu{
		apiKey: strings.TrimSpace(os.Getenv("GEO_XHS_KEY")),
		client: client,
	}
}

func (x *Xiaohongshu) Name() string    { return "xiaohongshu" }
func (x *Xiaohongshu) Available() bool { return x != nil && x.apiKey != "" }

// Fetch 采集小红书蒲公英笔记数/互动量/达人数。未配置 Key 时返回估算数据。
func (x *Xiaohongshu) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !x.Available() {
		return estimatedSignal(x.Name(), brand, keywords), nil
	}
	// TODO: 调用小红书蒲公英接口获取笔记数/互动量/达人数，当前预留，返回估算数据。
	return estimatedSignal(x.Name(), brand, keywords), nil
}

// ---------------- 抖音云图/巨量算数 ----------------

// DouyinOcean 抖音云图/巨量算数适配器，采集播放量/互动率/达人指数。
type DouyinOcean struct {
	apiKey string
	client *http.Client
}

// NewDouyinOcean 从环境变量 GEO_DOUYIN_OCEAN_KEY 构造抖音云图适配器。
func NewDouyinOcean(client *http.Client) *DouyinOcean {
	return &DouyinOcean{
		apiKey: strings.TrimSpace(os.Getenv("GEO_DOUYIN_OCEAN_KEY")),
		client: client,
	}
}

func (d *DouyinOcean) Name() string    { return "douyin_ocean" }
func (d *DouyinOcean) Available() bool { return d != nil && d.apiKey != "" }

// Fetch 采集抖音播放量/互动率/达人指数。未配置 Key 时返回估算数据。
func (d *DouyinOcean) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !d.Available() {
		return estimatedSignal(d.Name(), brand, keywords), nil
	}
	// TODO: 调用巨量算数/抖音云图接口，当前预留，返回估算数据。
	return estimatedSignal(d.Name(), brand, keywords), nil
}

// ---------------- 新闻通稿（美通社/财新）----------------

// NewsWire 新闻通稿适配器，抓取美通社/财新中品牌提及数。
type NewsWire struct {
	apiKey string
	client *http.Client
}

// NewNewsWire 从环境变量 GEO_NEWSWIRE_KEY 构造新闻通稿适配器。
func NewNewsWire(client *http.Client) *NewsWire {
	return &NewsWire{
		apiKey: strings.TrimSpace(os.Getenv("GEO_NEWSWIRE_KEY")),
		client: client,
	}
}

func (n *NewsWire) Name() string    { return "newswire" }
func (n *NewsWire) Available() bool { return n != nil && n.apiKey != "" }

// Fetch 抓取新闻通稿中品牌提及数。未配置 Key 时返回估算数据。
func (n *NewsWire) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !n.Available() {
		return estimatedSignal(n.Name(), brand, keywords), nil
	}
	// TODO: 抓取美通社/财新新闻通稿中品牌提及数，当前预留，返回估算数据。
	return estimatedSignal(n.Name(), brand, keywords), nil
}

// ---------------- CRM（Salesforce/HubSpot）----------------

// CRM 客户关系管理适配器，返回品牌相关的线索数/商机数。
// 通过 GEO_CRM_TYPE 指定类型（salesforce/hubspot），GEO_CRM_KEY 提供凭据。
type CRM struct {
	crmType string // "salesforce" | "hubspot"
	apiKey  string
	client  *http.Client
}

// NewCRM 从环境变量 GEO_CRM_TYPE 与 GEO_CRM_KEY 构造 CRM 适配器。
func NewCRM(client *http.Client) *CRM {
	return &CRM{
		crmType: strings.ToLower(strings.TrimSpace(os.Getenv("GEO_CRM_TYPE"))),
		apiKey:  strings.TrimSpace(os.Getenv("GEO_CRM_KEY")),
		client:  client,
	}
}

// Name 返回供应商标识，附带 CRM 类型（如 "crm:salesforce"）。
func (c *CRM) Name() string {
	if c != nil && c.crmType != "" {
		return "crm:" + c.crmType
	}
	return "crm"
}

// Available 是否已配置有效的 CRM 类型与凭据。
func (c *CRM) Available() bool {
	if c == nil || c.apiKey == "" || c.crmType == "" {
		return false
	}
	switch c.crmType {
	case "salesforce", "hubspot":
		return true
	}
	return false
}

// Fetch 返回品牌相关的线索数/商机数。未配置凭据时返回估算数据。
func (c *CRM) Fetch(ctx context.Context, brand string, keywords []string) (*DomesticSignal, error) {
	if !c.Available() {
		return estimatedSignal(c.Name(), brand, keywords), nil
	}
	// TODO: 调用 Salesforce/HubSpot API 获取品牌相关线索数/商机数，当前预留，返回估算数据。
	return estimatedSignal(c.Name(), brand, keywords), nil
}

// ---------------- 聚合器 ----------------

// DomesticAggregator 国内信号源聚合器，编排多个 DomesticSignalProvider。
type DomesticAggregator struct {
	providers []DomesticSignalProvider
}

// NewDomesticAggregator 构造聚合器，注册全部国内信号源适配器（共享同一 HTTP 客户端）。
func NewDomesticAggregator() *DomesticAggregator {
	a := &DomesticAggregator{}
	a.providers = []DomesticSignalProvider{
		NewBaiduIndex(domesticHTTPClient),
		NewWeChatIndex(domesticHTTPClient),
		NewZhihuHot(domesticHTTPClient),
		NewXiaohongshu(domesticHTTPClient),
		NewDouyinOcean(domesticHTTPClient),
		NewNewsWire(domesticHTTPClient),
		NewCRM(domesticHTTPClient),
	}
	return a
}

// FetchAll 顺序采集所有供应商的信号；单个失败不影响其余，全部失败时返回错误。
func (a *DomesticAggregator) FetchAll(ctx context.Context, brand string, keywords []string) ([]DomesticSignal, error) {
	out := make([]DomesticSignal, 0, len(a.providers))
	for _, p := range a.providers {
		sig, err := p.Fetch(ctx, brand, keywords)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[externalsignals] %s: 采集失败: %v\n", p.Name(), err)
			continue
		}
		if sig != nil {
			out = append(out, *sig)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("所有国内信号源采集均失败")
	}
	return out, nil
}

// AvailableProviders 返回已配置凭据（Available()=true）的供应商名称列表。
func (a *DomesticAggregator) AvailableProviders() []string {
	names := make([]string, 0, len(a.providers))
	for _, p := range a.providers {
		if p.Available() {
			names = append(names, p.Name())
		}
	}
	return names
}

// ---------------- 估算逻辑 ----------------

// estimatedSignal 基于品牌名 hash 生成确定性估算信号（同一品牌同一供应商每次结果一致）。
// 数值区间：
//   - index_score: 30-60
//   - trend: 7 个点，围绕 index_score ±10 波动
//   - mentions: 100-10000
//   - sentiment: -0.2 ~ 0.5
//   - source: "estimated"
func estimatedSignal(provider, brand string, keywords []string) *DomesticSignal {
	// 基于 provider+brand 生成确定性种子，保证同一品牌同一供应商结果稳定可复现
	sum := sha1.Sum([]byte(provider + ":" + brand))
	seed := binary.BigEndian.Uint64(sum[:8])

	// index_score: 30-60
	indexScore := 30.0 + float64(seed%31)

	// trend: 7 个点，围绕 index_score ±10 波动
	trend := make([]float64, 7)
	for i := 0; i < 7; i++ {
		offset := float64((seed>>(uint(i)*4+3))%21) - 10.0 // -10 ~ +10
		v := indexScore + offset
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		trend[i] = round2(v)
	}

	// mentions: 100-10000
	mentions := 100 + int(seed%9901)

	// sentiment: -0.2 ~ 0.5
	sentiment := -0.2 + float64(seed%71)/100.0

	// top_keywords: 优先用入参关键词，缺失时回退品牌名
	topKw := make([]string, 0, len(keywords))
	for _, k := range keywords {
		if k = strings.TrimSpace(k); k != "" {
			topKw = append(topKw, k)
		}
	}
	if len(topKw) == 0 {
		topKw = []string{brand}
	}

	return &DomesticSignal{
		Provider:    provider,
		Brand:       brand,
		IndexScore:  round2(indexScore),
		Trend:       trend,
		Mentions:    mentions,
		Sentiment:   round2(sentiment),
		TopKeywords: topKw,
		FetchedAt:   time.Now(),
		Source:      "estimated",
	}
}

// round2 保留两位小数。
func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
